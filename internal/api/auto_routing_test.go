package api

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/probe"
	"github.com/Valden92/routebox/internal/singbox"
)

func routingSettingsFixture(t *testing.T, enabled bool) *Server {
	t.Helper()
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Update(func(st *config.Settings) {
		st.AutoDetectRouting = enabled
		st.DomainRules = []config.DomainRule{{ID: "saved", Pattern: "saved.test", Path: config.RoutePersonal, Source: "auto", Enabled: true}}
	}); err != nil {
		t.Fatal(err)
	}
	s := NewTestServer(store, singbox.NewManager("unused-test-binary", filepath.Join(t.TempDir(), "config.json")))
	s.siteReport = func(_ context.Context, host string) probe.SiteReport {
		return probe.SiteReport{URL: host, Results: []probe.PathResult{{Path: config.RouteDirect, Available: true}}}
	}
	return s
}

func setBackgroundLearning(t *testing.T, s *Server, enabled bool) {
	t.Helper()
	if err := s.Store.Update(func(st *config.Settings) { st.AutoDetectRouting = enabled }); err != nil {
		t.Fatal(err)
	}
}

func TestAutoRoutingDisabledSkipsAllBackgroundLearning(t *testing.T) {
	s := routingSettingsFixture(t, false)
	before := s.Store.Get().DomainRules
	s.siteReport = func(context.Context, string) probe.SiteReport {
		t.Fatal("disabled learning started a site probe")
		return probe.SiteReport{}
	}
	for _, host := range []string{"new.test", "saved.test"} {
		resp, err := s.autoLearnHost(context.Background(), host, true, true)
		if err != nil || resp.Changed || resp.Reapplied {
			t.Fatalf("active learning: %+v, %v", resp, err)
		}
		changed, err := s.passiveLearnWorkHost(host)
		if err != nil || changed {
			t.Fatalf("passive learning: changed=%v, err=%v", changed, err)
		}
		if s.shouldAutoCheckObservedHost(host) {
			t.Fatal("disabled observer accepted a host")
		}
	}
	// A disabled tick must return before consulting the manager, Clash API, NM or history.
	s.SingBox = nil
	s.observeTraffic()
	if !reflect.DeepEqual(before, s.Store.Get().DomainRules) || len(s.observedHosts) != 0 {
		t.Fatal("disabled observer changed rules or reserved hosts")
	}
}

func TestExplicitAutoCheckWorksWithBackgroundLearningOff(t *testing.T) {
	s := routingSettingsFixture(t, false)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/rules/auto-check", strings.NewReader(`{"url":"manual.test"}`)))
	if w.Code != 200 || len(s.Store.Get().DomainRules) != 2 {
		t.Fatalf("manual check: status=%d, body=%s", w.Code, w.Body.String())
	}
	if s.Store.Get().AutoDetectRouting {
		t.Fatal("explicit check enabled the background observer")
	}
}

func TestInFlightAutoProbeCannotWriteAfterDisable(t *testing.T) {
	for _, host := range []string{"new.test", "saved.test"} {
		t.Run(host, func(t *testing.T) {
			s := routingSettingsFixture(t, true)
			before := s.Store.Get().DomainRules
			started, resume := make(chan struct{}), make(chan struct{})
			s.siteReport = func(_ context.Context, host string) probe.SiteReport {
				close(started)
				<-resume
				return probe.SiteReport{URL: host, Results: []probe.PathResult{{Path: config.RouteDirect, Available: true}}}
			}
			done := make(chan error, 1)
			go func() {
				_, err := s.autoLearnHost(context.Background(), host, true, true)
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("fixture probe did not start")
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("PUT", "/api/settings", strings.NewReader(`{"autoDetectRouting":false}`)))
			close(resume)
			if w.Code != 200 {
				t.Fatalf("disable: %d %s", w.Code, w.Body.String())
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, s.Store.Get().DomainRules) {
				t.Fatal("in-flight result changed rules after disabling")
			}
		})
	}
}

func TestBackgroundLearningCanResume(t *testing.T) {
	s := routingSettingsFixture(t, false)
	if s.shouldAutoCheckObservedHost("new.test") {
		t.Fatal("disabled host accepted")
	}
	setBackgroundLearning(t, s, true)
	if !s.shouldAutoCheckObservedHost("new.test") {
		t.Fatal("disabled observer left a throttle entry")
	}
	resp, err := s.autoLearnHost(context.Background(), "new.test", false, true)
	if err != nil || !resp.Changed {
		t.Fatalf("enabled active learning: %+v, %v", resp, err)
	}
	changed, err := s.passiveLearnWorkHost("work.test")
	if err != nil || !changed {
		t.Fatalf("enabled passive learning: %v, %v", changed, err)
	}
}

func TestCancelledBackgroundProbeDoesNotChangeRules(t *testing.T) {
	s := routingSettingsFixture(t, true)
	before := s.Store.Get().DomainRules
	ctx, cancel := context.WithCancel(context.Background())
	s.siteReport = func(context.Context, string) probe.SiteReport {
		cancel()
		return probe.SiteReport{Results: []probe.PathResult{{Path: config.RouteDirect, Available: true}}}
	}
	if _, err := s.autoLearnHost(ctx, "saved.test", false, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Store.Get().DomainRules) {
		t.Fatal("canceled probe changed a rule")
	}
}
