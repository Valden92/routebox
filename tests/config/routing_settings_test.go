package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestLegacyRoutingSettingsMigrateWithoutChangingFallback(t *testing.T) {
	for _, legacyPath := range []string{"personal", "work", "direct", "unknown", ""} {
		for _, flag := range []string{"true", "false", "missing"} {
			t.Run(legacyPath+"/"+flag, func(t *testing.T) {
				dir := t.TempDir()
				legacy := map[string]any{"defaultPath": legacyPath, "mainInterface": "fixture-main"}
				if flag != "missing" {
					legacy["autoDetectRouting"] = flag == "true"
				}
				body, err := json.Marshal(legacy)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, "settings.json")
				if err = os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
				store, err := config.NewStore(dir)
				if err != nil {
					t.Fatal(err)
				}
				if store.Get().AutoDetectRouting != (flag != "false") || store.Get().MainInterface != "fixture-main" {
					t.Fatal("migration lost the explicit learning setting or unrelated settings")
				}
				if err = store.SavePlain(); err != nil {
					t.Fatal(err)
				}
				body, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var saved map[string]any
				if err = json.Unmarshal(body, &saved); err != nil {
					t.Fatal(err)
				}
				if _, found := saved["defaultPath"]; found {
					t.Fatal("retired defaultPath persisted")
				}
				// Both reload and unlock use the same migration; keep the explicit false.
				if err = store.Lock("fixture-passphrase"); err != nil {
					t.Fatal(err)
				}
				reopened, err := config.NewStore(dir)
				if err != nil {
					t.Fatal(err)
				}
				if err = reopened.Unlock("fixture-passphrase"); err != nil {
					t.Fatal(err)
				}
				if reopened.Get().AutoDetectRouting != (flag != "false") {
					t.Fatal("unlock changed the learning setting")
				}
			})
		}
	}
}
