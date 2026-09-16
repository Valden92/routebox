package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/ping"
	"github.com/Valden92/routebox/internal/probe"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

// Автовыбор узла: пинг → порядок (вес ↓, пинг ↑) → пробный sing-box → сайты.
// Вес: успех +1, провал −1 (хранится в подписке). Прогресс — через job + poll.

const (
	autoSelectDefaultSites = "https://www.google.com/generate_204"
	autoSelectMaxSites     = 5
	// 0 в запросе = перебрать все узлы. Верхняя страховка от гигантских подписок.
	autoSelectHardCapNodes  = 500
	autoSelectOverallBudget = 15 * time.Minute
	autoSelectSiteTimeout   = 12 * time.Second
	autoSelectJobTTL        = 30 * time.Minute
)

type autoSelectReq struct {
	Sites []string `json:"sites"`
	// MaxNodes — опциональный потолок; 0/omit = все узлы подписки (до hard cap).
	MaxNodes int `json:"maxNodes"`
}

type autoSelectAttempt struct {
	NodeID    string  `json:"nodeId"`
	Name      string  `json:"name"`
	LatencyMs float64 `json:"latencyMs"`
	Weight    int     `json:"weight"`
	OK        bool    `json:"ok"`
	Error     string  `json:"error,omitempty"`
}

type autoSelectStartResp struct {
	JobID          string `json:"jobId"`
	SubscriptionID string `json:"subscriptionId"`
	Total          int    `json:"total"`
	Phase          string `json:"phase"`
}

type autoSelectJobStatus struct {
	JobID            string              `json:"jobId"`
	SubscriptionID   string              `json:"subscriptionId"`
	Phase            string              `json:"phase"` // pinging|probing|done|error
	Checked          int                 `json:"checked"`
	Total            int                 `json:"total"`
	CurrentName      string              `json:"currentName,omitempty"`
	Attempts         []autoSelectAttempt `json:"attempts"`
	Finished         bool                `json:"finished"`
	SelectedNodeID   string              `json:"selectedNodeId,omitempty"`
	SelectedNodeName string              `json:"selectedNodeName,omitempty"`
	Reconnected      bool                `json:"reconnected"`
	Message          string              `json:"message,omitempty"`
	Error            string              `json:"error,omitempty"`
}

type autoSelectJob struct {
	mu        sync.Mutex
	createdAt time.Time
	status    autoSelectJobStatus
}

func (j *autoSelectJob) snapshot() autoSelectJobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.status
	out.Attempts = append([]autoSelectAttempt(nil), j.status.Attempts...)
	return out
}

func (j *autoSelectJob) patch(fn func(*autoSelectJobStatus)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.status)
}

// NormalizeAutoSelectSites — чистка списка сайтов: trim, https-префикс, дедуп, лимит.
func NormalizeAutoSelectSites(sites []string) []string {
	out := make([]string, 0, len(sites))
	seen := map[string]struct{}{}
	for _, s := range sites {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
			s = "https://" + s
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if len(out) >= autoSelectMaxSites {
			break
		}
	}
	if len(out) == 0 {
		return []string{autoSelectDefaultSites}
	}
	return out
}

// resolveAutoSelectNodeLimit: maxNodes<=0 → все узлы (с hard cap); иначе min(maxNodes, total, hard cap).
func resolveAutoSelectNodeLimit(maxNodes, total int) int {
	if total < 0 {
		total = 0
	}
	limit := total
	if maxNodes > 0 && maxNodes < limit {
		limit = maxNodes
	}
	if limit > autoSelectHardCapNodes {
		limit = autoSelectHardCapNodes
	}
	return limit
}

// AdjustAutoSelectWeight: успех +1, провал −1. nil map безопасен.
func AdjustAutoSelectWeight(weights map[string]int, nodeID string, ok bool) map[string]int {
	if nodeID == "" {
		return weights
	}
	if weights == nil {
		weights = make(map[string]int)
	}
	if ok {
		weights[nodeID]++
	} else {
		weights[nodeID]--
	}
	return weights
}

