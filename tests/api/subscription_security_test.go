package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/api"
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestSubscriptionEndpointsRejectPrivateFetchWithoutMutation(t *testing.T) {
	var requests atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "must never be fetched")
	}))
	defer private.Close()
	for _, operation := range []string{"add", "refresh", "nodes_without_cache"} {
		t.Run(operation, func(t *testing.T) {
			store, err := config.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			lastRefresh := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			if err = store.Update(func(st *config.Settings) {
				st.Subscriptions = []config.Subscription{{ID: "existing", URL: private.URL + "/secret?token=private-secret", LastRefresh: lastRefresh, SelectedNodeID: "selected"}}
			}); err != nil {
				t.Fatal(err)
			}
			st := store.Get()
			cachePath := filepath.Join(st.DataDir, "subscriptions", "existing.json")
			var cacheBefore []byte
			if operation == "refresh" {
				if err = subscription.SaveCache(st.DataDir, "existing", []subscription.Node{{ID: "selected", Host: "old.example", Port: 443}}); err != nil {
					t.Fatal(err)
				}
				cacheBefore, err = os.ReadFile(cachePath)
				if err != nil {
					t.Fatal(err)
				}
			}
			settingsBefore, err := os.ReadFile(filepath.Join(st.DataDir, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			// No injected fetcher: exercise the production SSRF policy, without workers.
			srv := api.NewTestServer(store, singbox.NewManager("/nonexistent/sing-box", filepath.Join(st.DataDir, "sing-box.json")))
			method, path := http.MethodPost, "/api/subscriptions/existing/refresh"
			var body []byte
			if operation == "add" {
				path = "/api/subscriptions/"
				body, err = json.Marshal(map[string]any{"name": "blocked", "url": private.URL + "/secret?token=private-secret", "source": "url"})
				if err != nil {
					t.Fatal(err)
				}
			} else if operation == "nodes_without_cache" {
				method, path = http.MethodGet, "/api/subscriptions/existing/nodes"
			}
			req := httptest.NewRequest(method, path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code < 400 || !strings.Contains(w.Body.String(), "публичных HTTP(S)") || strings.Contains(w.Body.String(), "private-secret") {
				t.Fatalf("not safely blocked: %d %s", w.Code, w.Body.String())
			}
			settingsAfter, err := os.ReadFile(filepath.Join(st.DataDir, "settings.json"))
			if err != nil || !bytes.Equal(settingsBefore, settingsAfter) {
				t.Fatalf("failed fetch changed settings: %v", err)
			}
			cacheAfter, err := os.ReadFile(cachePath)
			if cacheBefore != nil {
				if err != nil || !bytes.Equal(cacheBefore, cacheAfter) {
					t.Fatalf("failed fetch changed cache: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("failed fetch created cache: %v", err)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("private target received %d requests", requests.Load())
	}
}
