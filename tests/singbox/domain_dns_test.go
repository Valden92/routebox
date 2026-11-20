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

type domainConfig struct {
	DNS struct {
		Servers []map[string]any `json:"servers"`
		Rules   []map[string]any `json:"rules"`
		Final   string           `json:"final"`
	} `json:"dns"`
	Route struct {
		Rules []map[string]any `json:"rules"`
		Final string           `json:"final"`
	} `json:"route"`
}

func domainRuleFixture(pattern string, path config.RoutePath, source string) config.DomainRule {
	return config.DomainRule{Pattern: pattern, Path: path, Source: source, Enabled: true}
}

func domainConfigFixture(t *testing.T, rules []config.DomainRule, personal, work bool) domainConfig {
	t.Helper()
	st := config.DefaultSettings(t.TempDir())
	st.PersonalVPN.Enabled = personal
	st.DomainRules = rules
	snapshot := singbox.RouterNetworkSnapshot{SystemVPNUp: work, SystemInterface: "tun0", SystemDNS: "10.23.0.53", MainInterface: "test-main"}
	node := &subscription.Node{Host: "gateway.provider.test", RawURI: "hy2://fixture-secret@gateway.provider.test:443?pinSHA256=legacy-pin"}
	path := filepath.Join(st.DataDir, "candidate.json")
	if err := singbox.WriteRouterConfigWithSnapshot(path, node, st, "tun100", snapshot); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = singbox.ValidateConfigBytes(body, work); err != nil {
		t.Fatal(err)
	}
	var cfg domainConfig
	if err = json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	servers := map[string]bool{}
	for _, server := range cfg.DNS.Servers {
		servers[server["tag"].(string)] = true
	}
	for _, rule := range cfg.DNS.Rules {
		if target, ok := rule["server"].(string); ok && !servers[target] {
			t.Fatalf("DNS references missing server %q", target)
		}
	}
	return cfg
}

// Evaluate only domain matches, leaving CIDR/process/safety rules outside this test.
func firstDomainTarget(t *testing.T, rules []map[string]any, host, key, fallback string) string {
	t.Helper()
	for _, rule := range rules {
		for _, raw := range domainValues(rule, "domain") {
			if host == raw {
				return rule[key].(string)
			}
		}
		for _, raw := range domainValues(rule, "domain_suffix") {
			suffix := strings.TrimPrefix(raw, ".")
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return rule[key].(string)
			}
		}
	}
	return fallback
}

func domainValues(rule map[string]any, key string) []string {
	var result []string
	if values, ok := rule[key].([]any); ok {
		for _, value := range values {
			result = append(result, value.(string))
		}
	}
	return result
}

