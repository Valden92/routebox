package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShutdownCancelsAndWaitsForProbeCleanup(t *testing.T) {
	server := &Server{}
	job := newAutoSelectJob("fixture", 1)
	server.autoSelectJobs.Store("fixture", job)
	stopped := make(chan struct{})
	go func() { server.StopRouter(); close(stopped) }()
	select {
	case <-job.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel job")
	}
	select {
	case <-stopped:
		t.Fatal("shutdown returned before probe cleanup")
	default:
	}
	close(job.done)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not await probe completion")
	}
	server.StopRouter() // repeated shutdown does not close/cancel twice
}

func TestShutdownCannotResurrectRouterOrStartNewProbe(t *testing.T) {
	// Nil manager/store prove rejected paths cannot read host state or mutate config.
	server := &Server{}
	server.StopRouter()
	if changed, err := server.ensureRouterResult(context.Background()); err == nil || changed {
		t.Fatalf("shutdown started full apply: %v", err)
	}
	if changed, err := server.publishLiveRules(context.Background(), "rules_changed"); err == nil || changed {
		t.Fatalf("shutdown published rules: %v", err)
	}
	response := httptest.NewRecorder()
	server.autoSelectNode(response, httptest.NewRequest(http.MethodPost, "/api/subscriptions/fixture/auto-select", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("shutdown accepted job: %d", response.Code)
	}
}

func TestShutdownCancelsProbeBeforeWaitingForRuntimeLock(t *testing.T) {
	server := &Server{}
	job := newAutoSelectJob("fixture", 1)
	server.autoSelectJobs.Store("fixture", job)
	server.runtimeMu.RLock()
	stopped := make(chan struct{})
	go func() { server.StopRouter(); close(stopped) }()
	select {
	case <-job.ctx.Done():
	case <-time.After(time.Second):
		server.runtimeMu.RUnlock()
		close(job.done)
		t.Fatal("probe was not canceled before waiting for runtime")
	}
	close(job.done)
	server.runtimeMu.RUnlock()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown deadlocked with runtime request")
	}
}
