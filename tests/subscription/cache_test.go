package subscription_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Valden92/routebox/internal/subscription"
)

func TestSaveLoadCache(t *testing.T) {
	dir := t.TempDir()
	nodes := []subscription.Node{
		{ID: "1", Host: "a.example", Port: 443, Protocol: "vless", Name: "A"},
	}
	if err := subscription.SaveCache(dir, "sub-1", nodes); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "subscriptions", "sub-1.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	c, err := subscription.LoadCache(dir, "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if c.SubscriptionID != "sub-1" || len(c.Nodes) != 1 || c.Nodes[0].Host != "a.example" {
		t.Fatalf("%+v", c)
	}
}

func TestLoadCacheMissing(t *testing.T) {
	_, err := subscription.LoadCache(t.TempDir(), "nope")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadCacheCorrupt(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subscriptions")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := subscription.LoadCache(dir, "bad")
	if err == nil {
		t.Fatal("expected error")
	}
}
