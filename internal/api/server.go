package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"

	"github.com/Valden92/routebox/internal/apps"
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/network"
	"github.com/Valden92/routebox/internal/nm"
	"github.com/Valden92/routebox/internal/ping"
	"github.com/Valden92/routebox/internal/probe"
	"github.com/Valden92/routebox/internal/routing"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

type Server struct {
	Store          *config.Store
	SingBox        *singbox.Manager
	router         chi.Router
	refresh        *refreshScheduler
	autoMu         sync.Mutex
	observedHosts  map[string]time.Time
	autoSelectJobs sync.Map // jobId → *autoSelectJob
}

func NewServer(store *config.Store, sb *singbox.Manager) *Server {
	s := &Server{Store: store, SingBox: sb, refresh: newRefreshScheduler(store), observedHosts: map[string]time.Time{}}
	s.routes()
	s.refresh.Start()
	s.startTrafficObserver()
	s.startSystemVPNWatcher()
	go func() {
		if !s.shouldStartRouterOnLaunch() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.ensureRouter(ctx)
	}()
	return s
}

// NewTestServer — HTTP API без фоновых воркеров и автозапуска router (для unit/httptest).
func NewTestServer(store *config.Store, sb *singbox.Manager) *Server {
	s := &Server{
		Store:         store,
		SingBox:       sb,
		refresh:       newRefreshScheduler(store),
		observedHosts: map[string]time.Time{},
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) shouldStartRouterOnLaunch() bool {
	st := s.Store.Get()
	if !singbox.SystemVPNUp(st) {
		return true
	}
	if st.PersonalVPN.Enabled && st.PersonalVPN.AutoConnect {
		return true
	}
	if st.PersonalVPN.Enabled && !st.PersonalVPN.AutoConnect {
		_ = s.Store.Update(func(cur *config.Settings) {
			cur.PersonalVPN.Enabled = false
		})
	}
	return false
}

func (s *Server) routes() {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))
	r.Get("/api/health", s.health)
	r.Get("/api/version", s.version)
	r.Get("/api/status", s.status)
	r.Get("/api/settings", s.getSettings)
	r.Put("/api/settings", s.putSettings)
	r.Post("/api/unlock", s.unlock)
	r.Post("/api/lock", s.lock)
	r.Route("/api/personal-vpn", func(r chi.Router) {
		r.Get("/readiness", s.personalReadinessHandler)
		r.Post("/connect", s.personalConnect)
		r.Post("/disconnect", s.personalDisconnect)
		r.Post("/reapply", s.personalReapply)
		r.Get("/status", s.personalStatus)
	})
	r.Route("/api/subscriptions", func(r chi.Router) {
		r.Get("/", s.listSubscriptions)
		r.Post("/", s.addSubscription)
		r.Post("/delete", s.deleteSubscriptionBody)
		r.Post("/{id}/refresh", s.refreshSubscription)
		r.Get("/{id}/nodes", s.listNodes)
		r.Post("/{id}/ping", s.pingNodes)
		r.Post("/{id}/select", s.selectNode)
		r.Post("/{id}/activate", s.activateSubscription)
		r.Post("/{id}/auto-select", s.autoSelectNode)
		r.Get("/{id}/auto-select/{jobId}", s.autoSelectNodeStatus)
		r.Put("/{id}", s.updateSubscription)
		r.Delete("/{id}", s.deleteSubscription)
	})
	r.Route("/api/rules", func(r chi.Router) {
		r.Get("/", s.listRules)
		r.Post("/", s.addRule)
		r.Post("/auto-check", s.autoCheckRule)
		r.Put("/{id}", s.updateRule)
		r.Delete("/{id}", s.deleteRule)
		r.Post("/suggest", s.suggestRule)
	})
	r.Get("/api/apps", s.listApps)
	r.Post("/api/probe/site", s.probeSite)
	s.router = r
}

const apiVersion = 5

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"apiVersion": apiVersion,
		"features": []string{
			"system-vpn-readonly",
			"personal-vpn-errors",
			"sing-box-recover",
			"auto-routing-observer",
			"subscription-import",
			"openvpn-import",
			"clash-import",
			"qr-import",
			"node-auto-select",
			"tls-fragment",
		},
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st := s.Store.Get()
	ctx := r.Context()
	internet := network.CheckInternet(ctx, st.MainInterface)
	sys := nm.Status(ctx, st.SystemVPN.NMConnectionID)
	ready := s.personalReadiness()
	personalActive := st.PersonalVPN.Enabled && ready.Configured && s.SingBox.Running()
	personal := map[string]any{
		"running":        personalActive,
		"enabled":        st.PersonalVPN.Enabled,
		"routingRunning": s.SingBox.Running(),
		"configured":     ready.Configured,
		"subscriptionId": st.PersonalVPN.ActiveSubscriptionID,
		"message":        ready.Message,
	}
	if sub := activeSubscription(st); sub != nil {
		personal["subscriptionName"] = sub.Name
		if sub.SelectedNodeID != "" {
			personal["selectedNodeId"] = sub.SelectedNodeID
		}
	}
	if err := s.SingBox.LastError(); err != "" && !s.SingBox.Running() {
		personal["error"] = err
	}
	personal["singBoxPath"] = singbox.ResolveBin(st.SingBoxPath)
	personal["tunCapable"] = singbox.HasTUNCapability(st.SingBoxPath)
	personal["hostConfigured"] = singbox.HostReady(st.SingBoxPath)
	personal["systemVpnActive"] = singbox.SystemVPNUp(st)
	personal["configMode"] = singbox.ConfigModeFor(st)
	fileMode := singbox.ConfigFileMode(st.SingBoxConfigPath)
	personal["configFileMode"] = fileMode
	wantMode := singbox.ConfigModeFor(st)
	personal["configStale"] = s.SingBox.Running() && fileMode != "" && fileMode != wantMode
	writeJSON(w, map[string]any{
		"internet":    internet,
		"systemVpn":   sys,
		"personalVpn": personal,
		"timestamp":   time.Now(),
	})
}

