package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/config"
)

func TestDefaultSettings(t *testing.T) {
	dir := t.TempDir()
	st := config.DefaultSettings(dir)
	if st.DataDir != dir {
		t.Fatalf("DataDir %q", st.DataDir)
	}
	if st.DefaultPath != config.RoutePersonal {
		t.Fatalf("DefaultPath %s", st.DefaultPath)
	}
	if st.SingBoxConfigPath != filepath.Join(dir, "sing-box.json") {
		t.Fatalf("SingBoxConfigPath %s", st.SingBoxConfigPath)
	}
	if st.SystemVPN.NMConnectionID != "PTsecurity" {
		t.Fatalf("NMConnectionID %s", st.SystemVPN.NMConnectionID)
	}
}

func TestSubscriptionRefreshInterval(t *testing.T) {
	if (config.Subscription{}).RefreshInterval() != 0 {
		t.Fatal("zero minutes")
	}
	s := config.Subscription{RefreshIntervalMinutes: 15}
	if s.RefreshInterval() != 15*time.Minute {
		t.Fatalf("%v", s.RefreshInterval())
	}
}

func TestStoreSaveLoadPlain(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(st *config.Settings) {
		st.MainInterface = "eth0"
		st.PersonalVPN.Enabled = true
	}); err != nil {
		t.Fatal(err)
	}

	store2, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := store2.Get()
	if got.MainInterface != "eth0" || !got.PersonalVPN.Enabled {
		t.Fatalf("%+v", got)
	}
}

func TestStoreLegacyWorkVPNMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	legacy := `{"workVpn":{"nmConnectionId":"LegacyVPN"},"mainInterface":"wlan0"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.Get().SystemVPN.NMConnectionID != "LegacyVPN" {
		t.Fatalf("%+v", store.Get().SystemVPN)
	}
}

func TestStoreLockUnlockRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(st *config.Settings) {
		st.MainInterface = "secret-iface"
	}); err != nil {
		t.Fatal(err)
	}
	pass := "test-passphrase-ok"
	if err := store.Lock(pass); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json.enc")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("plain should be removed: %v", err)
	}

	store2, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store2.TryLoad(); !os.IsNotExist(err) {
		t.Fatalf("TryLoad want ErrNotExist for enc, got %v", err)
	}
	if err := store2.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if store2.Get().MainInterface != "secret-iface" {
		t.Fatalf("%+v", store2.Get())
	}
}

func TestStoreUnlockBadPassphrase(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Lock("correct-horse"); err != nil {
		t.Fatal(err)
	}
	store2, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store2.TryLoad()
	if err := store2.Unlock("wrong"); err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreLockEmptyPassphrase(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Lock(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestTryLoadPlain(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlain(); err != nil {
		t.Fatal(err)
	}
	if err := store.TryLoad(); err != nil {
		t.Fatal(err)
	}
}