func (s *Server) autoSelectNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req autoSelectReq
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	st := s.Store.Get()
	nodes, err := s.loadNodes(r, id, st)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	sites := NormalizeAutoSelectSites(req.Sites)
	orderedLimit := resolveAutoSelectNodeLimit(req.MaxNodes, len(nodes))
	jobID := uuid.NewString()
	job := &autoSelectJob{
		createdAt: time.Now(),
		status: autoSelectJobStatus{
			JobID:          jobID,
			SubscriptionID: id,
			Phase:          "pinging",
			Total:          orderedLimit,
			Attempts:       []autoSelectAttempt{},
		},
	}
	s.autoSelectJobs.Store(jobID, job)
	go s.runAutoSelectJob(job, id, nodes, sites, orderedLimit)

	writeJSON(w, autoSelectStartResp{
		JobID:          jobID,
		SubscriptionID: id,
		Total:          orderedLimit,
		Phase:          "pinging",
	})
}

func (s *Server) autoSelectNodeStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	v, ok := s.autoSelectJobs.Load(jobID)
	if !ok {
		http.Error(w, "job not found", 404)
		return
	}
	job := v.(*autoSelectJob)
	st := job.snapshot()
	// Подписка в URL должна совпадать с job (защита от чужого jobId).
	if subID := chi.URLParam(r, "id"); subID != "" && subID != st.SubscriptionID {
		http.Error(w, "job not found", 404)
		return
	}
	writeJSON(w, st)
}

func (s *Server) runAutoSelectJob(job *autoSelectJob, subID string, nodes []subscription.Node, sites []string, orderedLimit int) {
	defer s.pruneAutoSelectJobs()

	ctx, cancel := context.WithTimeout(context.Background(), autoSelectOverallBudget)
	defer cancel()

	st := s.Store.Get()
	weights := map[string]int{}
	for i := range st.Subscriptions {
		if st.Subscriptions[i].ID == subID && st.Subscriptions[i].NodeAutoSelectWeights != nil {
			for k, v := range st.Subscriptions[i].NodeAutoSelectWeights {
				weights[k] = v
			}
			break
		}
	}

	job.patch(func(st *autoSelectJobStatus) { st.Phase = "pinging" })

	pingCtx, pingCancel := context.WithTimeout(ctx, 45*time.Second)
	results := ping.TCPBatch(pingCtx, st.MainInterface, nodes, 24, ping.BatchOptions{
		PreferICMP: s.SingBox.Running(),
	})
	pingCancel()
	byID := make(map[string]ping.Result, len(results))
	for _, res := range results {
		byID[res.NodeID] = res
	}
	ordered := ping.OrderByWeightThenPing(nodes, results, weights)
	if len(ordered) > orderedLimit {
		ordered = ordered[:orderedLimit]
	}
	job.patch(func(st *autoSelectJobStatus) {
		st.Total = len(ordered)
		st.Phase = "probing"
	})

	probeCfg := filepath.Join(st.DataDir, "probe", "sing-box.json")
	if err := singbox.WriteProbeConfig(probeCfg, ordered, st); err != nil {
		job.patch(func(st *autoSelectJobStatus) {
			st.Phase = "error"
			st.Finished = true
			st.Error = err.Error()
		})
		return
	}
	probeMgr := singbox.NewManager(st.SingBoxPath, probeCfg)
	probeMgr.SetProbeMode()
	if err := probeMgr.Start(ctx); err != nil {
		job.patch(func(st *autoSelectJobStatus) {
			st.Phase = "error"
			st.Finished = true
			st.Error = err.Error()
		})
		return
	}
	defer func() { _ = probeMgr.Stop() }()

	waitCtx, waitCancel := context.WithTimeout(ctx, 15*time.Second)
	err := singbox.WaitClashAPI(waitCtx, singbox.ProbeClashAPIAddr)
	waitCancel()
	if err != nil {
		job.patch(func(st *autoSelectJobStatus) {
			st.Phase = "error"
			st.Finished = true
			st.Error = err.Error()
		})
		return
	}

	var chosen *subscription.Node
	for i, n := range ordered {
		if ctx.Err() != nil {
			break
		}
		w := weights[n.ID]
		job.patch(func(st *autoSelectJobStatus) {
			st.CurrentName = n.Name
			st.Checked = i // ещё не завершили этот узел
		})

		attempt := autoSelectAttempt{NodeID: n.ID, Name: n.Name, Weight: w}
		if res, ok := byID[n.ID]; ok {
			attempt.LatencyMs = res.LatencyMs
		}
		if err := singbox.ClashSelectProxy(ctx, singbox.ProbeClashAPIAddr, "proxy", singbox.ProbeNodeTag(i)); err != nil {
			attempt.Error = err.Error()
			weights = AdjustAutoSelectWeight(weights, n.ID, false)
			attempt.Weight = weights[n.ID]
			s.persistAutoSelectWeights(subID, weights)
			job.patch(func(st *autoSelectJobStatus) {
				st.Attempts = append(st.Attempts, attempt)
				st.Checked = i + 1
			})
			continue
		}
		select {
		case <-ctx.Done():
		case <-time.After(200 * time.Millisecond):
		}
		if ctx.Err() != nil {
			break
		}
		ok, siteErr := checkSitesViaProxy(ctx, sites)
		attempt.OK = ok
		if siteErr != nil {
			attempt.Error = siteErr.Error()
		}
		weights = AdjustAutoSelectWeight(weights, n.ID, ok)
		attempt.Weight = weights[n.ID]
		s.persistAutoSelectWeights(subID, weights)
		job.patch(func(st *autoSelectJobStatus) {
			st.Attempts = append(st.Attempts, attempt)
			st.Checked = i + 1
		})
		if ok {
			nn := n
			chosen = &nn
			break
		}
	}
	_ = probeMgr.Stop()

	if chosen == nil {
		msg := fmt.Sprintf(
			"ни один из %d проверенных серверов не открыл проверяемые сайты — попробуйте другие сайты или обновите подписку",
			job.snapshot().Checked)
		job.patch(func(st *autoSelectJobStatus) {
			st.Phase = "done"
			st.Finished = true
			st.Message = msg
			st.CurrentName = ""
		})
		return
	}

	_ = s.Store.Update(func(cur *config.Settings) {
		for i := range cur.Subscriptions {
			if cur.Subscriptions[i].ID != subID {
				continue
			}
			cur.Subscriptions[i].SelectedNodeID = chosen.ID
			if cur.Subscriptions[i].NodeSelectCounts == nil {
				cur.Subscriptions[i].NodeSelectCounts = make(map[string]int)
			}
			cur.Subscriptions[i].NodeSelectCounts[chosen.ID]++
			cur.Subscriptions[i].NodeAutoSelectWeights = copyIntMap(weights)
		}
		cur.PersonalVPN.ActiveSubscriptionID = subID
	})

	reconnected := false
	st = s.Store.Get()
	if st.PersonalVPN.Enabled && singbox.HostReady(st.SingBoxPath) {
		if err := s.ensureRouter(ctx); err != nil {
			job.patch(func(st *autoSelectJobStatus) {
				st.Phase = "error"
				st.Finished = true
				st.SelectedNodeID = chosen.ID
				st.SelectedNodeName = chosen.Name
				st.Error = fmt.Sprintf("сервер выбран, но переприменить личный VPN не удалось: %v", err)
				st.CurrentName = ""
			})
			return
		}
		reconnected = true
	}
	job.patch(func(st *autoSelectJobStatus) {
		st.Phase = "done"
		st.Finished = true
		st.SelectedNodeID = chosen.ID
		st.SelectedNodeName = chosen.Name
		st.Reconnected = reconnected
		st.CurrentName = ""
	})
}