func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.Store.Get())
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var st config.Settings
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	err := s.Store.Update(func(cur *config.Settings) {
		if st.MainInterface != "" {
			cur.MainInterface = st.MainInterface
		}
		if st.SystemVPN.NMConnectionID != "" {
			cur.SystemVPN = st.SystemVPN
		}
		cur.PersonalVPN = st.PersonalVPN
		cur.DefaultPath = st.DefaultPath
		cur.AutoDetectRouting = st.AutoDetectRouting
		cur.DomainRules = st.DomainRules
		cur.AppRules = st.AppRules
		cur.Subscriptions = st.Subscriptions
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, s.Store.Get())
}

type unlockReq struct {
	Passphrase string `json:"passphrase"`
}

func (s *Server) unlock(w http.ResponseWriter, r *http.Request) {
	var req unlockReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.Store.Unlock(req.Passphrase); err != nil {
		http.Error(w, err.Error(), 401)
		return
	}
	writeJSON(w, map[string]string{"status": "unlocked"})
}

func (s *Server) lock(w http.ResponseWriter, r *http.Request) {
	var req unlockReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.Store.Lock(req.Passphrase); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]string{"status": "locked"})
}

func (s *Server) personalConnect(w http.ResponseWriter, r *http.Request) {
	ready := s.personalReadiness()
	if !ready.Configured {
		log.Printf("personal-connect: blocked readiness reason=%s msg=%q", ready.Reason, ready.Message)
		writeJSONError(w, http.StatusConflict, ready.Reason, ready.Message)
		return
	}
	st := s.Store.Get()
	if !singbox.HostReady(st.SingBoxPath) {
		writeJSONError(w, http.StatusPreconditionFailed, "host_not_ready",
			"Выполните в терминале: make sync (настройка TUN и NetworkManager)")
		return
	}
	node, err := s.selectedNode(st)
	if err != nil {
		log.Printf("personal-connect: selectedNode err=%v activeSub=%q", err, st.PersonalVPN.ActiveSubscriptionID)
		writeJSONError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	sysUp := singbox.SystemVPNUp(st)
	running := s.SingBox.Running()
	// При системном VPN SO_BINDTODEVICE на mainIface часто ломает TCP-пробу;
	// UDP-протоколы (hy2) вообще не слушают TCP. ICMP до host надёжнее.
	preferICMP := running || sysUp || ping.ProbeMode(node) == "icmp"
	log.Printf("personal-connect: sub=%s node=%s proto=%s net=%s %s:%d running=%v sysVPN=%v preferICMP=%v iface=%q mode=%s",
		st.PersonalVPN.ActiveSubscriptionID, node.Name, node.Protocol, node.Network,
		node.Host, node.Port, running, sysUp, preferICMP, st.MainInterface, ping.ProbeMode(node))
	if err := singbox.ProbeProxyReachable(node, st.MainInterface, preferICMP); err != nil {
		log.Printf("personal-connect: probe failed: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.Store.Update(func(st *config.Settings) {
		st.PersonalVPN.Enabled = true
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	st = s.Store.Get()
	if err := s.ensureRouter(r.Context()); err != nil {
		log.Printf("personal-connect: ensureRouter failed: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}
	log.Printf("personal-connect: ok mode=%s running=%v", singbox.ConfigModeFor(st), s.SingBox.Running())
	writeJSON(w, map[string]any{"running": true, "node": node, "configMode": singbox.ConfigModeFor(st)})
}

func (s *Server) personalReapply(w http.ResponseWriter, r *http.Request) {
	st := s.Store.Get()
	if err := s.ensureRouter(r.Context()); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"status": "reapplied", "configMode": singbox.ConfigModeFor(st)})
}

func (s *Server) ensureRouter(ctx context.Context) error {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	return s.ensureRouterLocked(ctx)
}

func (s *Server) ensureRouterLocked(ctx context.Context) error {
	st := s.Store.Get()
	if !singbox.HostReady(st.SingBoxPath) {
		return fmt.Errorf("host_not_ready: выполните make sync")
	}
	var node *subscription.Node
	if n, err := s.selectedNode(st); err == nil {
		node = &n
	} else {
		log.Printf("ensureRouter: no selected node (%v); personalEnabled=%v", err, st.PersonalVPN.Enabled)
	}
	nodeName := ""
	nodeProto := ""
	if node != nil {
		nodeName = node.Name
		nodeProto = node.Protocol
	}
	log.Printf("ensureRouter: personalEnabled=%v sysVPN=%v wasRunning=%v node=%q proto=%s",
		st.PersonalVPN.Enabled, singbox.SystemVPNUp(st), s.SingBox.Running(), nodeName, nodeProto)
	stoppedForReconfigure := false
	if s.SingBox.Running() {
		_ = s.SingBox.Stop()
		stoppedForReconfigure = true
	}
	if err := singbox.WriteRouterConfig(st.SingBoxConfigPath, node, st, "tun100"); err != nil {
		log.Printf("ensureRouter: WriteRouterConfig failed: %v", err)
		if stoppedForReconfigure {
			singbox.RestoreAfterPersonalVPNOn(st.MainInterface)
		}
		return err
	}
	if err := singbox.ValidateWrittenConfig(st.SingBoxConfigPath, st); err != nil {
		log.Printf("ensureRouter: ValidateWrittenConfig failed: %v", err)
		if stoppedForReconfigure {
			singbox.RestoreAfterPersonalVPNOn(st.MainInterface)
		}
		return err
	}
	singbox.SnapshotWorkVPNRoutes(singbox.SystemTunIface(st))
	if err := s.SingBox.Start(ctx); err != nil {
		log.Printf("ensureRouter: Start failed: %v", err)
		singbox.RestoreAfterPersonalVPNOn(st.MainInterface)
		return err
	}
	if !s.SingBox.Running() {
		singbox.RestoreAfterPersonalVPNOn(st.MainInterface)
		msg := s.SingBox.LastError()
		if msg == "" {
			msg = "sing-box не запустился"
		}
		log.Printf("ensureRouter: not running after Start: %s", msg)
		return fmt.Errorf("%s", msg)
	}
	log.Printf("ensureRouter: ok mode=%s", singbox.ConfigModeFor(st))
	return nil
}

func (s *Server) personalDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Update(func(st *config.Settings) {
		st.PersonalVPN.Enabled = false
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	st := s.Store.Get()
	sysUp := singbox.SystemVPNUp(st)
	wasRunning := s.SingBox.Running()
	log.Printf("personal-disconnect: sysVPN=%v wasRunning=%v (reconfigure without personal)", sysUp, wasRunning)
	// Раньше при sysVPN+running делали «deferred» и НЕ вызывали ensureRouter —
	// sing-box продолжал гонять личный outbound, а следующая подписка «магически»
	// начинала работать из‑за PreferICMP/тёплого процесса.
	if err := s.ensureRouter(r.Context()); err != nil {
		log.Printf("personal-disconnect: ensureRouter failed: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}
	status := "personal_disabled"
	if sysUp {
		status = "personal_disabled_deferred"
	}
	writeJSON(w, map[string]string{"status": status})
}

func (s *Server) personalStatus(w http.ResponseWriter, _ *http.Request) {
	st := s.Store.Get()
	ready := s.personalReadiness()
	personalActive := st.PersonalVPN.Enabled && ready.Configured && s.SingBox.Running()
	writeJSON(w, map[string]any{
		"running":         personalActive,
		"enabled":         st.PersonalVPN.Enabled,
		"routingRunning":  s.SingBox.Running(),
		"configured":      ready.Configured,
		"reason":          ready.Reason,
		"message":         ready.Message,
		"error":           s.SingBox.LastError(),
		"tunCapable":      singbox.HasTUNCapability(st.SingBoxPath),
		"hostConfigured":  singbox.HostReady(st.SingBoxPath),
		"systemVpnActive": singbox.SystemVPNUp(st),
		"configMode":      singbox.ConfigModeFor(st),
		"configFileMode":  singbox.ConfigFileMode(st.SingBoxConfigPath),
		"configStale":     s.SingBox.Running() && singbox.ConfigFileMode(st.SingBoxConfigPath) != singbox.ConfigModeFor(st),
	})
}

func (s *Server) listSubscriptions(w http.ResponseWriter, _ *http.Request) {
	st := s.Store.Get()
	out := make([]config.Subscription, len(st.Subscriptions))
	copy(out, st.Subscriptions)
	for i := range out {
		c, err := subscription.LoadCache(st.DataDir, out[i].ID)
		var nodes []subscription.Node
		if err == nil && len(c.Nodes) > 0 {
			nodes = subscription.FilterValidNodes(c.Nodes)
			if len(nodes) == 0 {
				nodes = c.Nodes
			}
			out[i].NodeCount = len(nodes)
			if strings.TrimSpace(out[i].ImportSummary) == "" {
				out[i].ImportSummary = subscription.EndpointLabel(nodes[0])
				if len(nodes) > 1 {
					out[i].ImportSummary += fmt.Sprintf(" · +%d", len(nodes)-1)
				}
			}
			selID, selCounts := subscription.RemapSelection(out[i].SelectedNodeID, out[i].NodeSelectCounts, nil, nodes)
			if selID != out[i].SelectedNodeID || selectionCountsChanged(out[i].NodeSelectCounts, selCounts) {
				subID := out[i].ID
				_ = s.Store.Update(func(cur *config.Settings) {
					for j := range cur.Subscriptions {
						if cur.Subscriptions[j].ID == subID {
							cur.Subscriptions[j].SelectedNodeID = selID
							cur.Subscriptions[j].NodeSelectCounts = selCounts
							return
						}
					}
				})
				out[i].SelectedNodeID = selID
				out[i].NodeSelectCounts = selCounts
			}
		}
		if src := subscription.NormalizeSource(out[i].Source); src != "" {
			out[i].Source = src
		} else {
			out[i].Source = subscription.InferSource(out[i].URL, nodes)
		}
	}
	writeJSON(w, out)
}

func selectionCountsChanged(a, b map[string]int) bool {
	if len(a) != len(b) {
		return true
	}
	for k, v := range a {
		if b[k] != v {
			return true
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			return true
		}
	}
	return false
}

type addSubReq struct {
	Name            string `json:"name"`
	URL             string `json:"url"`
	Source          string `json:"source"`  // url | text | uri | ovpn | clash (file на клиенте → text|ovpn|clash)
	Content         string `json:"content"` // тело для text/uri/ovpn
	Username        string `json:"username"`
	Password        string `json:"password"`
	RefreshInterval int    `json:"refreshIntervalMinutes"`
	AutoRefresh     bool   `json:"autoRefresh"`
}

func (s *Server) addSubscription(w http.ResponseWriter, r *http.Request) {
	var req addSubReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)
	req.Content = strings.TrimSpace(req.Content)
	req.Source = strings.ToLower(strings.TrimSpace(req.Source))
	req.Username = strings.TrimSpace(req.Username)
	if req.Name == "" {
		http.Error(w, "укажите название", 400)
		return
	}
	if req.Source == "" {
		if req.Content != "" {
			req.Source = "text"
		} else {
			req.Source = "url"
		}
	}

	var nodes []subscription.Node
	var fetchMeta subscription.Meta
	var skipped int
	switch req.Source {
	case "url":
		if !subscription.IsRemoteURL(req.URL) {
			http.Error(w, "укажите HTTP(S) URL подписки", 400)
			return
		}
		fetched, err := subscription.Fetch(r.Context(), req.URL)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		parsed, err := subscription.ParseBody(fetched.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		nodes = parsed
		fetchMeta = fetched.Meta
	case "text", "uri", "file":
		if req.Content == "" {
			http.Error(w, "вставьте содержимое подписки или share-ссылку", 400)
			return
		}
		if subscription.LooksLikeClash(req.Content) {
			parsed, skip, err := subscription.ParseClash([]byte(req.Content))
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			nodes = parsed
			skipped = skip
			req.Source = "clash"
		} else {
			parsed, err := subscription.ParseBody([]byte(req.Content))
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			nodes = parsed
		}
		req.URL = ""
		req.AutoRefresh = false
	case "ovpn":
		if req.Content == "" {
			http.Error(w, "вставьте содержимое .ovpn", 400)
			return
		}
		n, err := subscription.ParseOvpn(req.Content, req.Name)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if n.AuthUserPass {
			if req.Username == "" || req.Password == "" {
				http.Error(w, "для этого профиля нужны логин и пароль OpenVPN", 400)
				return
			}
			n.Username = req.Username
			n.Password = req.Password
		}
		nodes = []subscription.Node{n}
		req.URL = ""
		req.AutoRefresh = false
	case "clash":
		if req.Content == "" {
			http.Error(w, "вставьте Clash/Mihomo YAML (секция proxies)", 400)
			return
		}
		parsed, skip, err := subscription.ParseClash([]byte(req.Content))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		nodes = parsed
		skipped = skip
		req.URL = ""
		req.AutoRefresh = false
	default:
		http.Error(w, "неизвестный source (url|text|uri|ovpn|clash)", 400)
		return
	}

	nodes = subscription.FilterValidNodes(nodes)
	if len(nodes) == 0 {
		http.Error(w, "в подписке не найдено серверов (vless/ss/hysteria2/openvpn/…)", 400)
		return
	}

	id := uuid.NewString()
	mins := 60
	if req.RefreshInterval > 0 {
		mins = req.RefreshInterval
	}
	if !req.AutoRefresh || !subscription.IsRemoteURL(req.URL) {
		mins = 0
		req.AutoRefresh = false
	}
	sub := config.Subscription{
		ID:                     id,
		Name:                   req.Name,
		URL:                    req.URL,
		Source:                 subscription.NormalizeSource(req.Source),
		RefreshIntervalMinutes: mins,
		AutoRefresh:            req.AutoRefresh,
		Enabled:                true,
		CreatedAt:              time.Now(),
		LastRefresh:            time.Now(),
	}
	if sub.Source == "" {
		sub.Source = subscription.InferSource(req.URL, nodes)
	}
	applySubscriptionMeta(&sub, fetchMeta)
	if len(nodes) > 0 && !subscription.IsRemoteURL(req.URL) {
		sub.ImportSummary = subscription.EndpointLabel(nodes[0])
		if len(nodes) > 1 {
			sub.ImportSummary += fmt.Sprintf(" · +%d", len(nodes)-1)
		}
	}
	selID, selCounts := subscription.RemapSelection("", nil, nil, nodes)
	sub.SelectedNodeID = selID
	sub.NodeSelectCounts = selCounts
	if err := s.Store.Update(func(st *config.Settings) {
		st.Subscriptions = append(st.Subscriptions, sub)
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	st := s.Store.Get()
	if err := subscription.SaveCache(st.DataDir, id, nodes); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	sub.NodeCount = len(nodes)
	writeJSON(w, mergeSubImportResponse(sub, len(nodes), skipped))
}

func mergeSubImportResponse(sub config.Subscription, nodeCount, skipped int) map[string]any {
	b, _ := json.Marshal(sub)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{}
	}
	out["nodeCount"] = nodeCount
	if skipped > 0 {
		out["skipped"] = skipped
	}
	return out
}

func (s *Server) refreshSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st := s.Store.Get()
	var sub *config.Subscription
	for i := range st.Subscriptions {
		if st.Subscriptions[i].ID == id {
			sub = &st.Subscriptions[i]
			break
		}
	}
	if sub == nil {
		http.Error(w, "not found", 404)
		return
	}
	if !subscription.IsRemoteURL(sub.URL) {
		http.Error(w, "локальный импорт нельзя обновить по сети — добавьте URL или импортируйте заново", 400)
		return
	}
	fetched, err := subscription.Fetch(r.Context(), sub.URL)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	nodes, err := subscription.ParseBody(fetched.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	nodes = subscription.FilterValidNodes(nodes)
	if len(nodes) == 0 {
		http.Error(w, "в подписке не найдено серверов", 400)
		return
	}
	previous, _ := subscription.LoadCache(st.DataDir, id)
	_ = subscription.SaveCache(st.DataDir, id, nodes)
	s.reconcileSelectedNodeAfterRefresh(id, previous.Nodes, nodes)
	_ = s.Store.Update(func(cur *config.Settings) {
		for i := range cur.Subscriptions {
			if cur.Subscriptions[i].ID == id {
				cur.Subscriptions[i].LastRefresh = time.Now()
				applySubscriptionMeta(&cur.Subscriptions[i], fetched.Meta)
			}
		}
	})
	writeJSON(w, map[string]any{"count": len(nodes), "fetchedAt": time.Now()})
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st := s.Store.Get()
	nodes, err := s.loadNodes(r, id, st)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, nodes)
}

func (s *Server) loadNodes(r *http.Request, id string, st config.Settings) ([]subscription.Node, error) {
	c, err := subscription.LoadCache(st.DataDir, id)
	if err == nil && len(c.Nodes) > 0 {
		valid := subscription.FilterValidNodes(c.Nodes)
		if len(valid) > 0 {
			return valid, nil
		}
		// битый кэш (например HTML вместо подписки) — перекачаем
	}
	var sub *config.Subscription
	for i := range st.Subscriptions {
		if st.Subscriptions[i].ID == id {
			sub = &st.Subscriptions[i]
			break
		}
	}
	if sub == nil {
		return nil, os.ErrNotExist
	}
	if !subscription.IsRemoteURL(sub.URL) {
		return nil, fmt.Errorf("локальный импорт: кэш узлов пуст — импортируйте подписку заново")
	}
	fetched, err := subscription.Fetch(r.Context(), sub.URL)
	if err != nil {
		return nil, err
	}
	nodes, err := subscription.ParseBody(fetched.Body)
	if err != nil {
		return nil, err
	}
	nodes = subscription.FilterValidNodes(nodes)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("в подписке не найдено серверов")
	}
	_ = subscription.SaveCache(st.DataDir, id, nodes)
	_ = s.Store.Update(func(cur *config.Settings) {
		for i := range cur.Subscriptions {
			if cur.Subscriptions[i].ID == id {
				applySubscriptionMeta(&cur.Subscriptions[i], fetched.Meta)
				cur.Subscriptions[i].LastRefresh = time.Now()
			}
		}
	})
	return nodes, nil
}

type updateSubReq struct {
	AutoRefresh            *bool `json:"autoRefresh"`
	RefreshIntervalMinutes *int  `json:"refreshIntervalMinutes"`
}

func (s *Server) updateSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateSubReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	err := s.Store.Update(func(st *config.Settings) {
		for i := range st.Subscriptions {
			if st.Subscriptions[i].ID != id {
				continue
			}
			remote := subscription.IsRemoteURL(st.Subscriptions[i].URL)
			if !remote {
				st.Subscriptions[i].AutoRefresh = false
				st.Subscriptions[i].RefreshIntervalMinutes = 0
				return
			}
			if req.AutoRefresh != nil {
				st.Subscriptions[i].AutoRefresh = *req.AutoRefresh
				if !*req.AutoRefresh {
					st.Subscriptions[i].RefreshIntervalMinutes = 0
				}
			}
			if req.RefreshIntervalMinutes != nil {
				st.Subscriptions[i].RefreshIntervalMinutes = *req.RefreshIntervalMinutes
			}
			if st.Subscriptions[i].AutoRefresh && st.Subscriptions[i].RefreshIntervalMinutes <= 0 {
				st.Subscriptions[i].RefreshIntervalMinutes = 60
			}
			return
		}
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, sub := range s.Store.Get().Subscriptions {
		if sub.ID == id {
			writeJSON(w, sub)
			return
		}
	}
	http.Error(w, "not found", 404)
}

type deleteSubBodyReq struct {
	ID string `json:"id"`
}

func (s *Server) deleteSubscriptionBody(w http.ResponseWriter, r *http.Request) {
	var req deleteSubBodyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "id required", 400)
		return
	}
	s.deleteSubscriptionID(w, req.ID)
}

func (s *Server) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	s.deleteSubscriptionID(w, chi.URLParam(r, "id"))
}

func (s *Server) deleteSubscriptionID(w http.ResponseWriter, id string) {
	st := s.Store.Get()
	found := false
	wasActive := false
	err := s.Store.Update(func(cur *config.Settings) {
		var kept []config.Subscription
		for _, sub := range cur.Subscriptions {
			if sub.ID == id {
				found = true
				continue
			}
			kept = append(kept, sub)
		}
		cur.Subscriptions = kept
		if PersonalVPNTiedToSubscription(cur.PersonalVPN.ActiveSubscriptionID, id) {
			wasActive = true
			cur.PersonalVPN.ActiveSubscriptionID = ""
		}
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !found {
		http.Error(w, "subscription not found", 404)
		return
	}
	_ = subscription.DeleteCache(st.DataDir, id)
	// Раньше Stop вызывался при удалении ЛЮБОЙ подписки → рвал чужой активный VPN
	// и RestoreAfterPersonalVPNOn ломал маршруты/пинг до следующего connect.
	if wasActive && s.SingBox.Running() {
		_ = s.SingBox.Stop()
		singbox.RestoreAfterPersonalVPNOn(st.MainInterface)
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// PersonalVPNTiedToSubscription — удаление этой подписки должно гасить личный VPN.
func PersonalVPNTiedToSubscription(activeID, deletedID string) bool {
	return deletedID != "" && activeID == deletedID
}

func (s *Server) pingNodes(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st := s.Store.Get()
	c, err := subscription.LoadCache(st.DataDir, id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	results := ping.TCPBatch(r.Context(), st.MainInterface, c.Nodes, 24, ping.BatchOptions{
		// auto_redirect делает TCP dial «мгновенным» (~0 ms) — берём ICMP до host.
		PreferICMP: s.SingBox.Running(),
	})
	writeJSON(w, results)
}

type selectReq struct {
	NodeID string `json:"nodeId"`
}

func (s *Server) selectNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req selectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	_ = s.Store.Update(func(st *config.Settings) {
		for i := range st.Subscriptions {
			if st.Subscriptions[i].ID == id {
				st.Subscriptions[i].SelectedNodeID = req.NodeID
				if st.Subscriptions[i].NodeSelectCounts == nil {
					st.Subscriptions[i].NodeSelectCounts = make(map[string]int)
				}
				st.Subscriptions[i].NodeSelectCounts[req.NodeID]++
			}
		}
		st.PersonalVPN.ActiveSubscriptionID = id
	})
	writeJSON(w, map[string]string{"status": "selected"})
}

// activateSubscription делает подписку активной без смены выбранного узла.
func (s *Server) activateSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st := s.Store.Get()
	found := false
	for _, sub := range st.Subscriptions {
		if sub.ID == id {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "subscription not found", 404)
		return
	}
	if err := s.Store.Update(func(cur *config.Settings) {
		cur.PersonalVPN.ActiveSubscriptionID = id
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]string{"status": "activated", "subscriptionId": id})
}

func (s *Server) selectedNode(st config.Settings) (subscription.Node, error) {
	id := st.PersonalVPN.ActiveSubscriptionID
	if id == "" {
		return subscription.Node{}, os.ErrInvalid
	}
	var sub config.Subscription
	for _, x := range st.Subscriptions {
		if x.ID == id {
			sub = x
			break
		}
	}
	if sub.ID == "" {
		return subscription.Node{}, os.ErrNotExist
	}
	if sub.SelectedNodeID == "" {
		return subscription.Node{}, fmt.Errorf("сервер не выбран")
	}
	c, err := subscription.LoadCache(st.DataDir, id)
	if err != nil {
		return subscription.Node{}, err
	}
	if n, ok := subscription.FindNodeByID(c.Nodes, sub.SelectedNodeID); ok {
		return n, nil
	}
	return subscription.Node{}, fmt.Errorf("выбранный сервер исчез из подписки — выберите другой")
}

// reconcileSelectedNodeAfterRefresh сохраняет выбор, если узел остался или
// тот же endpoint (host:port:protocol) есть под новым id; иначе сбрасывает выбор.
func (s *Server) reconcileSelectedNodeAfterRefresh(subID string, previous, next []subscription.Node) {
	reconcileSelectedNode(s.Store, subID, previous, next)
}

func (s *Server) listRules(w http.ResponseWriter, _ *http.Request) {
	st := s.Store.Get()
	domains := st.DomainRules
	if domains == nil {
		domains = []config.DomainRule{}
	}
	apps := st.AppRules
	if apps == nil {
		apps = []config.AppRule{}
	}
	writeJSON(w, map[string]any{"domains": domains, "apps": apps})
}

type addRuleReq struct {
	Pattern     string           `json:"pattern"`
	Path        config.RoutePath `json:"path"`
	ProcessName string           `json:"processName,omitempty"`
}

func (s *Server) addRule(w http.ResponseWriter, r *http.Request) {
	var req addRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	id := uuid.NewString()
	err := s.Store.Update(func(st *config.Settings) {
		if req.ProcessName != "" {
			st.AppRules = append(st.AppRules, config.AppRule{
				ID: id, ProcessName: req.ProcessName, Path: req.Path, Enabled: true,
			})
			return
		}
		pattern := routing.NormalizeRulePattern(req.Pattern)
		if pattern == "" {
			pattern = req.Pattern
		}
		if idx := routing.FindDomainRuleIndex(st.DomainRules, pattern); idx >= 0 {
			st.DomainRules[idx].Pattern = pattern
			st.DomainRules[idx].Path = req.Path
			st.DomainRules[idx].Source = "manual"
			st.DomainRules[idx].Enabled = true
			st.DomainRules[idx].LastError = ""
			id = st.DomainRules[idx].ID
			return
		}
		st.DomainRules = append(st.DomainRules, config.DomainRule{
			ID: id, Pattern: pattern, Path: req.Path, Source: "manual", Enabled: true, CreatedAt: time.Now(),
		})
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	reapplied, err := s.reapplyPersonalIfRunning(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "reapply_failed", err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "reapplied": reapplied})
}

type updateRuleReq struct {
	Pattern string           `json:"pattern,omitempty"`
	Path    config.RoutePath `json:"path,omitempty"`
	Enabled *bool            `json:"enabled,omitempty"`
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var updated bool
	err := s.Store.Update(func(st *config.Settings) {
		for i := range st.DomainRules {
			if st.DomainRules[i].ID != id {
				continue
			}
			if req.Pattern != "" {
				st.DomainRules[i].Pattern = routing.NormalizeRulePattern(req.Pattern)
			}
			if req.Path != "" {
				st.DomainRules[i].Path = req.Path
				st.DomainRules[i].Source = "manual"
			}
			if req.Enabled != nil {
				st.DomainRules[i].Enabled = *req.Enabled
			}
			updated = true
			return
		}
		for i := range st.AppRules {
			if st.AppRules[i].ID != id {
				continue
			}
			if req.Path != "" {
				st.AppRules[i].Path = req.Path
			}
			if req.Enabled != nil {
				st.AppRules[i].Enabled = *req.Enabled
			}
			updated = true
			return
		}
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !updated {
		http.Error(w, "rule not found", 404)
		return
	}
	reapplied, err := s.reapplyPersonalIfRunning(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "reapply_failed", err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": "updated", "reapplied": reapplied})
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.Store.Update(func(st *config.Settings) {
		st.DomainRules = routing.FilterDomainRules(st.DomainRules, id)
		st.AppRules = routing.FilterAppRules(st.AppRules, id)
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	reapplied, err := s.reapplyPersonalIfRunning(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "reapply_failed", err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": "deleted", "reapplied": reapplied})
}

type suggestReq struct {
	Host string `json:"host"`
}

type autoCheckReq struct {
	URL string `json:"url"`
}

type autoCheckResp struct {
	Host          string            `json:"host"`
	Rule          config.DomainRule `json:"rule,omitempty"`
	Report        probe.SiteReport  `json:"report"`
	Changed       bool              `json:"changed"`
	Reapplied     bool              `json:"reapplied"`
	SuggestedPath config.RoutePath  `json:"suggestedPath,omitempty"`
	Message       string            `json:"message,omitempty"`
}

func (s *Server) autoCheckRule(w http.ResponseWriter, r *http.Request) {
	var req autoCheckReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	resp, err := s.autoLearnHost(r.Context(), req.URL, true)
	if err != nil {
		if err == errEmptyHost {
			http.Error(w, "empty host", 400)
			return
		}
		if strings.Contains(err.Error(), "reapply") {
			writeJSONError(w, http.StatusInternalServerError, "reapply_failed", err.Error())
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) suggestRule(w http.ResponseWriter, r *http.Request) {
	var req suggestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	st := s.Store.Get()
	sys := nm.Status(r.Context(), st.SystemVPN.NMConnectionID)
	path := routing.SuggestPath(req.Host, sys.Routes, routing.DefaultCorpSuffixes())
	writeJSON(w, map[string]any{"host": req.Host, "suggestedPath": path})
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	list, err := apps.Scan(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, list)
}

type probeReq struct {
	URL string `json:"url"`
}

func (s *Server) probeSite(w http.ResponseWriter, r *http.Request) {
	var req probeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, s.checkSiteReport(r.Context(), req.URL))
}

func (s *Server) checkSiteReport(ctx context.Context, rawURL string) probe.SiteReport {
	st := s.Store.Get()
	sys := nm.Status(ctx, st.SystemVPN.NMConnectionID)
	sysIface := sys.Interface
	if sysIface == "" {
		sysIface = "tun0"
	}
	skip := map[config.RoutePath]string{}
	if !sys.Connected {
		skip[config.RouteWork] = "Системный VPN выключен"
	}
	if !s.SingBox.Running() || !st.PersonalVPN.Enabled {
		skip[config.RoutePersonal] = "Личный VPN выключен"
	}
	bind := probe.BindMap(st.MainInterface, sysIface, singbox.PersonalProbeProxyURL)
	return probe.CheckSite(ctx, rawURL, probe.DefaultPaths(), bind, skip)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) reapplyPersonalIfRunning(ctx context.Context) (bool, error) {
	if !s.SingBox.Running() {
		return false, nil
	}
	return true, s.ensureRouter(ctx)
}

var errEmptyHost = errors.New("empty host")

func (s *Server) autoLearnHost(ctx context.Context, raw string, allowReapply bool) (autoCheckResp, error) {
	host := routing.NormalizeRulePattern(raw)
	if host == "" {
		return autoCheckResp{}, errEmptyHost
	}
	report := s.checkSiteReport(ctx, host)
	best, hasBest := probe.BestAvailablePath(report)
	now := time.Now()
	resp := autoCheckResp{Host: host, Report: report}
	if hasBest {
		resp.SuggestedPath = best
	}

	var routeChanged bool
	var reapplyNeeded bool
	var rule config.DomainRule
	var ruleFound bool
	err := s.Store.Update(func(cur *config.Settings) {
		idx := routing.FindDomainRuleIndex(cur.DomainRules, host)
		if idx >= 0 {
			ruleFound = true
			cur.DomainRules[idx].LastCheckedAt = now
			cur.DomainRules[idx].CheckCount++
			current := probe.ResultForPath(report, cur.DomainRules[idx].Path)
			switch {
			case current != nil && current.Skipped:
				cur.DomainRules[idx].LastError = current.SkipReason
			case current != nil && current.Available:
				cur.DomainRules[idx].LastSuccessAt = now
				cur.DomainRules[idx].LastError = ""
			case cur.DomainRules[idx].Source != "auto":
				if current != nil && current.Error != "" {
					cur.DomainRules[idx].LastError = current.Error
				} else {
					cur.DomainRules[idx].LastError = "ручное правило не сработало"
				}
			case hasBest:
				oldPath := cur.DomainRules[idx].Path
				if oldPath != best {
					cur.DomainRules[idx].Path = best
					routeChanged = true
					reapplyNeeded = oldPath != config.RouteDirect || best != config.RouteDirect
				}
				cur.DomainRules[idx].LastSuccessAt = now
				cur.DomainRules[idx].LastError = ""
			default:
				cur.DomainRules[idx].LastError = "домен недоступен по доступным путям"
			}
			rule = cur.DomainRules[idx]
			return
		}
		if !hasBest {
			return
		}
		ruleFound = true
		routeChanged = true
		reapplyNeeded = best != config.RouteDirect
		rule = config.DomainRule{
			ID:            uuid.NewString(),
			Pattern:       host,
			Path:          best,
			Source:        "auto",
			Enabled:       true,
			CreatedAt:     now,
			LastCheckedAt: now,
			LastSuccessAt: now,
			CheckCount:    1,
		}
		cur.DomainRules = append(cur.DomainRules, rule)
	})
	if err != nil {
		return resp, err
	}
	resp.Rule = rule
	resp.Changed = routeChanged
	if !ruleFound {
		resp.Message = "Домен недоступен по доступным сейчас путям"
		return resp, nil
	}
	if reapplyNeeded && allowReapply {
		reapplied, err := s.reapplyPersonalIfRunning(ctx)
		if err != nil {
			return resp, fmt.Errorf("reapply failed: %w", err)
		}
		resp.Reapplied = reapplied
	}
	return resp, nil
}

func (s *Server) passiveLearnWorkHost(raw string) (bool, error) {
	host := routing.NormalizeRulePattern(raw)
	if host == "" {
		return false, errEmptyHost
	}
	now := time.Now()
	changed := false
	err := s.Store.Update(func(cur *config.Settings) {
		idx := routing.FindDomainRuleIndex(cur.DomainRules, host)
		if idx >= 0 {
			r := &cur.DomainRules[idx]
			if r.Source != "auto" {
				return
			}
			r.LastCheckedAt = now
			r.LastSuccessAt = now
			r.LastError = ""
			r.CheckCount++
			if r.Path != config.RouteWork {
				r.Path = config.RouteWork
				changed = true
			}
			return
		}
		cur.DomainRules = append(cur.DomainRules, config.DomainRule{
			ID:            uuid.NewString(),
			Pattern:       host,
			Path:          config.RouteWork,
			Source:        "auto",
			Enabled:       true,
			CreatedAt:     now,
			LastCheckedAt: now,
			LastSuccessAt: now,
			CheckCount:    1,
		})
		changed = true
	})
	return changed, err
}

type clashConnectionsResp struct {
	Connections []struct {
		Metadata struct {
			Host          string `json:"host"`
			DestinationIP string `json:"destinationIP"`
		} `json:"metadata"`
	} `json:"connections"`
}

func (s *Server) startTrafficObserver() {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.observeTraffic()
		}
	}()
}

func (s *Server) startSystemVPNWatcher() {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.reapplyIfConfigModeChanged()
		}
	}()
}

func (s *Server) reapplyIfConfigModeChanged() {
	if !s.SingBox.Running() {
		return
	}
	st := s.Store.Get()
	wantMode := singbox.ConfigModeFor(st)
	fileMode := singbox.ConfigFileMode(st.SingBoxConfigPath)
	if fileMode == "" || fileMode == wantMode {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = s.ensureRouter(ctx)
}

func (s *Server) observeTraffic() {
	if !s.SingBox.Running() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	hosts, err := observedSingBoxHosts(ctx)
	cancel()
	if err != nil {
		hosts = nil
	}
	hosts = routing.UniqueHosts(hosts)
	checked := 0
	for _, host := range hosts {
		if !s.shouldAutoCheckObservedHost(host) {
			continue
		}
		checkCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		_, _ = s.autoLearnHost(checkCtx, host, true)
		cancel()
		checked++
		if checked >= 3 {
			return
		}
	}

	sys := nm.Status(context.Background(), s.Store.Get().SystemVPN.NMConnectionID)
	if sys.Connected {
		for _, host := range routing.UniqueHosts(observedBrowserHistoryHosts()) {
			if !routing.IsCorpHost(host) || !s.shouldAutoCheckObservedHost(host) {
				continue
			}
			_, _ = s.passiveLearnWorkHost(host)
			checked++
			if checked >= 3 {
				break
			}
		}
	}
}

func observedSingBoxHosts(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+singbox.ClashAPIAddr+"/connections", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data clashConnectionsResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var hosts []string
	for _, c := range data.Connections {
		host := routing.NormalizeObservedHost(c.Metadata.Host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts, nil
}

func observedBrowserHistoryHosts() []string {
	paths := browserHistoryPaths()
	if len(paths) == 0 {
		return nil
	}
	args := []string{"-c", `
import sqlite3, sys, time, urllib.parse
cutoff = int((time.time() - 120) * 1000000) + 11644473600000000
seen = set()
for path in sys.argv[1:]:
    try:
        con = sqlite3.connect(f"file:{path}?mode=ro&immutable=1", uri=True, timeout=0.2)
        rows = con.execute("select url from urls where last_visit_time >= ? order by last_visit_time desc limit 80", (cutoff,)).fetchall()
        con.close()
    except Exception:
        continue
    for (u,) in rows:
        host = urllib.parse.urlparse(u).hostname
        if host and host not in seen:
            seen.add(host)
            print(host)
`}
	args = append(args, paths...)
	out, err := exec.Command("python3", args...).Output()
	if err != nil {
		return nil
	}
	var hosts []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if host := routing.NormalizeObservedHost(line); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func browserHistoryPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	patterns := []string{
		filepath.Join(home, ".config", "google-chrome", "*", "History"),
		filepath.Join(home, ".config", "chromium", "*", "History"),
		filepath.Join(home, "snap", "chromium", "common", "chromium", "*", "History"),
		filepath.Join(home, ".var", "app", "*", "config", "*", "*", "History"),
	}
	var paths []string
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, p := range matches {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

func (s *Server) shouldAutoCheckObservedHost(host string) bool {
	const minInterval = 10 * time.Minute
	now := time.Now()
	st := s.Store.Get()
	if node, err := s.selectedNode(st); err == nil && strings.EqualFold(host, node.Host) {
		return false
	}
	if idx := routing.FindDomainRuleIndex(st.DomainRules, host); idx >= 0 {
		rule := st.DomainRules[idx]
		if rule.Source != "auto" {
			return false
		}
		if !rule.LastCheckedAt.IsZero() && now.Sub(rule.LastCheckedAt) < minInterval {
			return false
		}
	}
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if last, ok := s.observedHosts[host]; ok && now.Sub(last) < minInterval {
		return false
	}
	s.observedHosts[host] = now
	return true
}
