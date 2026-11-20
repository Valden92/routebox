package config_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func newStore(t *testing.T, dir string) *config.Store {
	t.Helper()
	s, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func readSettingsFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncryptedSettingsSurviveUpdatesAndRestarts(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t, dir)
	if err := s.Update(func(st *config.Settings) {
		st.Subscriptions = []config.Subscription{{ID: "secret-subscription", URL: "https://example.invalid/private-token"}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Lock("first-password"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json.enc")
	before := readSettingsFile(t, path)
	if err := s.Update(func(st *config.Settings) {
		st.MainInterface = "secret-interface"
		st.Encrypted = false // callers cannot disable the storage mode
		st.DataDir = "/untrusted"
	}); err != nil {
		t.Fatal(err)
	}
	after := readSettingsFile(t, path)
	if bytes.Equal(before, after) {
		t.Fatal("encrypted update was not saved")
	}
	for _, secret := range []string{"secret-interface", "private-token", "mainInterface"} {
		if bytes.Contains(after, []byte(secret)) {
			t.Fatalf("plaintext %q leaked", secret)
		}
	}
	var oldBlob, newBlob struct{ Salt, Nonce []byte }
	if err := json.Unmarshal(before, &oldBlob); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &newBlob); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldBlob.Salt, newBlob.Salt) || bytes.Equal(oldBlob.Nonce, newBlob.Nonce) {
		t.Fatal("save should retain the key salt and use a fresh nonce")
	}
	if err := s.SavePlain(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected plaintext: %v", err)
	}
	for range 2 {
		s = newStore(t, dir)
		if !s.IsLocked() || !s.Get().Encrypted {
			t.Fatal("restart must require unlock")
		}
		before = readSettingsFile(t, path)
		for range 2 {
			if err := s.TryLoad(); !errors.Is(err, config.ErrLocked) {
				t.Fatalf("TryLoad: %v", err)
			}
		}
		called := false
		if err := s.Update(func(*config.Settings) { called = true }); !errors.Is(err, config.ErrLocked) || called {
			t.Fatalf("locked update: %v, callback=%v", err, called)
		}
		for _, save := range []func() error{s.SavePlain, func() error { return s.Lock("replacement") }} {
			if err := save(); !errors.Is(err, config.ErrLocked) {
				t.Fatalf("locked save: %v", err)
			}
		}
		if !bytes.Equal(before, readSettingsFile(t, path)) {
			t.Fatal("locked store overwrote settings")
		}
		if err := s.Unlock("wrong-password"); err == nil || !s.IsLocked() {
			t.Fatal("wrong password changed state")
		}
		if !bytes.Equal(before, readSettingsFile(t, path)) {
			t.Fatal("wrong password changed file")
		}
		if err := s.Unlock("first-password"); err != nil {
			t.Fatal(err)
		}
		got := s.Get()
		if s.IsLocked() || !got.Encrypted || got.MainInterface != "secret-interface" || got.DataDir != dir || len(got.Subscriptions) != 1 {
			t.Fatalf("lost settings: %+v", got)
		}
		if s.Path() != path {
			t.Fatalf("unstable path %s", s.Path())
		}
		if err := s.Update(func(st *config.Settings) { st.Subscriptions[0].Name = "updated" }); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions %o", info.Mode().Perm())
	}
}

func TestRepeatedLockChangesPasswordWithoutAddingSuffix(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t, dir)
	if err := s.Lock("old-password"); err != nil {
		t.Fatal(err)
	}
	if err := s.Lock("new-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json.enc.enc")); !os.IsNotExist(err) {
		t.Fatalf("extra suffix: %v", err)
	}
	s = newStore(t, dir)
	if err := s.Unlock("old-password"); err == nil {
		t.Fatal("old password still accepted")
	}
	if err := s.Unlock("new-password"); err != nil {
		t.Fatal(err)
	}
}

func TestEncryptedFileTakesPrecedenceOverStalePlaintext(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t, dir)
	if err := s.Update(func(st *config.Settings) { st.MainInterface = "encrypted-version" }); err != nil {
		t.Fatal(err)
	}
	if err := s.Lock("password"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"mainInterface":"stale"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s = newStore(t, dir)
	if !s.IsLocked() {
		t.Fatal("plaintext bypassed encryption")
	}
	if err := s.Unlock("password"); err != nil {
		t.Fatal(err)
	}
	if s.Get().MainInterface != "encrypted-version" {
		t.Fatal("loaded stale plaintext")
	}
	if err := os.Remove(s.Path()); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(); !os.IsNotExist(err) {
		t.Fatalf("missing encrypted file should not load stale plaintext: %v", err)
	}
}

