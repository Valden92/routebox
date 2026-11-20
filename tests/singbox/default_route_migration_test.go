package singbox_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestLegacyDefaultPathKeepsDirectFallback(t *testing.T) {
	for _, legacy := range []string{"personal", "work", "direct"} {
		t.Run(legacy, func(t *testing.T) {
			dir := t.TempDir()
			body := []byte(`{"defaultPath":"` + legacy + `","personalVpn":{"enabled":true}}`)
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), body, 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := config.NewStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			node := &subscription.Node{Host: "gateway.provider.test", RawURI: "hy2://fixture@gateway.provider.test:443"}
			path := filepath.Join(dir, "candidate.json")
			// Both VPN paths are available: migration still must not select them globally.
			snapshot := singbox.RouterNetworkSnapshot{SystemVPNUp: true, SystemInterface: "tun0", SystemDNS: "10.23.0.53", MainInterface: "fixture-main"}
			if err = singbox.WriteRouterConfigWithSnapshot(path, node, store.Get(), "tun100", snapshot); err != nil {
				t.Fatal(err)
			}
			body, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var cfg domainConfig
			if err = json.Unmarshal(body, &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.Route.Final != "direct" || cfg.DNS.Final != "dns-direct" {
				t.Fatalf("legacy setting changed default route: route=%s DNS=%s", cfg.Route.Final, cfg.DNS.Final)
			}
		})
	}
}
