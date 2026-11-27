package singbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestBlockRulesWinForAllAvailableNetworks(t *testing.T) {
	for _, personal := range []bool{false, true} {
		for _, work := range []bool{false, true} {
			t.Run(fmt.Sprintf("personal=%v/work=%v", personal, work), func(t *testing.T) {
				st := config.DefaultSettings(t.TempDir())
				st.PersonalVPN.Enabled = personal
				st.DomainRules = []config.DomainRule{
					{Pattern: "api.example.test", Path: config.RouteDirect, Source: "manual", Enabled: true},
					{Pattern: "*.example.test", Path: config.RoutePersonal, Source: "auto", Enabled: true},
					{Pattern: "*.example.test", Path: config.RouteBlock, Source: "manual", Enabled: true},
					{Pattern: "only.test", Path: config.RouteBlock, Source: "manual", Enabled: true},
					{Pattern: "only.test", Path: config.RouteWork, Source: "manual", Enabled: true},
					{Pattern: "disabled.test", Path: config.RouteBlock, Enabled: false},
					{Pattern: "*.пример.рф", Path: config.RouteBlock, Enabled: true},
					{Pattern: "*.other.test", Path: config.RouteDirect, Source: "manual", Enabled: true},
				}
				files, err := singbox.LiveRuleFiles(st, personal, work)
				if err != nil {
					t.Fatal(err)
				}
				for _, host := range []string{"example.test", "api.example.test", "sub.api.example.test", "only.test", "api.xn--e1afmkfd.xn--p1ai"} {
					var matches []string
					for _, tag := range []string{"block", "direct", "proxy", "work"} {
						var set struct {
							Rules []map[string]any `json:"rules"`
						}
						if err = json.Unmarshal(files[tag+"-domains.json"], &set); err != nil {
							t.Fatal(err)
						}
						for _, r := range set.Rules {
							if matchHeadless(t, r, host, "") {
								matches = append(matches, tag)
								break
							}
						}
					}
					if len(matches) != 1 || matches[0] != "block" {
						t.Fatalf("%s matched %v", host, matches)
					}
				}
				var deny struct {
					Rules []map[string]any `json:"rules"`
				}
				if err = json.Unmarshal(files["block-domains.json"], &deny); err != nil {
					t.Fatal(err)
				}
				for _, host := range []string{"sub.only.test", "disabled.test", "notexample.test", "good.other.test"} {
					for _, r := range deny.Rules {
						if matchHeadless(t, r, host, "") {
							t.Fatalf("overblocked %s", host)
						}
					}
				}
				snapshot := singbox.RouterNetworkSnapshot{MainInterface: "fixture-main", SystemVPNUp: work, SystemInterface: "tun0", SystemDNS: "10.23.0.53", WorkRoutes: []string{"10.0.0.0/8"}}
				node := &subscription.Node{Host: "api.example.test", RawURI: "hy2://fixture@api.example.test:443"}
				candidate := filepath.Join(st.DataDir, "static.json")
				if err = singbox.WriteRouterConfigWithSnapshot(candidate, node, st, "tun100", snapshot); err != nil {
					t.Fatal(err)
				}
				checkBlockConfigOrder(t, candidate, false)
				if err = singbox.WriteLiveRouterConfig(candidate, node, st, "tun100", snapshot, st.DataDir); err != nil {
					t.Fatal(err)
				}
				checkBlockConfigOrder(t, candidate, true)
				before, err := os.ReadFile(candidate)
				if err != nil {
					t.Fatal(err)
				}
				st.DomainRules = nil
				if err = singbox.WriteLiveRouterConfig(candidate, node, st, "tun100", snapshot, st.DataDir); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Fatal("adding/removing deny changes core config")
				}
			})
		}
	}
}

func checkBlockConfigOrder(t *testing.T, path string, live bool) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
		DNS struct {
			Rules []map[string]any `json:"rules"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Route.Rules[0]["action"] != "sniff" || cfg.Route.Rules[1]["action"] != "hijack-dns" {
		t.Fatal("lost sniff/DNS interception")
	}
	for _, r := range []map[string]any{cfg.Route.Rules[2], cfg.DNS.Rules[0]} {
		if r["action"] != "reject" || r["outbound"] != nil || r["server"] != nil {
			t.Fatalf("deny was routed: %+v", r)
		}
		for _, key := range []string{"network", "ip_version", "query_type"} {
			if r[key] != nil {
				t.Fatalf("deny limited to %s", key)
			}
		}
		if live && r["rule_set"].([]any)[0] != "block-domains" {
			t.Fatal("DNS and traffic do not share deny set")
		}
	}
}

// Explicit opt-in only: check parses fixtures, never starts TUN, listeners or a VPN.
func TestBlockConfigurationWithCoreCheck(t *testing.T) {
	binary := os.Getenv("ROUTEBOX_CHECK_BINARY")
	if binary == "" {
		t.Skip("set ROUTEBOX_CHECK_BINARY to validate with sing-box check")
	}
	for _, work := range []bool{false, true} {
		st := config.DefaultSettings(t.TempDir())
		st.PersonalVPN.Enabled = true
		st.DomainRules = []config.DomainRule{{Pattern: "*.ads.test", Path: config.RouteBlock, Source: "manual", Enabled: true}}
		files, err := singbox.LiveRuleFiles(st, true, work)
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(st.DataDir, name), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		path := filepath.Join(st.DataDir, "sing-box.json")
		snapshot := singbox.RouterNetworkSnapshot{MainInterface: "lo", SystemVPNUp: work, SystemInterface: "tun0", SystemDNS: "10.23.0.53"}
		node := &subscription.Node{Host: "gateway.provider.test", RawURI: "hy2://fixture@gateway.provider.test:443"}
		if err := singbox.WriteLiveRouterConfig(path, node, st, "tun100", snapshot, st.DataDir); err != nil {
			t.Fatal(err)
		}
		if err := singbox.ValidateConfigContext(context.Background(), binary, path); err != nil {
			t.Fatal(err)
		}
	}
}
