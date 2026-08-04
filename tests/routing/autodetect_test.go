package routing_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/routing"
)

func TestSuggestPath(t *testing.T) {
	corp := routing.DefaultCorpSuffixes()
	cases := []struct {
		host string
		want config.RoutePath
	}{
		{"", config.RouteDirect},
		{"localhost", config.RouteDirect},
		{"printer.local", config.RouteDirect},
		{"127.0.0.1", config.RouteDirect},
		{"10.0.0.5", config.RouteDirect},
		{"portal.ptsecurity.ru", config.RouteWork},
		{"x.ptsecurity.com", config.RouteWork},
		{"music.yandex.ru", config.RouteDirect},
		{"web.telegram.org", config.RoutePersonal},
		{"example.com", config.RoutePersonal},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			got := routing.SuggestPath(tc.host, nil, corp)
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestNormalizeRulePattern(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"*.ptsecurity.ru", "*.ptsecurity.ru"},
		{"HTTPS://Music.Yandex.RU/path", "music.yandex.ru"},
		{"music.yandex.ru", "music.yandex.ru"},
		{"https://music.yandex.ru:443/", "music.yandex.ru"},
	}
	for _, tc := range cases {
		if got := routing.NormalizeRulePattern(tc.in); got != tc.want {
			t.Fatalf("%q → %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsCorpHost(t *testing.T) {
	if !routing.IsCorpHost("sso.ptsecurity.ru") {
		t.Fatal("corp")
	}
	if routing.IsCorpHost("example.com") {
		t.Fatal("not corp")
	}
}

func TestNormalizeObservedHost(t *testing.T) {
	if got := routing.NormalizeObservedHost("https://A.Example.COM/x"); got != "a.example.com" {
		t.Fatalf("%q", got)
	}
	if routing.NormalizeObservedHost("8.8.8.8") != "" {
		t.Fatal("ip")
	}
	if routing.NormalizeObservedHost("foo.local") != "" {
		t.Fatal("local")
	}
	if routing.NormalizeObservedHost("single") != "" {
		t.Fatal("no dot")
	}
}

func TestUniqueHosts(t *testing.T) {
	got := routing.UniqueHosts([]string{
		"https://a.example.com",
		"A.Example.COM",
		"8.8.8.8",
		"",
		"b.example.com",
	})
	if len(got) != 2 || got[0] != "a.example.com" || got[1] != "b.example.com" {
		t.Fatalf("%v", got)
	}
}

func TestFindDomainRuleIndex(t *testing.T) {
	rules := []config.DomainRule{
		{Pattern: "exact.example.com"},
		{Pattern: "*.ptsecurity.ru"},
	}
	if i := routing.FindDomainRuleIndex(rules, "exact.example.com"); i != 0 {
		t.Fatalf("exact %d", i)
	}
	if i := routing.FindDomainRuleIndex(rules, "sso.ptsecurity.ru"); i != 1 {
		t.Fatalf("wild %d", i)
	}
	if i := routing.FindDomainRuleIndex(rules, "other.com"); i != -1 {
		t.Fatalf("miss %d", i)
	}
}

func TestFilterRules(t *testing.T) {
	domains := []config.DomainRule{{ID: "a"}, {ID: "b"}}
	apps := []config.AppRule{{ID: "x"}, {ID: "y"}}
	if got := routing.FilterDomainRules(domains, "a"); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("%+v", got)
	}
	if got := routing.FilterAppRules(apps, "y"); len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("%+v", got)
	}
}
