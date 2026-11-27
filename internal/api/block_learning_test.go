package api

import (
	"context"
	"reflect"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/probe"
)

func TestDenySkipsActiveAndPassiveLearning(t *testing.T) {
	s := routingSettingsFixture(t, true)
	if err := s.Store.Update(func(st *config.Settings) {
		st.DomainRules = []config.DomainRule{
			{ID: "auto", Pattern: "ads.example.test", Path: config.RouteDirect, Source: "auto", Enabled: true},
			{ID: "deny", Pattern: "*.example.test", Path: config.RouteBlock, Source: "manual", Enabled: true},
		}
	}); err != nil {
		t.Fatal(err)
	}
	before := s.Store.Get().DomainRules
	s.siteReport = func(context.Context, string) probe.SiteReport {
		t.Fatal("probed denied host")
		return probe.SiteReport{}
	}
	for _, host := range []string{"example.test", "ads.example.test", "new.example.test"} {
		r, err := s.autoLearnHost(context.Background(), host, true, false)
		if err != nil || r.Changed || r.Reapplied || r.Rule.ID != "deny" || r.Message == "" {
			t.Fatalf("active: %+v %v", r, err)
		}
		changed, err := s.passiveLearnWorkHost(host)
		if changed || err != nil {
			t.Fatalf("passive: %v %v", changed, err)
		}
	}
	if !reflect.DeepEqual(before, s.Store.Get().DomainRules) {
		t.Fatal("learning changed denied rules")
	}
}
func TestDenyAddedDuringProbeCannotBeOverwritten(t *testing.T) {
	s := routingSettingsFixture(t, true)
	s.siteReport = func(_ context.Context, host string) probe.SiteReport {
		if err := s.Store.Update(func(st *config.Settings) {
			st.DomainRules = append(st.DomainRules, config.DomainRule{ID: "deny", Pattern: "*.example.test", Path: config.RouteBlock, Source: "manual", Enabled: true})
		}); err != nil {
			t.Fatal(err)
		}
		return probe.SiteReport{URL: host, Results: []probe.PathResult{{Path: config.RouteDirect, Available: true}}}
	}
	r, err := s.autoLearnHost(context.Background(), "new.example.test", false, false)
	if err != nil || r.Changed || r.Rule.ID != "deny" || r.SuggestedPath != "" || len(s.Store.Get().DomainRules) != 2 {
		t.Fatalf("in-flight learning bypassed deny: %+v %v", r, err)
	}
}
