package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Valden92/routebox/internal/api"
	"github.com/Valden92/routebox/internal/config"
)

func TestLockedAPIRequiresUnlockAndPreservesSettings(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opErr := store.Update(func(st *config.Settings) { st.MainInterface = "secret-interface" }); opErr != nil {
		t.Fatal(opErr)
	}
	if opErr := store.Lock("password"); opErr != nil {
		t.Fatal(opErr)
	}
	path := store.Path()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err = config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A nil manager proves protected handlers and test-server unlock never start routing.
	srv := api.NewTestServer(store, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	for _, path := range []string{"/api/health", "/api/version"} {
		if code, raw := doJSON(t, ts, http.MethodGet, path, nil); code != 200 {
			t.Fatalf("%s: %d %s", path, code, raw)
		}
	}
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/settings"}, {"GET", "/api/status"}, {"GET", "/api/apps"},
		{"PUT", "/api/settings"}, {"POST", "/api/personal-vpn/connect"}, {"POST", "/api/personal-vpn/disconnect"},
		{"POST", "/api/probe/site"}, {"POST", "/api/subscriptions/"}, {"POST", "/api/lock"},
	} {
		code, raw := doJSON(t, ts, tc.method, tc.path, map[string]string{})
		if code != http.StatusLocked || !bytes.Contains(raw, []byte("config_locked")) {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, code, raw)
		}
	}
	if code, _ := doJSON(t, ts, "POST", "/api/unlock", map[string]string{"passphrase": "wrong"}); code != 401 {
		t.Fatalf("bad-password status %d", code)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !store.IsLocked() {
		t.Fatal("locked requests changed file/state")
	}
	if code, raw := doJSON(t, ts, "POST", "/api/unlock", map[string]string{"passphrase": "password"}); code != 200 {
		t.Fatalf("unlock: %d %s", code, raw)
	}
	if code, raw := doJSON(t, ts, "GET", "/api/settings", nil); code != 200 || !bytes.Contains(raw, []byte("secret-interface")) {
		t.Fatalf("settings: %d %s", code, raw)
	}
	if srv.SingBox != nil {
		t.Fatal("test server started runtime on unlock")
	}
	if code, raw := doJSON(t, ts, "PUT", "/api/settings", map[string]string{"mainInterface": "changed-interface"}); code != 200 {
		t.Fatalf("save: %d %s", code, raw)
	}
	store, err = config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opErr := store.Unlock("password"); opErr != nil {
		t.Fatal(opErr)
	}
	if store.Get().MainInterface != "changed-interface" {
		t.Fatal("API update did not survive restart")
	}
}
