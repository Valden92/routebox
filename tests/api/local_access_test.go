package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/api"
	"github.com/Valden92/routebox/internal/config"
)

func localGuard(t *testing.T) (*api.LocalAccess, string) {
	t.Helper()
	dir := t.TempDir()
	guard, err := api.NewLocalAccess(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".api-token"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, ".api-token"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 64 || info.Mode().Perm() != 0o600 {
		t.Fatal("credential format/permissions invalid")
	}
	return guard, string(raw)
}

func guardedRequest(t *testing.T, guard *api.LocalAccess, method, path, token, origin string, change func(*http.Request)) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	called := false
	handler := guard.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(method, "http://127.0.0.1:47891"+path, nil)
	req.RemoteAddr = "127.0.0.1:54321"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if change != nil {
		change(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, called
}

func TestLocalAPIRequiresCredentialBeforeHandlers(t *testing.T) {
	guard, token := localGuard(t)
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			rec, called := guardedRequest(t, guard, method, "/api/settings", "", "", nil)
			if rec.Code != 401 || called || !strings.Contains(rec.Body.String(), "api_auth_required") {
				t.Fatalf("guard failed: %d", rec.Code)
			}
		})
	}
	for _, origin := range []string{"", "http://localhost:1420", "http://127.0.0.1:1420", "tauri://localhost", "http://tauri.localhost", "https://tauri.localhost"} {
		rec, called := guardedRequest(t, guard, "GET", "/api/settings", token, origin, nil)
		if rec.Code != 200 || !called {
			t.Fatalf("trusted client failed: %q %d", origin, rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != origin || rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("overbroad CORS")
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("credential response may be cached")
		}
	}
}

func TestLocalAPIRejectsUntrustedSourcesEvenWithToken(t *testing.T) {
	guard, token := localGuard(t)
	for _, origin := range []string{"https://evil.invalid", "null", "http://localhost:14200", "http://localhost:1420.evil.invalid", "http://localhost:1420/", "http://localhost:5173"} {
		rec, called := guardedRequest(t, guard, "GET", "/api/health", token, origin, nil)
		if rec.Code != 403 || called || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("untrusted origin accepted: %q", origin)
		}
	}
	cases := []struct {
		name   string
		change func(*http.Request)
	}{
		{"rebind host", func(r *http.Request) { r.Host = "evil.invalid:47891" }},
		{"loopback suffix", func(r *http.Request) { r.Host = "127.0.0.1.evil.invalid:47891" }},
		{"missing port", func(r *http.Request) { r.Host = "127.0.0.1" }},
		{"remote peer spoofing proxy", func(r *http.Request) { r.RemoteAddr = "192.0.2.1:50000"; r.Header.Set("X-Forwarded-For", "127.0.0.1") }},
		{"duplicate origin", func(r *http.Request) { r.Header.Add("Origin", "http://localhost:1420") }},
		{"empty origin", func(r *http.Request) { r.Header.Set("Origin", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origin := ""
			if tc.name == "duplicate origin" {
				origin = "http://localhost:1420"
			}
			rec, called := guardedRequest(t, guard, "GET", "/api/settings", token, origin, tc.change)
			if rec.Code != 403 || called {
				t.Fatalf("source accepted: %d", rec.Code)
			}
		})
	}
}

func TestLocalAPITokenTransportAndRotation(t *testing.T) {
	guard, token := localGuard(t)
	other, _ := localGuard(t)
	cases := []struct {
		name, path, token string
		change            func(*http.Request)
	}{
		{"wrong token", "/api/settings", strings.Repeat("f", 64), nil},
		{"query token", "/api/settings?token=" + token, "", nil},
		{"duplicate authorization", "/api/settings", token, func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+token) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, called := guardedRequest(t, guard, "GET", tc.path, tc.token, "", tc.change)
			if rec.Code != 401 || called {
				t.Fatalf("bad token accepted: %d", rec.Code)
			}
		})
	}
	rec, called := guardedRequest(t, other, "GET", "/api/settings", token, "", nil)
	if rec.Code != 401 || called {
		t.Fatal("another daemon accepted old token")
	}
	for _, path := range []string{"/api/health", "/api/version"} {
		rec, called = guardedRequest(t, guard, "GET", path, "", "", nil)
		if rec.Code != 200 || !called {
			t.Fatal("public probe requires credential")
		}
	}
	rec, called = guardedRequest(t, guard, "POST", "/api/health", "", "", nil)
	if rec.Code != 401 || called {
		t.Fatal("public path exempted a mutation")
	}
}

func TestLocalAPIPreflightDoesNotRunHandlers(t *testing.T) {
	guard, _ := localGuard(t)
	rec, called := guardedRequest(t, guard, "OPTIONS", "/api/settings", "", "http://localhost:1420", func(r *http.Request) {
		r.Header.Set("Access-Control-Request-Method", "PUT")
		r.Header.Set("Access-Control-Request-Headers", "authorization, Content-Type")
	})
	if rec.Code != 204 || called {
		t.Fatalf("preflight failed: %d", rec.Code)
	}
	for _, tc := range []struct{ origin, method, headers string }{{"", "GET", "authorization"}, {"http://localhost:1420", "PATCH", ""}, {"http://localhost:1420", "PUT", "x-unknown"}} {
		rec, called = guardedRequest(t, guard, "OPTIONS", "/api/settings", "", tc.origin, func(r *http.Request) {
			r.Header.Set("Access-Control-Request-Method", tc.method)
			r.Header.Set("Access-Control-Request-Headers", tc.headers)
		})
		if rec.Code != 403 || called {
			t.Fatal("bad preflight accepted")
		}
	}
}

func TestLocalAPIListenAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1:47891", "localhost:47891", "[::1]:47891"} {
		if err := api.ValidateListenAddress(address); err != nil {
			t.Fatal(err)
		}
	}
	for _, address := range []string{":47891", "0.0.0.0:47891", "[::]:47891", "192.0.2.1:47891", "evil.invalid:47891", "127.0.0.1:0", "127.0.0.1:99999", "127.0.0.1"} {
		if err := api.ValidateListenAddress(address); err == nil {
			t.Fatalf("unsafe bind accepted: %s", address)
		}
	}
}

func TestLocalAPIComposesWithLockedSettings(t *testing.T) {
	guard, token := localGuard(t)
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Lock("password"); err != nil {
		t.Fatal(err)
	}
	store, err = config.NewStore(store.Get().DataDir)
	if err != nil {
		t.Fatal(err)
	}
	handler := guard.Handler(api.NewTestServer(store, nil).Handler())
	req := httptest.NewRequest("GET", "http://127.0.0.1:47891/api/settings", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var result struct {
		Code string `json:"code"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 423 || result.Code != "config_locked" {
		t.Fatalf("encryption bypassed: %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "http://127.0.0.1:47891/api/unlock", strings.NewReader(`{"passphrase":"password"}`))
	req.RemoteAddr = "127.0.0.1:50000"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 401 || !store.IsLocked() {
		t.Fatal("unauthenticated unlock reached the store")
	}
}
