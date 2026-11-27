package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestDomainBlockCRUDAndWildcardScope(t *testing.T) {
	ts, store, _ := newAPITest(t)
	add := func(pattern, path string) string {
		t.Helper()
		code, body := doJSON(t, ts, http.MethodPost, "/api/rules", map[string]string{"pattern": pattern, "path": path})
		if code != 200 {
			t.Fatalf("add: %d %s", code, body)
		}
		var r struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	deny := add("*.example.test", "block")
	add("ads.example.test", "direct")
	add("metrics.example.test", "block")
	add("metrics.example.test", "block") // exact duplicate must not append another deny
	rules := store.Get().DomainRules
	if len(rules) != 3 || rules[0].ID != deny || rules[0].Pattern != "*.example.test" || rules[0].Path != config.RouteBlock {
		t.Fatalf("suffix deny was overwritten: %+v", rules)
	}
	for _, req := range []map[string]any{{"enabled": false}, {"enabled": true}, {"path": "personal"}, {"path": "block"}} {
		code, body := doJSON(t, ts, http.MethodPut, "/api/rules/"+deny, req)
		if code != 200 {
			t.Fatalf("update: %d %s", code, body)
		}
	}
	if code, _ := doJSON(t, ts, http.MethodDelete, "/api/rules/"+deny, nil); code != 200 {
		t.Fatal(code)
	}
	for _, r := range store.Get().DomainRules {
		if r.ID == deny {
			t.Fatal("deny not deleted")
		}
	}
}
func TestBlockValidationDoesNotChangeRules(t *testing.T) {
	ts, store, _ := newAPITest(t)
	if err := store.Update(func(st *config.Settings) {
		st.DomainRules = []config.DomainRule{
			{ID: "ip", Pattern: "127.0.0.1", Path: config.RouteDirect, Enabled: true},
			{ID: "deny", Pattern: "ads.test", Path: config.RouteBlock, Source: "manual", Enabled: true},
		}
		st.AppRules = []config.AppRule{{ID: "app", ProcessName: "fixture", Path: config.RouteDirect, Enabled: true}}
	}); err != nil {
		t.Fatal(err)
	}
	before := store.Get()
	for _, tc := range []struct {
		method, path string
		body         map[string]any
	}{
		{"POST", "/api/rules", map[string]any{"pattern": "*", "path": "block"}},
		{"POST", "/api/rules", map[string]any{"pattern": "127.0.0.1", "path": "block"}},
		{"POST", "/api/rules", map[string]any{"processName": "fixture", "path": "block"}},
		{"PUT", "/api/rules/app", map[string]any{"path": "block"}},
		{"PUT", "/api/rules/ip", map[string]any{"path": "block"}},
		{"PUT", "/api/rules/deny", map[string]any{"pattern": "bad..example.com"}},
	} {
		code, body := doJSON(t, ts, tc.method, tc.path, tc.body)
		if code != 400 {
			t.Fatalf("%s: %d %s", tc.path, code, body)
		}
		if !reflect.DeepEqual(before, store.Get()) {
			t.Fatal("invalid block changed settings")
		}
	}
}
