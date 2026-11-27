package routing_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/routing"
)

func TestNormalizeBlockPattern(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://ADS.Example.com/path", "ads.example.com"}, {"*.Example.com.", "*.example.com"},
		{"*.пример.рф", "*.xn--e1afmkfd.xn--p1ai"},
	} {
		got, err := routing.NormalizeBlockPattern(tc.raw)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %q %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{"", "*", "*.", "bad..example.com", "127.0.0.1", "[2001:db8::1]"} {
		if got, err := routing.NormalizeBlockPattern(raw); err == nil {
			t.Fatalf("accepted %q as %q", raw, got)
		}
	}
}
func TestBlockingRuleWinsRegardlessOfSourceAndSpecificAllow(t *testing.T) {
	rules := []config.DomainRule{
		{ID: "allow", Pattern: "api.example.com", Path: config.RouteDirect, Source: "manual", Enabled: true},
		{ID: "deny", Pattern: "*.example.com", Path: config.RouteBlock, Source: "manual", Enabled: true},
		{ID: "unicode", Pattern: "*.пример.рф", Path: config.RouteBlock, Enabled: true},
		{ID: "disabled", Pattern: "disabled.test", Path: config.RouteBlock},
	}
	for _, host := range []string{"example.com", "api.example.com", "a.b.example.com", "почта.пример.рф", "api.xn--e1afmkfd.xn--p1ai"} {
		if _, ok := routing.BlockingDomainRule(rules, host); !ok {
			t.Fatalf("not blocked: %s", host)
		}
	}
	for _, host := range []string{"notexample.com", "example.com.evil", "disabled.test", "other.test"} {
		if _, ok := routing.BlockingDomainRule(rules, host); ok {
			t.Fatalf("overblocked: %s", host)
		}
	}
	rules[1].Pattern = "api.example.com"
	if _, ok := routing.BlockingDomainRule(rules, "sub.api.example.com"); ok {
		t.Fatal("exact deny matched subdomain")
	}
}
