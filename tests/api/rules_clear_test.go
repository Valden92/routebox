package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/api"
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
)

func clearFixture(t *testing.T) (*api.Server, *config.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Update(func(st *config.Settings) {
		st.DomainRules = []config.DomainRule{
			{ID: "auto1", Pattern: "a.invalid", Source: "auto", Path: config.RoutePersonal, Enabled: true},
			{ID: "auto2", Pattern: "b.invalid", Source: "auto", Path: config.RouteWork, Enabled: false},
			{ID: "manual", Pattern: "m.invalid", Source: "manual", Path: config.RouteDirect, Enabled: true},
			{ID: "legacy", Pattern: "l.invalid", Path: config.RouteDirect, Enabled: true},
			{ID: "import", Pattern: "i.invalid", Source: "import", Path: config.RouteWork, Enabled: true},
		}
		st.AppRules = []config.AppRule{{ID: "app", ProcessName: "fixture", Path: config.RoutePersonal, Enabled: true}}
	}); err != nil {
		t.Fatal(err)
	}
	return api.NewTestServer(store, singbox.NewManager("/nonexistent-test-core", filepath.Join(dir, "core.json"))), store
}

func clearRequest(t *testing.T, srv *api.Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/rules/clear", strings.NewReader(body)))
	return w
}

func TestClearRulesOnlyConfirmedSourceAndIDs(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []string
	}{
		{"automatic across routes and states", `{"source":"auto","ids":["auto1","auto2","auto1"]}`, []string{"manual", "legacy", "import"}},
		{"manual includes legacy and imported", `{"source":"manual","ids":["manual","legacy","import"]}`, []string{"auto1", "auto2"}},
		{"new rules and app IDs survive", `{"source":"auto","ids":["auto1","app","gone"]}`, []string{"auto2", "manual", "legacy", "import"}},
		{"already deleted is a no-op", `{"source":"auto","ids":["gone"]}`, []string{"auto1", "auto2", "manual", "legacy", "import"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, store := clearFixture(t)
			w := clearRequest(t, srv, tc.body)
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			st := store.Get()
			var ids []string
			for _, r := range st.DomainRules {
				ids = append(ids, r.ID)
			}
			if !reflect.DeepEqual(ids, tc.want) || len(st.AppRules) != 1 || st.AppRules[0].ID != "app" {
				t.Fatalf("rules=%v apps=%v", ids, st.AppRules)
			}
			reloaded, err := config.NewStore(st.DataDir)
			if err != nil {
				t.Fatal(err)
			}
			if err = reloaded.TryLoad(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reloaded.Get().DomainRules, st.DomainRules) {
				t.Fatal("saved rules differ")
			}
		})
	}
}

func TestClearRulesRejectsInvalidAndChangedSourceAtomically(t *testing.T) {
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{`, 400}, {`{}`, 400}, {`{"source":"all","ids":["auto1"]}`, 400},
		{`{"source":"auto","ids":[]}`, 400}, {`{"source":"auto","ids":[""]}`, 400},
		{`{"source":"auto","ids":["auto1"],"unexpected":true}`, 400},
		{`{"source":"auto","ids":["auto1"]} {}`, 400},
		{`{"source":"auto","ids":["auto1","manual"]}`, 409},
	} {
		t.Run(tc.body, func(t *testing.T) {
			srv, store := clearFixture(t)
			before := store.Get()
			w := clearRequest(t, srv, tc.body)
			if w.Code != tc.code || !reflect.DeepEqual(before.DomainRules, store.Get().DomainRules) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestClearRulesSaveFailureDoesNotRemoveCachedRules(t *testing.T) {
	srv, store := clearFixture(t)
	before := store.Get()
	path := filepath.Join(before.DataDir, "settings.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	w := clearRequest(t, srv, `{"source":"auto","ids":["auto1"]}`)
	if w.Code != 500 || !reflect.DeepEqual(before.DomainRules, store.Get().DomainRules) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestClearRulesRespectsLocalAPIAuthentication(t *testing.T) {
	srv, store := clearFixture(t)
	before := store.Get()
	guard, token := localGuard(t)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47891/api/rules/clear", strings.NewReader(`{"source":"auto","ids":["auto1"]}`))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	guard.Handler(srv.Handler()).ServeHTTP(w, req)
	if w.Code != 401 || !reflect.DeepEqual(before.DomainRules, store.Get().DomainRules) {
		t.Fatal("unauthorized cleanup changed rules")
	}
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47891/api/rules/clear", strings.NewReader(`{"source":"auto","ids":["auto1"]}`))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	guard.Handler(srv.Handler()).ServeHTTP(w, req)
	if w.Code != 200 || len(store.Get().DomainRules) != 4 {
		t.Fatalf("authorized status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestClearRulesCannotWriteLockedSettings(t *testing.T) {
	_, store := clearFixture(t)
	if err := store.Lock("fixture-passphrase"); err != nil {
		t.Fatal(err)
	}
	dir := store.Get().DataDir
	locked, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = locked.TryLoad(); err != config.ErrLocked {
		t.Fatalf("expected locked, got %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "settings.json.enc"))
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewTestServer(locked, singbox.NewManager("/nonexistent-test-core", filepath.Join(dir, "core.json")))
	w := clearRequest(t, srv, `{"source":"auto","ids":["auto1"]}`)
	after, err := os.ReadFile(filepath.Join(dir, "settings.json.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != 423 || string(before) != string(after) {
		t.Fatal("locked settings were changed")
	}
}
