package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestInitializeStoreEncryptedRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opErr := store.Update(func(st *config.Settings) { st.MainInterface = "preserved-interface" }); opErr != nil {
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
	for _, pass := range []string{"", "wrong"} {
		restarted, initErr := initializeStore(dir, "127.0.0.1:12345", pass)
		if pass == "" && (initErr != nil || !restarted.IsLocked()) {
			t.Fatalf("locked startup: %v", initErr)
		}
		if pass != "" && initErr == nil {
			t.Fatal("wrong password accepted")
		}
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("startup overwrote encrypted settings")
		}
	}
	restarted, err := initializeStore(dir, "127.0.0.1:12345", "password")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.IsLocked() || restarted.Get().APIListen != "127.0.0.1:12345" || restarted.Get().MainInterface != "preserved-interface" {
		t.Fatal("unlocked startup lost settings")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(statErr) {
		t.Fatalf("plaintext recreated: %v", statErr)
	}
	store, err = config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opErr := store.Unlock("password"); opErr != nil {
		t.Fatal(opErr)
	}
	if store.Get().APIListen != "127.0.0.1:12345" {
		t.Fatal("startup update was not encrypted")
	}
}

func TestInitializeStorePlainAndMissing(t *testing.T) {
	dir := t.TempDir()
	store, err := initializeStore(dir, "127.0.0.1:12345", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.IsLocked() || store.Get().APIListen != "127.0.0.1:12345" {
		t.Fatal("bad initial settings")
	}
	if opErr := store.Update(func(st *config.Settings) { st.MainInterface = "saved-interface" }); opErr != nil {
		t.Fatal(opErr)
	}
	store, err = initializeStore(dir, "127.0.0.1:23456", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.Get().MainInterface != "saved-interface" || store.Get().APIListen != "127.0.0.1:23456" {
		t.Fatal("lost plaintext settings")
	}
}
