package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestRuleListAddsDomainGroupsWithoutChangingSettings(t *testing.T) {
	ts, store, _ := newAPITest(t)
	if err := store.Update(func(st *config.Settings) {
		st.DomainRules = []config.DomainRule{
			{ID: "1", Pattern: "cdn.music.yandex.ru", Path: config.RouteDirect, Source: "auto", Enabled: true},
			{ID: "2", Pattern: "*.example.co.uk", Path: config.RoutePersonal, Source: "manual", Enabled: false},
			{ID: "3", Pattern: "api.example.co.uk", Path: config.RouteWork, Source: "auto", Enabled: true},
		}
	}); err != nil {
		t.Fatal(err)
	}
	before := store.Get()
	path := filepath.Join(before.DataDir, "settings.json")
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		code, body := doJSON(t, ts, http.MethodGet, "/api/rules", nil)
		if code != 200 {
			t.Fatalf("status=%d body=%s", code, body)
		}
		var response struct {
			Domains []struct {
				config.DomainRule
				DomainGroup string `json:"domainGroup"`
			}
		}
		if err = json.Unmarshal(body, &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Domains) != 3 {
			t.Fatal("rules lost")
		}
		for i, want := range []string{"yandex.ru", "example.co.uk", "example.co.uk"} {
			if response.Domains[i].DomainGroup != want || !reflect.DeepEqual(response.Domains[i].DomainRule, before.DomainRules[i]) {
				t.Fatalf("rule %d changed or grouped incorrectly: %+v", i, response.Domains[i])
			}
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != string(after) || !reflect.DeepEqual(before, store.Get()) {
		t.Fatal("read-only grouping changed settings")
	}
}
