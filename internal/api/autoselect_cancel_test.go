package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Valden92/routebox/internal/config"
)

func autoSelectCancelRequest(t *testing.T, subID, jobID string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/subscriptions/"+subID+"/auto-select/"+jobID+"/cancel", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", subID)
	rctx.URLParams.Add("jobId", jobID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestAutoSelectCancelWaitsForProbeCleanup(t *testing.T) {
	s := &Server{}
	job := newAutoSelectTestJob(t, 312)
	s.autoSelectJobs.Store(job.status.JobID, job)
	s.autoSelectMu.Lock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			s.autoSelectMu.Unlock()
			close(job.done)
		})
	}
	t.Cleanup(release)
	w := httptest.NewRecorder()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		s.autoSelectNodeCancel(w, autoSelectCancelRequest(t, "test-sub", job.status.JobID))
	}()
	select {
	case <-job.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not reach the probe context")
	}
	select {
	case <-returned:
		t.Fatal("cancel returned before probe cleanup")
	default:
	}
	if s.autoSelectMu.TryLock() {
		s.autoSelectMu.Unlock()
		t.Fatal("probe lease was released before cleanup")
	}
	if !job.finishIfCanceled() {
		t.Fatal("worker did not acknowledge cancellation")
	}
	release()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not return after probe cleanup")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status: %d %s", w.Code, w.Body.String())
	}
	var status autoSelectJobStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Finished || status.Phase != "canceled" || status.SelectedNodeID != "" {
		t.Fatalf("wrong cancellation result: %+v", status)
	}
	if !s.autoSelectMu.TryLock() {
		t.Fatal("a new job cannot acquire the released probe lease")
	}
	s.autoSelectMu.Unlock()
	// Повторная отмена не должна ждать несуществующий процесс.
	w = httptest.NewRecorder()
	s.autoSelectNodeCancel(w, autoSelectCancelRequest(t, "test-sub", job.status.JobID))
	if w.Code != http.StatusOK {
		t.Fatalf("repeated cancel: %d", w.Code)
	}
}

func TestAutoSelectCancelValidatesSubscriptionAndJob(t *testing.T) {
	s := &Server{}
	job := newAutoSelectTestJob(t, 312)
	s.autoSelectJobs.Store(job.status.JobID, job)
	for _, tc := range []struct{ sub, job string }{
		{sub: "another-sub", job: job.status.JobID},
		{sub: "test-sub", job: "missing"},
	} {
		w := httptest.NewRecorder()
		s.autoSelectNodeCancel(w, autoSelectCancelRequest(t, tc.sub, tc.job))
		if w.Code != http.StatusNotFound || job.ctx.Err() != nil {
			t.Fatalf("foreign or unknown job affected active probe: status=%d ctx=%v", w.Code, job.ctx.Err())
		}
	}
}

func TestAutoSelectCancelDoesNotInterruptApply(t *testing.T) {
	s := &Server{}
	job := newAutoSelectTestJob(t, 312)
	s.autoSelectJobs.Store(job.status.JobID, job)
	if !job.beginApply() {
		t.Fatal("cannot enter apply phase")
	}
	w := httptest.NewRecorder()
	s.autoSelectNodeCancel(w, autoSelectCancelRequest(t, "test-sub", job.status.JobID))
	if w.Code != http.StatusConflict || job.ctx.Err() != nil {
		t.Fatal("cancellation interrupted a router reconfiguration")
	}
	job.patch(func(st *autoSelectJobStatus) {
		st.Phase = "done"
		st.Finished = true
		st.SelectedNodeID = "selected"
	})
	close(job.done)
	w = httptest.NewRecorder()
	s.autoSelectNodeCancel(w, autoSelectCancelRequest(t, "test-sub", job.status.JobID))
	if w.Code != http.StatusOK || job.snapshot().SelectedNodeID != "selected" {
		t.Fatal("closing a completed result changed the selected node")
	}
}

func TestAutoSelectCancellationWinsBeforeApply(t *testing.T) {
	job := newAutoSelectTestJob(t, 312)
	job.patch(func(st *autoSelectJobStatus) {
		st.Phase = "probing"
		st.Checked = 79
		st.CurrentName = "in-flight"
	})
	if !job.requestCancel() || job.beginApply() {
		t.Fatal("a canceled probe was allowed to change the selected node")
	}
	if !job.finishIfCanceled() {
		t.Fatal("canceled job did not finish")
	}
	st := job.snapshot()
	if st.Phase != "canceled" || !st.Finished || st.Checked != 79 || st.CurrentName != "" || st.Error != "" {
		t.Fatalf("cancellation was reported as a failed subscription: %+v", st)
	}
}

func TestAutoSelectCancelAndApplyAreMutuallyExclusive(t *testing.T) {
	for i := 0; i < 100; i++ {
		job := newAutoSelectTestJob(t, 312)
		start := make(chan struct{})
		canceled, applying := make(chan bool, 1), make(chan bool, 1)
		go func() { <-start; canceled <- job.requestCancel() }()
		go func() { <-start; applying <- job.beginApply() }()
		close(start)
		if accepted, applied := <-canceled, <-applying; accepted == applied {
			t.Fatalf("cancel and apply must have exactly one winner: cancel=%v apply=%v", accepted, applied)
		}
	}
}

func TestAutoSelectCancelSurvivesAbortedHTTPRequest(t *testing.T) {
	s := &Server{}
	job := newAutoSelectTestJob(t, 312)
	s.autoSelectJobs.Store(job.status.JobID, job)
	r := autoSelectCancelRequest(t, "test-sub", job.status.JobID)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	s.autoSelectNodeCancel(httptest.NewRecorder(), r.WithContext(ctx))
	if job.ctx.Err() == nil {
		t.Fatal("disconnecting the browser lost the stop request")
	}
}

func TestAutoSelectCancelRoute(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestServer(store, nil)
	job := newAutoSelectTestJob(t, 312)
	job.requestCancel()
	job.finishIfCanceled()
	close(job.done)
	s.autoSelectJobs.Store(job.status.JobID, job)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/subscriptions/test-sub/auto-select/"+job.status.JobID+"/cancel", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("cancel route is not registered: %d %s", w.Code, w.Body.String())
	}
}