func TestDNSAndTrafficShareExplicitDomainPrecedence(t *testing.T) {
	direct := config.RouteDirect
	personal := config.RoutePersonal
	work := config.RouteWork
	for _, tc := range []struct {
		name           string
		rules          []config.DomainRule
		host, want     string
		personal, work bool
	}{
		{"manual direct exception", []config.DomainRule{domainRuleFixture("*.example.test", personal, "auto"), domainRuleFixture("api.example.test", direct, "manual")}, "api.example.test", "direct", true, false},
		{"manual direct wildcard", []config.DomainRule{domainRuleFixture("api.example.test", personal, "auto"), domainRuleFixture("*.example.test", direct, "manual")}, "api.example.test", "direct", true, false},
		{"manual work exception", []config.DomainRule{domainRuleFixture("*.example.test", personal, "auto"), domainRuleFixture("api.example.test", work, "manual")}, "api.example.test", "work", true, true},
		{"manual direct overrides work wildcard", []config.DomainRule{domainRuleFixture("*.example.test", work, "auto"), domainRuleFixture("api.example.test", direct, "manual")}, "api.example.test", "direct", true, true},
		{"manual personal exception", []config.DomainRule{domainRuleFixture("*.example.test", direct, "auto"), domainRuleFixture("api.example.test", personal, "manual")}, "api.example.test", "proxy", true, false},
		{"manual broader than auto", []config.DomainRule{domainRuleFixture("api.example.test", direct, "auto"), domainRuleFixture("*.example.test", personal, "manual")}, "api.example.test", "proxy", true, false},
		{"manual exact over manual mask", []config.DomainRule{domainRuleFixture("*.example.test", personal, "manual"), domainRuleFixture("api.example.test", direct, "manual")}, "api.example.test", "direct", true, false},
		{"same source specificity", []config.DomainRule{domainRuleFixture("*.example.test", personal, "auto"), domainRuleFixture("*.api.example.test", direct, "auto")}, "v1.api.example.test", "direct", true, false},
		{"duplicate pattern", []config.DomainRule{domainRuleFixture("api.example.test", personal, "auto"), domainRuleFixture("api.example.test", direct, "manual")}, "api.example.test", "direct", true, false},
		{"equal source last wins", []config.DomainRule{domainRuleFixture("api.example.test", personal, "manual"), domainRuleFixture("api.example.test", direct, "manual")}, "api.example.test", "direct", true, false},
		{"personal unavailable", []config.DomainRule{domainRuleFixture("api.example.test", personal, "manual")}, "api.example.test", "direct", false, false},
		{"work unavailable", []config.DomainRule{domainRuleFixture("api.example.test", work, "manual"), domainRuleFixture("*.example.test", personal, "auto")}, "api.example.test", "proxy", true, false},
		{"personal unavailable work fallback", []config.DomainRule{domainRuleFixture("api.example.test", personal, "manual"), domainRuleFixture("*.example.test", work, "auto")}, "api.example.test", "work", false, true},
		{"explicit ru before built in", []config.DomainRule{domainRuleFixture("news.ru", personal, "manual")}, "news.ru", "proxy", true, false},
		{"no suffix boundary crossing", []config.DomainRule{domainRuleFixture("*.example.test", personal, "manual")}, "notexample.test", "direct", true, false},
		{"legacy unknown path remains direct", []config.DomainRule{domainRuleFixture("api.example.test", config.RoutePath("legacy-unknown"), "manual"), domainRuleFixture("*.example.test", personal, "auto")}, "api.example.test", "direct", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := domainConfigFixture(t, tc.rules, tc.personal, tc.work)
			route := firstDomainTarget(t, cfg.Route.Rules, tc.host, "outbound", cfg.Route.Final)
			dns := firstDomainTarget(t, cfg.DNS.Rules, tc.host, "server", cfg.DNS.Final)
			if route != tc.want || dns != "dns-"+tc.want {
				t.Fatalf("host=%s route=%s DNS=%s want=%s/dns-%s", tc.host, route, dns, tc.want, tc.want)
			}
		})
	}
}

func TestDisabledDomainRuleCannotOverrideDNS(t *testing.T) {
	disabled := domainRuleFixture("api.example.test", config.RouteDirect, "manual")
	disabled.Enabled = false
	cfg := domainConfigFixture(t, []config.DomainRule{disabled, domainRuleFixture("*.example.test", config.RoutePersonal, "auto")}, true, false)
	if firstDomainTarget(t, cfg.DNS.Rules, "api.example.test", "server", cfg.DNS.Final) != "dns-proxy" || firstDomainTarget(t, cfg.Route.Rules, "api.example.test", "outbound", cfg.Route.Final) != "proxy" {
		t.Fatal("disabled exception affected DNS or routing")
	}
}

func TestBuiltInDNSDefaultsRemainAfterExplicitRules(t *testing.T) {
	cfg := domainConfigFixture(t, nil, true, true)
	for _, tc := range []struct{ host, want string }{{"wiki.ptsecurity.com", "dns-work"}, {"news.ru", "dns-direct"}, {"example.org", "dns-direct"}} {
		if got := firstDomainTarget(t, cfg.DNS.Rules, tc.host, "server", cfg.DNS.Final); got != tc.want {
			t.Fatalf("%s: DNS=%s want=%s", tc.host, got, tc.want)
		}
	}
}
