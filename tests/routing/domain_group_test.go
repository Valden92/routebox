package routing_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/routing"
)

func TestDomainRuleGroup(t *testing.T) {
	for _, tc := range []struct{ pattern, want string }{
		{"cdn.music.yandex.ru", "yandex.ru"}, {"*.yandex.ru", "yandex.ru"},
		{"https://Docs.Example.COM/path?x=1", "example.com"}, {"api.example.com.", "example.com"},
		{"a.shop.example.co.uk", "example.co.uk"}, {"a.other.co.uk", "other.co.uk"},
		{"cdn.example.com.au", "example.com.au"}, {"one.github.io", "one.github.io"},
		{"api.two.github.io", "two.github.io"}, {"почта.пример.рф", "пример.рф"},
		{"api.xn--e1afmkfd.xn--p1ai", "пример.рф"}, {"localhost", "localhost"},
		{"co.uk", "co.uk"}, {"127.0.0.1", "127.0.0.1"}, {"[2001:db8::1]", "2001:db8::1"},
		{"", ""}, {"bad..example.com", "bad..example.com"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			if got := routing.DomainRuleGroup(tc.pattern); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