func TestMalformedEncryptedSettingsNeverPanicOrOverwrite(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t, dir)
	if err := s.Lock("password"); err != nil {
		t.Fatal(err)
	}
	var valid map[string]any
	if err := json.Unmarshal(readSettingsFile(t, s.Path()), &valid); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"unknown version", func(b map[string]any) { b["version"] = 2 }},
		{"missing version", func(b map[string]any) { delete(b, "version") }},
		{"short salt", func(b map[string]any) { b["salt"] = "AA==" }},
		{"missing nonce", func(b map[string]any) { delete(b, "nonce") }},
		{"short nonce", func(b map[string]any) { b["nonce"] = "AA==" }},
		{"short ciphertext", func(b map[string]any) { b["data"] = "AA==" }},
		{"invalid base64", func(b map[string]any) { b["nonce"] = "!" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := make(map[string]any, len(valid))
			for k, v := range valid {
				b[k] = v
			}
			tc.change(b)
			raw, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "settings.json.enc")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			store := newStore(t, filepath.Dir(path))
			if err := store.Unlock("password"); err == nil {
				t.Fatal("malformed blob accepted")
			}
			if !store.IsLocked() || !bytes.Equal(raw, readSettingsFile(t, path)) {
				t.Fatal("bad blob changed state or file")
			}
		})
	}
}

func TestFailedSaveKeepsMemoryAndPreviousFile(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "encrypted"}[encrypted], func(t *testing.T) {
			dir := t.TempDir()
			s := newStore(t, dir)
			if err := s.Update(func(st *config.Settings) {
				st.Subscriptions = []config.Subscription{{ID: "original", NodeSelectCounts: map[string]int{"node": 1}}}
			}); err != nil {
				t.Fatal(err)
			}
			if encrypted {
				if err := s.Lock("password"); err != nil {
					t.Fatal(err)
				}
			}
			path := s.Path()
			before := readSettingsFile(t, path)
			snapshot := s.Get()
			// A directory at the destination causes the final rename to fail, even as root.
			if err := os.Rename(path, path+".previous"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			err := s.Update(func(st *config.Settings) {
				st.MainInterface = "failed"
				st.Subscriptions[0].NodeSelectCounts["node"] = 99
			})
			if err == nil {
				t.Fatal("expected write failure")
			}
			if !reflect.DeepEqual(snapshot, s.Get()) {
				t.Fatal("failed save changed memory")
			}
			if !bytes.Equal(before, readSettingsFile(t, path+".previous")) {
				t.Fatal("failed save damaged previous file")
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, ".settings-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary files leaked: %v, %v", leftovers, err)
			}
		})
	}
}

func TestSettingsSnapshotsDoNotShareReferences(t *testing.T) {
	s := newStore(t, t.TempDir())
	fragment := true
	input := config.Settings{Subscriptions: []config.Subscription{{ID: "original", NodeSelectCounts: map[string]int{"n": 1}, NodeAutoSelectWeights: map[string]int{"n": 2}}}, DomainRules: []config.DomainRule{{ID: "rule"}}, AppRules: []config.AppRule{{ID: "app"}}, PersonalVPN: config.PersonalVPN{TLSFragment: &fragment}}
	var retained *config.Settings
	if err := s.Update(func(st *config.Settings) {
		st.Subscriptions = input.Subscriptions
		st.DomainRules = input.DomainRules
		st.AppRules = input.AppRules
		st.PersonalVPN = input.PersonalVPN
		retained = st
	}); err != nil {
		t.Fatal(err)
	}
	original := s.Get()
	mutate := func(st *config.Settings) {
		st.Subscriptions[0].ID = "changed"
		st.Subscriptions[0].NodeSelectCounts["n"] = 99
		st.Subscriptions[0].NodeAutoSelectWeights["n"] = 99
		st.DomainRules[0].ID = "changed"
		st.AppRules[0].ID = "changed"
		*st.PersonalVPN.TLSFragment = false
	}
	snapshot := s.Get()
	mutate(&snapshot)
	mutate(&input)
	mutate(retained)
	if !reflect.DeepEqual(original, s.Get()) {
		t.Fatal("external mutation escaped the transaction")
	}
}

