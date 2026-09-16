package ping_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/ping"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestOrderByResults(t *testing.T) {
	nodes := []subscription.Node{
		{ID: "slow", Name: "slow"},
		{ID: "fast", Name: "fast"},
		{ID: "dead", Name: "dead"},
		{ID: "miss", Name: "miss"},
	}
	results := []ping.Result{
		{NodeID: "slow", OK: true, LatencyMs: 200},
		{NodeID: "fast", OK: true, LatencyMs: 40},
		{NodeID: "dead", OK: false, LatencyMs: 4000},
	}
	ordered := ping.OrderByResults(nodes, results)
	want := []string{"fast", "slow", "dead", "miss"}
	for i, id := range want {
		if ordered[i].ID != id {
			t.Fatalf("pos %d: got %s want %s (%+v)", i, ordered[i].ID, id, ordered)
		}
	}
}

func TestOrderByWeightThenPing(t *testing.T) {
	nodes := []subscription.Node{
		{ID: "low", Name: "low"},
		{ID: "high", Name: "high"},
		{ID: "mid", Name: "mid"},
	}
	results := []ping.Result{
		{NodeID: "low", OK: true, LatencyMs: 10},
		{NodeID: "high", OK: true, LatencyMs: 200},
		{NodeID: "mid", OK: true, LatencyMs: 50},
	}
	weights := map[string]int{"high": 2, "mid": 1, "low": -1}
	ordered := ping.OrderByWeightThenPing(nodes, results, weights)
	want := []string{"high", "mid", "low"}
	for i, id := range want {
		if ordered[i].ID != id {
			t.Fatalf("pos %d: got %s want %s", i, ordered[i].ID, id)
		}
	}
}
