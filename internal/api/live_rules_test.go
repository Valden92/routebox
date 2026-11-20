package api

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func liveRuleFixture(t *testing.T) (*liveRouterRules, config.Settings) {
	t.Helper()
	st := config.DefaultSettings(t.TempDir())
	st.PersonalVPN.Enabled = true
	node := &subscription.Node{RawURI: "hy2://fixture@gateway.provider.test:443?pinSHA256=legacy-pin"}
	l, err := prepareLiveRouterRules(st.SingBoxConfigPath, node, st, singbox.RouterNetworkSnapshot{MainInterface: "fixture-main"})
	if err != nil {
		t.Fatal(err)
	}
	return l, st
}

func readLiveFiles(t *testing.T, l *liveRouterRules) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(l.dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name()] = body
	}
	return result
}

func assertLiveFiles(t *testing.T, l *liveRouterRules, want map[string][]byte) {
	t.Helper()
	for name, body := range readLiveFiles(t, l) {
		if !bytes.Equal(body, want[name]) {
			t.Fatalf("active rule file changed unexpectedly: %s", name)
		}
	}
}

func TestRulePublicationDoesNotReplaceTransportAndRepeatedAddIsNoop(t *testing.T) {
	l, st := liveRuleFixture(t)
	st.DomainRules = []config.DomainRule{{Pattern: "routebox-check.invalid", Path: config.RouteDirect, Source: "manual", Enabled: true}}
	st.AppRules = []config.AppRule{{ProcessName: "fixture-browser", Path: config.RoutePersonal, Enabled: true}}
	checks := 0
	validate := func(_ context.Context, path string) error {
		checks++
		if filepath.Dir(path) == l.dir {
			t.Fatal("validation touched live files")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return singbox.ValidateConfigBytes(body, false)
	}
	version, changed, err := l.publish(context.Background(), st, validate)
	if err != nil || !changed || version == "" || checks != 1 {
		t.Fatalf("publish: changed=%v err=%v checks=%d", changed, err, checks)
	}
	body, err := os.ReadFile(l.configPath)
	if err != nil || !bytes.Equal(body, l.core) {
		t.Fatal("core config was replaced")
	}
	before := readLiveFiles(t, l)
	for range 5 {
		next, changed, err := l.publish(context.Background(), st, validate)
		if err != nil || changed || next != version || checks != 1 {
			t.Fatalf("repeat changed core rules: changed=%v err=%v", changed, err)
		}
	}
	assertLiveFiles(t, l, before)
	// Deletion, enable/disable and route changes use the same restart-free path.
	st.DomainRules = nil
	st.AppRules[0].Enabled = false
	if _, changed, err := l.publish(context.Background(), st, validate); err != nil || !changed {
		t.Fatalf("delete/disable: changed=%v err=%v", changed, err)
	}
}

func TestRulePublicationFailurePreservesLiveFiles(t *testing.T) {
	for _, kind := range []string{"validation", "cancellation", "transport drift", "core file drift", "core drift during validation", "missing rules", "partial rename"} {
		t.Run(kind, func(t *testing.T) {
			l, st := liveRuleFixture(t)
			before := readLiveFiles(t, l)
			st.DomainRules = []config.DomainRule{{Pattern: "routebox-check.invalid", Path: config.RoutePersonal, Source: "manual", Enabled: true}}
			// Both domain sets change, so a late rename failure exercises restoration.
			st.DomainRules = append(st.DomainRules, config.DomainRule{Pattern: "direct.invalid", Path: config.RouteDirect, Source: "manual", Enabled: true})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			validate := func(_ context.Context, path string) error {
				switch kind {
				case "validation":
					return errors.New("rejected")
				case "cancellation":
					cancel()
				case "core drift during validation":
					return os.WriteFile(l.configPath, []byte("changed"), 0o600)
				case "partial rename":
					return os.Remove(filepath.Join(filepath.Dir(path), "proxy-domains.json"))
				}
				return nil
			}
			switch kind {
			case "transport drift":
				st.MainInterface = "changed-main"
			case "core file drift":
				if err := os.WriteFile(l.configPath, []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing rules":
				if err := os.Remove(filepath.Join(l.dir, "work-domains.json")); err != nil {
					t.Fatal(err)
				}
			}
			_, changed, err := l.publish(ctx, st, validate)
			if err == nil || changed {
				t.Fatalf("failure was accepted: changed=%v err=%v", changed, err)
			}
			assertLiveFiles(t, l, before)
		})
	}
}

func TestRulePublicationDoesNotClaimRuntimeAcknowledgement(t *testing.T) {
	l, st := liveRuleFixture(t)
	now := time.Now()
	tracker := configApplyTracker{state: routerConfigurationState{Phase: "applied", AppliedVersion: configVersion(l.core), LastSuccessfulAt: &now}, settingsKnown: true, appliedKey: routingSettingsKey(st)}
	st.DomainRules = []config.DomainRule{{Pattern: "routebox-check.invalid", Path: config.RouteDirect, Enabled: true}}
	tracker.recordRulePublication("rules_changed", "rules-version", true, nil)
	state := tracker.snapshot(st, true)
	if state.Phase != "rules_published" || state.PublishedRulesVersion != "rules-version" || state.MatchesSavedSettings != nil || !state.LastSuccessfulAt.Equal(now) {
		t.Fatalf("publication claimed a confirmed restart: %+v", state)
	}
	tracker.recordRulePublication("rules_changed", "rules-version", false, nil)
	if state := tracker.snapshot(st, true); !state.Unchanged || state.Phase != "rules_published" {
		t.Fatalf("no-op: %+v", state)
	}
	tracker.recordRulePublication("rules_changed", "", false, errors.New("failure"))
	if state := tracker.snapshot(st, true); state.Phase != "rejected" || state.AppliedVersion != configVersion(l.core) {
		t.Fatalf("failure discarded core: %+v", state)
	}
}