func TestConcurrentEncryptedReadsAndUpdates(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t, dir)
	if err := s.Update(func(st *config.Settings) {
		st.Subscriptions = []config.Subscription{{NodeSelectCounts: map[string]int{"n": 0}}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Lock("password"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 15 {
				snapshot := s.Get()
				snapshot.Subscriptions[0].NodeSelectCounts["n"] = -100
				_ = s.Path()
				_ = s.IsLocked()
				if err := s.Update(func(st *config.Settings) { st.Subscriptions[0].NodeSelectCounts["n"]++ }); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if s.Get().Subscriptions[0].NodeSelectCounts["n"] != 60 {
		t.Fatal("lost concurrent update")
	}
	restarted := newStore(t, dir)
	if err := restarted.Unlock("password"); err != nil {
		t.Fatal(err)
	}
	if restarted.Get().Subscriptions[0].NodeSelectCounts["n"] != 60 {
		t.Fatal("disk differs from memory")
	}
}

func TestLegacyEncryptedFormatKeepsDefaultsAndMigration(t *testing.T) {
	// Fixture uses the original v1 PBKDF2/AES-GCM format and an encrypted:false payload.
	raw := readSettingsFile(t, filepath.Join("testdata", "legacy-v1.fixture.json"))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json.enc"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s := newStore(t, dir)
	if err := s.Unlock("legacy-password"); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	if !got.Encrypted || got.DataDir != dir || got.MainInterface != "legacy-encrypted" || got.SystemVPN.NMConnectionID != "LegacyVPN" || got.APIListen != "127.0.0.1:47891" || got.SingBoxPath == "" || got.SingBoxConfigPath != filepath.Join(dir, "sing-box.json") {
		t.Fatalf("compatibility load: %+v", got)
	}
	if err := s.SavePlain(); err != nil {
		t.Fatal(err)
	}
	if err := s.TryLoad(); err != nil {
		t.Fatal(err)
	}
	restarted := newStore(t, dir)
	if err := restarted.Unlock("legacy-password"); err != nil {
		t.Fatal(err)
	}
	if !restarted.Get().Encrypted {
		t.Fatal("legacy payload disabled encryption")
	}
}

func TestLockWriteFailureKeepsPreviousModeAndPassword(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "encrypted"}[encrypted], func(t *testing.T) {
			dir := t.TempDir()
			s := newStore(t, dir)
			if err := s.Update(func(st *config.Settings) { st.MainInterface = "original" }); err != nil {
				t.Fatal(err)
			}
			if encrypted {
				if err := s.Lock("old-password"); err != nil {
					t.Fatal(err)
				}
			}
			original := s.Get()
			oldPath := s.Path()
			before := readSettingsFile(t, oldPath)
			encPath := filepath.Join(dir, "settings.json.enc")
			if encrypted {
				if err := os.Rename(encPath, encPath+".previous"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(encPath, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := s.Lock("new-password"); err == nil {
				t.Fatal("expected lock write failure")
			}
			if !reflect.DeepEqual(original, s.Get()) || s.Path() != oldPath {
				t.Fatal("failed lock changed mode or settings")
			}
			if err := os.Remove(encPath); err != nil {
				t.Fatal(err)
			}
			if encrypted {
				if err := os.Rename(encPath+".previous", encPath); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(before, readSettingsFile(t, oldPath)) {
				t.Fatal("failed lock changed file")
			}
			if err := s.SavePlain(); err != nil {
				t.Fatal(err)
			}
			restarted := newStore(t, dir)
			if encrypted {
				if err := restarted.Unlock("old-password"); err != nil {
					t.Fatal(err)
				}
			}
			if restarted.Get().MainInterface != "original" {
				t.Fatal("failed lock lost settings")
			}
		})
	}
}

func TestPlaintextCleanupFailureIsReportedWithoutDowngrading(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t, dir)
	plainPath := filepath.Join(dir, "settings.json")
	if err := os.Mkdir(plainPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plainPath, "obstacle"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Lock("password"); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	if !s.Get().Encrypted || s.IsLocked() {
		t.Fatal("committed encryption/key was discarded")
	}
	if err := s.SavePlain(); err != nil {
		t.Fatal(err)
	}
	restarted := newStore(t, dir)
	if err := restarted.Unlock("password"); err != nil {
		t.Fatal(err)
	}
}
