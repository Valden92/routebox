package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/ping"
	"github.com/Valden92/routebox/internal/subscription"
)

func newAutoSelectTestJob(t *testing.T, total int) *autoSelectJob {
	t.Helper()
	job := newAutoSelectJob("test-sub", total)
	t.Cleanup(job.cancel)
	return job
}

func TestAutoSelectJobHasNoOverallDeadline(t *testing.T) {
	job := newAutoSelectTestJob(t, 312)
	if deadline, exists := job.ctx.Deadline(); exists {
		t.Fatalf("full subscription scan has an overall deadline: %v", deadline)
	}
	if err := job.ctx.Err(); err != nil {
		t.Fatal(err)
	}
	job.cancel()
	if !errors.Is(job.ctx.Err(), context.Canceled) {
		t.Fatal("job context cannot be canceled independently of a site timeout")
	}
}

func TestProbeAutoSelectNodes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		total      int
		successAt  int
		wantChecks int
	}{
		{name: "all 312 fail", total: 312, successAt: -1, wantChecks: 312},
		{name: "no hidden 500 node limit", total: 1001, successAt: -1, wantChecks: 1001},
		{name: "first success", total: 312, successAt: 0, wantChecks: 1},
		{name: "success after the previous partial scan", total: 312, successAt: 200, wantChecks: 201},
		{name: "last server succeeds", total: 312, successAt: 311, wantChecks: 312},
		{name: "empty list", successAt: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := newAutoSelectTestJob(t, tc.total)
			nodes := make([]subscription.Node, tc.total)
			for i := range nodes {
				nodes[i] = subscription.Node{ID: fmt.Sprintf("node-%d", i), Name: fmt.Sprintf("Server %d", i)}
			}
			calls, saves := 0, 0
			var persisted map[string]int
			byID := map[string]ping.Result{"node-0": {LatencyMs: 23}}
			chosen, err := probeAutoSelectNodes(job, nodes, byID, nil, func(ctx context.Context, i int) (bool, error) {
				if _, exists := ctx.Deadline(); exists {
					t.Fatal("a fixed deadline was applied to the scan")
				}
				progress := job.snapshot()
				if progress.Checked != i || progress.CurrentName != nodes[i].Name {
					t.Fatalf("wrong in-flight progress: %+v", progress)
				}
				calls++
				if i == tc.successAt {
					return true, nil
				}
				// An individual site timeout must not abort subsequent servers.
				return false, context.DeadlineExceeded
			}, func(current map[string]int) {
				saves++
				persisted = current
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls != tc.wantChecks || saves != tc.wantChecks {
				t.Fatalf("checks=%d saves=%d want=%d", calls, saves, tc.wantChecks)
			}
			if tc.successAt < 0 {
				if chosen != nil {
					t.Fatalf("unexpected selected node: %s", chosen.ID)
				}
			} else if chosen == nil || chosen.ID != nodes[tc.successAt].ID {
				t.Fatal("did not select the first successful node in the ranked list")
			}
			status := job.snapshot()
			if status.Checked != tc.wantChecks || len(status.Attempts) != tc.wantChecks || status.Total != tc.total {
				t.Fatalf("wrong final counts: checked=%d attempts=%d total=%d", status.Checked, len(status.Attempts), status.Total)
			}
			for i, attempt := range status.Attempts {
				wantWeight := -1
				if i == tc.successAt {
					wantWeight = 1
				} else if attempt.Error != context.DeadlineExceeded.Error() {
					t.Fatalf("attempt %d lost its individual error", i)
				}
				if attempt.NodeID != nodes[i].ID || attempt.OK != (i == tc.successAt) ||
					attempt.Weight != wantWeight || persisted[attempt.NodeID] != wantWeight {
					t.Fatalf("wrong result or weight at attempt %d: %+v", i, attempt)
				}
			}
			if tc.wantChecks > 0 && status.Attempts[0].LatencyMs != 23 {
				t.Fatal("ping result was lost")
			}
		})
	}
}

func TestProbeAutoSelectNodesCancellation(t *testing.T) {
	for _, cancelBeforeStart := range []bool{false, true} {
		t.Run(fmt.Sprintf("before-start=%v", cancelBeforeStart), func(t *testing.T) {
			job := newAutoSelectTestJob(t, 3)
			if cancelBeforeStart {
				job.cancel()
			}
			calls, saves := 0, 0
			weights := map[string]int{"a": 2}
			chosen, err := probeAutoSelectNodes(job, []subscription.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
				nil, weights, func(context.Context, int) (bool, error) {
					calls++
					job.cancel()
					return false, context.Canceled
				}, func(map[string]int) { saves++ })
			if chosen != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation reported as an ordinary no-match: chosen=%v err=%v", chosen, err)
			}
			wantCalls := 1
			if cancelBeforeStart {
				wantCalls = 0
			}
			if calls != wantCalls || saves != 0 || weights["a"] != 2 || job.snapshot().Checked != 0 {
				t.Fatal("cancellation must not penalize or count an unfinished server")
			}
		})
	}
}

func TestPruneAutoSelectJobsPreservesActiveAndRecentResults(t *testing.T) {
	s := &Server{}
	active := newAutoSelectTestJob(t, 312)
	recent := newAutoSelectTestJob(t, 312)
	expired := newAutoSelectTestJob(t, 312)
	recent.patch(func(st *autoSelectJobStatus) { st.Finished = true })
	expired.patch(func(st *autoSelectJobStatus) { st.Finished = true })
	expired.finishedAt = time.Now().Add(-2 * autoSelectJobTTL)
	// Even an old timestamp must never make an active job expire.
	active.finishedAt = expired.finishedAt
	s.autoSelectJobs.Store("active", active)
	s.autoSelectJobs.Store("recent", recent)
	s.autoSelectJobs.Store("expired", expired)
	s.pruneAutoSelectJobs()
	for _, id := range []string{"active", "recent"} {
		if _, exists := s.autoSelectJobs.Load(id); !exists {
			t.Fatalf("job %q disappeared while active or shortly after finishing", id)
		}
	}
	if _, exists := s.autoSelectJobs.Load("expired"); exists {
		t.Fatal("expired result was retained")
	}
	if recent.finishedAt.IsZero() {
		t.Fatal("completion time was not recorded")
	}
	finishedAt := recent.finishedAt
	recent.patch(func(st *autoSelectJobStatus) { st.Message = "updated" })
	if !recent.finishedAt.Equal(finishedAt) {
		t.Fatal("result update reset the completion time")
	}
}

func TestAutoSelectNodeRejectsConcurrentProbe(t *testing.T) {
	s := &Server{}
	s.autoSelectMu.Lock()
	defer s.autoSelectMu.Unlock()
	w := httptest.NewRecorder()
	s.autoSelectNode(w, httptest.NewRequest(http.MethodPost, "/api/subscriptions/sub/auto-select", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "уже выполняется") {
		t.Fatalf("concurrent job was not rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestAutoSelectNodeReleasesProbeLockOnRequestError(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store}
	w := httptest.NewRecorder()
	s.autoSelectNode(w, httptest.NewRequest(http.MethodPost, "/api/subscriptions/missing/auto-select", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	if !s.autoSelectMu.TryLock() {
		t.Fatal("failed request permanently locked the probe")
	}
	s.autoSelectMu.Unlock()
}
