package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Valden92/routebox/internal/api"
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func newAPITest(t *testing.T) (*httptest.Server, *config.Store, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(func(st *config.Settings) {
		st.SystemVPN.NMConnectionID = "PTsecurity-nonexistent-api-test"
		st.MainInterface = "lo"
	})

	subBody := "vless://bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb@api-test.example.com:443?encryption=none&security=tls&sni=api-test.example.com#APINode\n"
	subSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, subBody)
	}))
	t.Cleanup(subSrv.Close)

	sb := singbox.NewManager("/nonexistent/sing-box-for-tests", filepath.Join(dir, "sing-box.json"))
	srv := api.NewTestServer(store, sb)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, store, subSrv
}

func doJSON(t *testing.T, ts *httptest.Server, method, path string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func TestHealthAndVersion(t *testing.T) {
	ts, _, _ := newAPITest(t)
	code, raw := doJSON(t, ts, http.MethodGet, "/api/health", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var health map[string]string
	_ = json.Unmarshal(raw, &health)
	if health["status"] != "ok" {
		t.Fatalf("%v", health)
	}

	code, raw = doJSON(t, ts, http.MethodGet, "/api/version", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var ver map[string]any
	_ = json.Unmarshal(raw, &ver)
	if ver["apiVersion"] == nil {
		t.Fatalf("%v", ver)
	}
}

func TestSubscriptionCRUDAndSelect(t *testing.T) {
	ts, store, subSrv := newAPITest(t)

	code, raw := doJSON(t, ts, http.MethodPost, "/api/subscriptions/", map[string]any{
		"name":                   "test-sub",
		"url":                    subSrv.URL,
		"autoRefresh":            false,
		"refreshIntervalMinutes": 0,
	})
	if code != 200 {
		t.Fatalf("add: %d %s", code, raw)
	}
	var sub config.Subscription
	if err := json.Unmarshal(raw, &sub); err != nil || sub.ID == "" {
		t.Fatalf("%s", raw)
	}

	code, raw = doJSON(t, ts, http.MethodGet, "/api/subscriptions/", nil)
	if code != 200 {
		t.Fatalf("list: %d %s", code, raw)
	}
	var list []config.Subscription
	_ = json.Unmarshal(raw, &list)
	if len(list) != 1 || list[0].ID != sub.ID {
		t.Fatalf("%+v", list)
	}

	code, raw = doJSON(t, ts, http.MethodGet, "/api/subscriptions/"+sub.ID+"/nodes", nil)
	if code != 200 {
		t.Fatalf("nodes: %d %s", code, raw)
	}
	var nodes []subscription.Node
	_ = json.Unmarshal(raw, &nodes)
	if len(nodes) != 1 || nodes[0].Host != "api-test.example.com" {
		t.Fatalf("%+v", nodes)
	}

	code, raw = doJSON(t, ts, http.MethodPost, "/api/subscriptions/"+sub.ID+"/select", map[string]any{
		"nodeId": nodes[0].ID,
	})
	if code != 200 {
		t.Fatalf("select: %d %s", code, raw)
	}
	got := store.Get()
	if got.PersonalVPN.ActiveSubscriptionID != sub.ID {
		t.Fatalf("active sub %q", got.PersonalVPN.ActiveSubscriptionID)
	}
	var selected config.Subscription
	for _, s := range got.Subscriptions {
		if s.ID == sub.ID {
			selected = s
		}
	}
	if selected.SelectedNodeID != nodes[0].ID || selected.NodeSelectCounts[nodes[0].ID] != 1 {
		t.Fatalf("%+v", selected)
	}

	code, raw = doJSON(t, ts, http.MethodPut, "/api/subscriptions/"+sub.ID, map[string]any{
		"autoRefresh":            true,
		"refreshIntervalMinutes": 30,
	})
	if code != 200 {
		t.Fatalf("update: %d %s", code, raw)
	}
	_ = json.Unmarshal(raw, &sub)
	if !sub.AutoRefresh || sub.RefreshIntervalMinutes != 30 {
		t.Fatalf("%+v", sub)
	}

	code, raw = doJSON(t, ts, http.MethodDelete, "/api/subscriptions/"+sub.ID, nil)
	if code != 200 {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if len(store.Get().Subscriptions) != 0 {
		t.Fatalf("%+v", store.Get().Subscriptions)
	}
	if store.Get().PersonalVPN.ActiveSubscriptionID != "" {
		t.Fatal("active sub should clear")
	}
}

func TestAddSubscriptionBadBody(t *testing.T) {
	ts, _, _ := newAPITest(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<!DOCTYPE html><html>")
	}))
	t.Cleanup(bad.Close)
	code, _ := doJSON(t, ts, http.MethodPost, "/api/subscriptions/", map[string]any{
		"name": "bad", "url": bad.URL, "autoRefresh": false,
	})
	if code != 400 && code != 502 {
		t.Fatalf("want 400/502, got %d", code)
	}
}

func TestRulesCRUD(t *testing.T) {
	ts, store, _ := newAPITest(t)

	code, raw := doJSON(t, ts, http.MethodPost, "/api/rules/", map[string]any{
		"pattern": "https://Music.Yandex.RU/path",
		"path":    "direct",
	})
	if code != 200 {
		t.Fatalf("add: %d %s", code, raw)
	}
	var added map[string]string
	_ = json.Unmarshal(raw, &added)
	id := added["id"]
	if id == "" {
		t.Fatal("no id")
	}
	rules := store.Get().DomainRules
	if len(rules) != 1 || rules[0].Pattern != "music.yandex.ru" || rules[0].Path != config.RouteDirect {
		t.Fatalf("%+v", rules)
	}

	code, raw = doJSON(t, ts, http.MethodGet, "/api/rules/", nil)
	if code != 200 {
		t.Fatalf("list: %d %s", code, raw)
	}

	enabled := false
	code, raw = doJSON(t, ts, http.MethodPut, "/api/rules/"+id, map[string]any{
		"path":    "personal",
		"enabled": enabled,
	})
	if code != 200 {
		t.Fatalf("update: %d %s", code, raw)
	}
	r0 := store.Get().DomainRules[0]
	if r0.Path != config.RoutePersonal || r0.Enabled {
		t.Fatalf("%+v", r0)
	}

	code, raw = doJSON(t, ts, http.MethodDelete, "/api/rules/"+id, nil)
	if code != 200 {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if len(store.Get().DomainRules) != 0 {
		t.Fatalf("%+v", store.Get().DomainRules)
	}
}

func TestSuggestRule(t *testing.T) {
	ts, _, _ := newAPITest(t)
	code, raw := doJSON(t, ts, http.MethodPost, "/api/rules/suggest", map[string]any{
		"host": "portal.ptsecurity.ru",
	})
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out["suggestedPath"] != string(config.RouteWork) {
		t.Fatalf("%v", out)
	}
}

func TestSettingsGetPut(t *testing.T) {
	ts, store, _ := newAPITest(t)
	code, raw := doJSON(t, ts, http.MethodGet, "/api/settings", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	code, raw = doJSON(t, ts, http.MethodPut, "/api/settings", map[string]any{
		"mainInterface": "eth1",
	})
	if code != 200 {
		t.Fatalf("put: %d %s", code, raw)
	}
	if store.Get().MainInterface != "eth1" {
		t.Fatalf("%+v", store.Get())
	}
}
