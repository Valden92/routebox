package api

import (
	"testing"
)

func TestNormalizeAutoSelectSites(t *testing.T) {
	got := NormalizeAutoSelectSites([]string{"  ", "example.com", "https://a.test", "example.com", "http://b.test"})
	if len(got) != 3 {
		t.Fatalf("%v", got)
	}
	if got[0] != "https://example.com" || got[1] != "https://a.test" || got[2] != "http://b.test" {
		t.Fatalf("%v", got)
	}
	def := NormalizeAutoSelectSites(nil)
	if len(def) != 1 || def[0] != autoSelectDefaultSites {
		t.Fatalf("%v", def)
	}
}

func TestResolveAutoSelectNodeLimit(t *testing.T) {
	if got := resolveAutoSelectNodeLimit(0, 42); got != 42 {
		t.Fatalf("all nodes: %d", got)
	}
	if got := resolveAutoSelectNodeLimit(-1, 10); got != 10 {
		t.Fatalf("negative = all: %d", got)
	}
	if got := resolveAutoSelectNodeLimit(5, 40); got != 5 {
		t.Fatalf("explicit cap: %d", got)
	}
	if got := resolveAutoSelectNodeLimit(0, 10_000); got != autoSelectHardCapNodes {
		t.Fatalf("hard cap: %d", got)
	}
}

func TestAdjustAutoSelectWeight(t *testing.T) {
	var m map[string]int
	m = AdjustAutoSelectWeight(m, "a", true)
	m = AdjustAutoSelectWeight(m, "a", true)
	m = AdjustAutoSelectWeight(m, "b", false)
	if m["a"] != 2 || m["b"] != -1 {
		t.Fatalf("%v", m)
	}
}
