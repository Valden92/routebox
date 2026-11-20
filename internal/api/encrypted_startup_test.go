package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestEncryptedStartupWaitsForSuccessfulUnlock(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opErr := store.Lock("password"); opErr != nil {
		t.Fatal(opErr)
	}
	store, err = config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Production construction must not access a manager or launch workers while locked.
	srv := NewServer(store, nil)
	if srv.runtimeStarted || srv.SingBox != nil {
		t.Fatal("runtime started while locked")
	}
	starts := 0
	srv.startWorkers = func() { starts++ }
	srv.startRuntimeLocked()
	if starts != 0 {
		t.Fatal("locked runtime started")
	}
	request := func(pass string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/unlock", bytes.NewBufferString(`{"passphrase":"`+pass+`"}`)))
		return rec.Code
	}
	if code := request("wrong"); code != http.StatusUnauthorized || starts != 0 {
		t.Fatalf("wrong-password startup: %d / %d", code, starts)
	}
	if code := request("password"); code != http.StatusOK || starts != 1 {
		t.Fatalf("unlocked startup: %d / %d", code, starts)
	}
	srv.startRuntimeLocked()
	if starts != 1 {
		t.Fatal("runtime started twice")
	}
	if code := request("password"); code != http.StatusUnauthorized || starts != 1 {
		t.Fatalf("duplicate unlock: %d / %d", code, starts)
	}
}

func TestLockedShutdownDoesNotTouchNetwork(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opErr := store.Lock("password"); opErr != nil {
		t.Fatal(opErr)
	}
	store, err = config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(store, nil)
	srv.StopRouter() // nil manager proves no stop/recover occurs
	starts := 0
	srv.startWorkers = func() { starts++ }
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/unlock", bytes.NewBufferString(`{"passphrase":"password"}`)))
	if rec.Code != http.StatusServiceUnavailable || !store.IsLocked() || starts != 0 {
		t.Fatalf("shutdown allowed unlock/start: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/settings", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("shutdown allowed settings request: %d", rec.Code)
	}
}
