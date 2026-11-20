package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestScheduledRefreshCannotFetchPrivateSubscription(t *testing.T) {
	var requests atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer private.Close()
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lastRefresh := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err = store.Update(func(st *config.Settings) {
		st.Subscriptions = []config.Subscription{{ID: "private", URL: private.URL, AutoRefresh: true, RefreshIntervalMinutes: 1, LastRefresh: lastRefresh, SelectedNodeID: "selected"}}
	}); err != nil {
		t.Fatal(err)
	}
	st := store.Get()
	if err = subscription.SaveCache(st.DataDir, "private", []subscription.Node{{ID: "selected", Host: "old.example", Port: 443}}); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(st.DataDir, "subscriptions", "private.json")
	before, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	newRefreshScheduler(store).tick()
	after, err := os.ReadFile(cachePath)
	if err != nil || !bytes.Equal(before, after) || requests.Load() != 0 {
		t.Fatalf("unsafe refresh: requests=%d cacheErr=%v", requests.Load(), err)
	}
	got := store.Get().Subscriptions[0]
	if !got.LastRefresh.Equal(lastRefresh) || got.SelectedNodeID != "selected" {
		t.Fatalf("blocked refresh changed subscription state: %+v", got)
	}
}