func (s *Server) persistAutoSelectWeights(subID string, weights map[string]int) {
	_ = s.Store.Update(func(cur *config.Settings) {
		for i := range cur.Subscriptions {
			if cur.Subscriptions[i].ID == subID {
				cur.Subscriptions[i].NodeAutoSelectWeights = copyIntMap(weights)
				return
			}
		}
	})
}

func copyIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Server) pruneAutoSelectJobs() {
	cutoff := time.Now().Add(-autoSelectJobTTL)
	s.autoSelectJobs.Range(func(key, value any) bool {
		job := value.(*autoSelectJob)
		if job.createdAt.Before(cutoff) {
			s.autoSelectJobs.Delete(key)
		}
		return true
	})
}

// checkSitesViaProxy проверяет сайты последовательно через пробный прокси;
// первый же недоступный сайт прерывает проверку (fail fast).
func checkSitesViaProxy(ctx context.Context, sites []string) (bool, error) {
	for _, site := range sites {
		siteCtx, cancel := context.WithTimeout(ctx, autoSelectSiteTimeout)
		report := probe.CheckSite(siteCtx, site,
			[]config.RoutePath{config.RoutePersonal},
			map[config.RoutePath]string{config.RoutePersonal: singbox.ProbeProxyURL},
			nil)
		cancel()
		res := probe.ResultForPath(report, config.RoutePersonal)
		if res == nil || !res.Available {
			if res != nil && res.Error != "" {
				return false, fmt.Errorf("%s: %s", site, res.Error)
			}
			return false, fmt.Errorf("%s недоступен", site)
		}
	}
	return true, nil
}
