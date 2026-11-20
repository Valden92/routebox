package singbox_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

// Interpret the documented headless-rule boolean operators, independently of
// the compiler, and check that exactly one outbound wins after grouping.
func matchHeadless(t *testing.T, rule map[string]any, host, process string) bool {
	t.Helper()
	var matches bool
	if rule["type"] == "logical" {
		matches = rule["mode"] == "and"
		for _, raw := range rule["rules"].([]any) {
			child := matchHeadless(t, raw.(map[string]any), host, process)
			if rule["mode"] == "and" {
				matches = matches && child
			} else {
				matches = matches || child
			}
		}
	} else {
		for key, value := range rule {
			if key == "invert" {
				continue
			}
			for _, raw := range value.([]any) {
				v := raw.(string)
				switch key {
				case "domain":
					matches = matches || host == v
				case "domain_suffix":
					matches = matches || host == v || strings.HasSuffix(host, "."+v)
				case "process_name":
					matches = matches || process == v
				default:
					t.Fatalf("unsupported headless field %s", key)
				}
			}
		}
	}
	if rule["invert"] == true {
		matches = !matches
	}
	return matches
}

func TestLiveRuleSetsPreserveDomainAndApplicationPrecedence(t *testing.T) {
	st := config.DefaultSettings(t.TempDir())
	st.DomainRules = []config.DomainRule{
		{Pattern: "*.example.test", Path: config.RoutePersonal, Source: "auto", Enabled: true},
		{Pattern: "*.corp.example.test", Path: config.RouteWork, Source: "manual", Enabled: true},
		{Pattern: "api.corp.example.test", Path: config.RouteDirect, Source: "manual", Enabled: true},
		{Pattern: "*.manual.test", Path: config.RoutePersonal, Source: "manual", Enabled: true},
		{Pattern: "auto.manual.test", Path: config.RouteDirect, Source: "auto", Enabled: true},
		{Pattern: "disabled.example.test", Path: config.RouteDirect, Source: "manual", Enabled: false},
	}
	st.AppRules = []config.AppRule{
		{ProcessName: "browser", Path: config.RoutePersonal, Enabled: true},
		{ProcessName: "browser", Path: config.RouteDirect, Enabled: true},
		{ProcessName: "editor", Path: config.RouteWork, Enabled: true},
	}
	for _, availability := range []struct {
		name           string
		personal, work bool
	}{
		{"all", true, true}, {"no personal", false, true}, {"no work", true, false}, {"direct only", false, false},
	} {
		t.Run(availability.name, func(t *testing.T) {
			files, err := singbox.LiveRuleFiles(st, availability.personal, availability.work)
			if err != nil {
				t.Fatal(err)
			}
			cases := []struct{ kind, host, process, want string }{
				{"domains", "api.corp.example.test", "", "direct"},
				{"domains", "corp.example.test", "", "work"},
				{"domains", "other.example.test", "", "proxy"},
				{"domains", "example.test", "", "proxy"},
				{"domains", "auto.manual.test", "", "proxy"},
				{"domains", "disabled.example.test", "", "proxy"},
				{"domains", "unrelated.test", "", ""},
				{"apps", "", "browser", "proxy"},
				{"apps", "", "editor", "work"},
				{"apps", "", "routebox-metadata", ""},
				{"apps", "", "unconfigured", ""},
			}
			for _, tc := range cases {
				want := tc.want
				if want == "work" && !availability.work {
					if tc.kind == "domains" && availability.personal {
						want = "proxy"
					} else {
						want = ""
					}
				}
				if want == "proxy" && !availability.personal {
					if tc.process == "browser" || tc.host == "auto.manual.test" {
						want = "direct"
					} else {
						want = ""
					}
				}
				var targets []string
				for _, path := range []string{"direct", "proxy", "work"} {
					var set struct {
						Rules []map[string]any `json:"rules"`
					}
					if err := json.Unmarshal(files[path+"-"+tc.kind+".json"], &set); err != nil {
						t.Fatal(err)
					}
					for _, rule := range set.Rules {
						if matchHeadless(t, rule, tc.host, tc.process) {
							targets = append(targets, path)
							break
						}
					}
				}
				if len(targets) > 1 || strings.Join(targets, "") != want {
					t.Fatalf("%s %s %s: matches=%v want=%s", tc.kind, tc.host, tc.process, targets, want)
				}
			}
		})
	}
}

func TestLiveConfigSharesDomainSetsWithDNSAndPreservesCoexist(t *testing.T) {
	for _, work := range []bool{false, true} {
		st := config.DefaultSettings(t.TempDir())
		st.PersonalVPN.Enabled = true
		snapshot := singbox.RouterNetworkSnapshot{SystemVPNUp: work, SystemInterface: "tun0", SystemDNS: "10.23.0.53", MainInterface: "fixture-main", WorkRoutes: []string{"10.30.0.0/16"}}
		path := filepath.Join(t.TempDir(), "sing-box.json")
		dir := filepath.Join(t.TempDir(), "rules")
		node := &subscription.Node{RawURI: "hy2://fixture@gateway.provider.test:443?pinSHA256=legacy-pin"}
		if err := singbox.WriteLiveRouterConfig(path, node, st, "tun100", snapshot, dir); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := singbox.ValidateConfigBytes(body, work); err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Route struct {
				Rules []map[string]any `json:"rules"`
				Sets  []map[string]any `json:"rule_set"`
			} `json:"route"`
			DNS struct {
				Rules []map[string]any `json:"rules"`
			} `json:"dns"`
		}
		if err := json.Unmarshal(body, &cfg); err != nil {
			t.Fatal(err)
		}
		count := 4
		if work {
			count = 6
		}
		if len(cfg.Route.Sets) != count {
			t.Fatalf("sets=%d", len(cfg.Route.Sets))
		}
		for _, r := range cfg.DNS.Rules {
			if tags, ok := r["rule_set"].([]any); ok {
				found := false
				for _, traffic := range cfg.Route.Rules {
					if v, ok := traffic["rule_set"].([]any); ok && v[0] == tags[0] {
						found = true
					}
				}
				if !found {
					t.Fatal("DNS does not share traffic rule-set")
				}
			}
		}
	}
}
