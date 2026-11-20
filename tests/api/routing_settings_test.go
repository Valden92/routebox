package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestLearningTogglePreservesOtherSettings(t *testing.T) {
	ts, store, _ := newAPITest(t)
	if err := store.Update(func(st *config.Settings) {
		st.PersonalVPN.Enabled = true
		st.PersonalVPN.ActiveSubscriptionID = "keep-subscription"
		st.DomainRules = []config.DomainRule{{ID: "domain", Pattern: "example.test", Path: config.RoutePersonal, Enabled: true}}
		st.AppRules = []config.AppRule{{ID: "app", ProcessName: "fixture", Path: config.RouteWork, Enabled: true}}
		st.Subscriptions = []config.Subscription{{ID: "keep-subscription", Name: "fixture", SelectedNodeID: "keep-node"}}
	}); err != nil {
		t.Fatal(err)
	}
	expected := store.Get()
	for _, enabled := range []bool{false, true, false} {
		code, raw := doJSON(t, ts, http.MethodPut, "/api/settings", map[string]any{"autoDetectRouting": enabled})
		if code != 200 {
			t.Fatalf("toggle: %d %s", code, raw)
		}
		expected.AutoDetectRouting = enabled
		if !reflect.DeepEqual(expected, store.Get()) {
			t.Fatal("toggling background learning changed unrelated settings")
		}
	}
	// An unrelated partial update must not silently re-enable learning.
	code, raw := doJSON(t, ts, http.MethodPut, "/api/settings", map[string]any{"mainInterface": "fixture-main"})
	if code != 200 {
		t.Fatalf("partial update: %d %s", code, raw)
	}
	expected.MainInterface = "fixture-main"
	if !reflect.DeepEqual(expected, store.Get()) {
		t.Fatal("omitted settings were replaced")
	}
	code, raw = doJSON(t, ts, http.MethodGet, "/api/settings", nil)
	var output map[string]any
	if code != 200 || json.Unmarshal(raw, &output) != nil {
		t.Fatalf("settings response: %d %s", code, raw)
	}
	if _, present := output["defaultPath"]; present {
		t.Fatal("API still advertises an unsupported default route")
	}
	reloaded, err := config.NewStore(expected.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, reloaded.Get()) {
		t.Fatal("partial update did not survive restart")
	}
	// Explicit empty arrays still clear lists, while omitted fields preserve them.
	code, raw = doJSON(t, ts, http.MethodPut, "/api/settings", map[string]any{
		"domainRules": []any{}, "appRules": []any{}, "subscriptions": []any{},
	})
	if code != 200 {
		t.Fatalf("clear lists: %d %s", code, raw)
	}
	st := store.Get()
	if len(st.DomainRules)+len(st.AppRules)+len(st.Subscriptions) != 0 || !st.PersonalVPN.Enabled || st.AutoDetectRouting {
		t.Fatal("explicit list replacement changed unrelated settings")
	}
}

func TestSettingsRejectInvalidLearningFlag(t *testing.T) {
	ts, store, _ := newAPITest(t)
	before := store.Get()
	code, _ := doJSON(t, ts, http.MethodPut, "/api/settings", map[string]any{"autoDetectRouting": "false"})
	if code != 400 || !reflect.DeepEqual(before, store.Get()) {
		t.Fatal("invalid flag changed settings")
	}
}
