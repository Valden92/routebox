package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestRepeatedRuleAddReportsUnchangedWithoutDuplicating(t *testing.T) {
	ts, store, _ := newAPITest(t)
	var id string
	for i := range 6 {
		code, raw := doJSON(t, ts, http.MethodPost, "/api/rules", map[string]any{
			"pattern": "routebox-check.invalid", "path": "direct",
		})
		var result struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if code != http.StatusOK || json.Unmarshal(raw, &result) != nil {
			t.Fatalf("add: %d %s", code, raw)
		}
		want := "unchanged"
		if i == 0 {
			want, id = "created", result.ID
		}
		if result.Status != want || result.ID != id || id == "" || len(store.Get().DomainRules) != 1 {
			t.Fatalf("repeat %d: status=%s want=%s rules=%d", i, result.Status, want, len(store.Get().DomainRules))
		}
	}
	code, raw := doJSON(t, ts, http.MethodPost, "/api/rules", map[string]any{
		"pattern": "https://ROUTEBOX-CHECK.invalid/path", "path": "personal",
	})
	var result map[string]any
	if code != http.StatusOK || json.Unmarshal(raw, &result) != nil || result["status"] != "updated" || result["id"] != id {
		t.Fatalf("updated rule result: %d %s", code, raw)
	}
	st := store.Get()
	if len(st.DomainRules) != 1 || st.DomainRules[0].Path != config.RoutePersonal {
		t.Fatal("changed path did not update the existing rule")
	}
}
