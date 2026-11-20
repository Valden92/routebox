package api

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestRealGeneratorKeepsNoopAcrossEquivalentNetworkSnapshots(t *testing.T) {
	f := newRollbackFixture(t, false, false)
	var tracker configApplyTracker
	st := config.DefaultSettings(t.TempDir())
	st.PersonalVPN.Enabled = true
	st.DomainRules = []config.DomainRule{{ID: "existing", Pattern: "routebox-check.invalid", Path: config.RouteDirect, Source: "manual", Enabled: true}}
	node := &subscription.Node{Host: "gateway.provider.test", RawURI: "hy2://fixture@gateway.provider.test:443?pinSHA256=legacy-pin"}
	snapshot := singbox.RouterNetworkSnapshot{
		SystemVPNUp: true, SystemInterface: "tun0", SystemDNS: "10.23.0.53", MainInterface: "fixture-main",
		WorkRoutes: []string{"10.40.0.0/16", "10.30.0.0/16"}, EndpointCIDRs: []string{"198.51.100.8/32", "192.0.2.8/32"},
	}
	prepare := func(path string) error {
		if err := singbox.WriteRouterConfigWithSnapshot(path, node, st, "tun100", snapshot); err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return singbox.ValidateConfigBytes(body, snapshot.SystemVPNUp)
	}
	if restarted, err := tracker.apply(context.Background(), st, f.path, "startup", prepare, f.runtime); err != nil || !restarted {
		t.Fatalf("fixture initial apply: restart=%v err=%v", restarted, err)
	}
	initial := tracker.snapshot(st, true)
	for i := range 6 {
		// DNS answer ordering and route listing order may change without a network change.
		slices.Reverse(snapshot.WorkRoutes)
		slices.Reverse(snapshot.EndpointCIDRs)
		beforeRoutes, beforeEndpoints := slices.Clone(snapshot.WorkRoutes), slices.Clone(snapshot.EndpointCIDRs)
		restarted, err := tracker.apply(context.Background(), st, f.path, "rules_changed", prepare, f.runtime)
		state := tracker.snapshot(st, true)
		if err != nil || restarted || !state.Unchanged || state.DesiredVersion != initial.AppliedVersion || state.AppliedVersion != initial.AppliedVersion || f.starts != 1 || f.stops != 0 {
			t.Fatalf("equivalent snapshot %d changed config: restart=%v err=%v starts=%d stops=%d unchanged=%v", i, restarted, err, f.starts, f.stops, state.Unchanged)
		}
		if !slices.Equal(snapshot.WorkRoutes, beforeRoutes) || !slices.Equal(snapshot.EndpointCIDRs, beforeEndpoints) {
			t.Fatal("generator mutated the captured snapshot")
		}
		if !state.LastSuccessfulAt.Equal(*initial.LastSuccessfulAt) {
			t.Fatal("no-op reported a new successful start")
		}
	}
	snapshot.EndpointCIDRs = append(snapshot.EndpointCIDRs, snapshot.EndpointCIDRs[0])
	snapshot.WorkRoutes = append(snapshot.WorkRoutes, snapshot.WorkRoutes[0])
	if restarted, err := tracker.apply(context.Background(), st, f.path, "rules_changed", prepare, f.runtime); err != nil || restarted {
		t.Fatalf("duplicate entries changed route-set semantics: restart=%v err=%v", restarted, err)
	}
	// A real route change still requires application.
	snapshot.WorkRoutes = append(snapshot.WorkRoutes, "10.50.0.0/16")
	if restarted, err := tracker.apply(context.Background(), st, f.path, "rules_changed", prepare, f.runtime); err != nil || !restarted || f.starts != 2 {
		t.Fatalf("actual network change ignored: restart=%v err=%v starts=%d", restarted, err, f.starts)
	}
}
