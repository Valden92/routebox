package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestNetworkSnapshotCollectsEachSourceOnce(t *testing.T) {
	for _, up := range []bool{false, true} {
		t.Run(map[bool]string{false: "offline", true: "coexist"}[up], func(t *testing.T) {
			st := config.DefaultSettings(t.TempDir())
			st.MainInterface = ""
			calls := map[string]int{}
			routes := []string{"10.23.0.0/16"}
			endpoints := []string{"203.0.113.8/32"}
			snapshot := captureRouterNetwork(st, routerNetworkSources{
				vpn: func(config.Settings) (bool, string) {
					calls["vpn"]++
					if calls["vpn"] > 1 {
						return !up, "tun-other"
					}
					return up, "tun9"
				},
				dns: func(iface string) string {
					calls["dns"]++
					if iface != "tun9" {
						t.Fatalf("DNS from wrong interface: %s", iface)
					}
					return "10.23.0.53"
				},
				routes: func(iface string) []string {
					calls["routes"]++
					if iface != "tun9" {
						t.Fatalf("routes from wrong interface: %s", iface)
					}
					return routes
				},
				endpoints:     func(config.Settings) []string { calls["endpoints"]++; return endpoints },
				mainInterface: func() string { calls["main"]++; return "wlan-test" },
			})
			if calls["vpn"] != 1 || calls["main"] != 1 || snapshot.SystemVPNUp != up || snapshot.MainInterface != "wlan-test" {
				t.Fatalf("inconsistent collection: %+v %v", snapshot, calls)
			}
			want := 0
			if up {
				want = 1
			}
			for _, name := range []string{"dns", "routes", "endpoints"} {
				if calls[name] != want {
					t.Fatalf("%s read %d times", name, calls[name])
				}
			}
			routes[0] = "changed"
			endpoints[0] = "changed"
			if up && (snapshot.WorkRoutes[0] != "10.23.0.0/16" || snapshot.EndpointCIDRs[0] != "203.0.113.8/32") {
				t.Fatal("snapshot aliases mutable source arrays")
			}
		})
	}
}

func TestNetworkSnapshotKeepsConfiguredMainInterface(t *testing.T) {
	st := config.DefaultSettings(t.TempDir())
	st.MainInterface = "configured"
	snapshot := captureRouterNetwork(st, routerNetworkSources{vpn: func(config.Settings) (bool, string) { return false, "" }, mainInterface: func() string { t.Fatal("unnecessary interface discovery"); return "" }})
	if snapshot.MainInterface != "configured" || snapshot.SystemInterface != "tun0" {
		t.Fatalf("unexpected defaults: %+v", snapshot)
	}
}

func TestConfigUsesCapturedNetworkWithoutHostReads(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "unexpected-host-read")
	for _, name := range []string{"nmcli", "ip", "resolvectl"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf 'called\\n' >> \"$SNAPSHOT_TEST_MARKER\"\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("SNAPSHOT_TEST_MARKER", marker)
	st := config.DefaultSettings(dir)
	st.MainInterface = "different-configured-interface"
	st.PersonalVPN.Enabled = true
	node := &subscription.Node{RawURI: "hy2://test-secret@hy2.example:443?pinSHA256=aabbcc"}
	for _, up := range []bool{true, false} {
		snapshot := RouterNetworkSnapshot{SystemVPNUp: up, SystemInterface: "tun9", SystemDNS: "10.23.0.53", WorkRoutes: []string{"10.23.0.0/16"}, EndpointCIDRs: []string{"203.0.113.8/32"}, MainInterface: "wlan-snapshot"}
		path := filepath.Join(dir, "config.json")
		if err := WriteRouterConfigWithSnapshot(path, node, st, "tun100", snapshot); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = ValidateConfigBytes(body, up); err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Outbounds []map[string]any `json:"outbounds"`
			DNS       struct {
				Servers []map[string]any `json:"servers"`
				Rules   []map[string]any `json:"rules"`
			} `json:"dns"`
			Route struct {
				Rules []map[string]any `json:"rules"`
			} `json:"route"`
			Inbounds []map[string]any `json:"inbounds"`
		}
		if err = json.Unmarshal(body, &cfg); err != nil {
			t.Fatal(err)
		}
		work := false
		for _, out := range cfg.Outbounds {
			if out["tag"] == "direct" && out["bind_interface"] != "wlan-snapshot" {
				t.Fatal("main interface was re-read")
			}
			if out["tag"] == "work" {
				work = true
				if out["bind_interface"] != "tun9" {
					t.Fatal("work outbound differs from snapshot")
				}
			}
			if out["tag"] == "proxy" {
				if out["tls"].(map[string]any)["insecure"] != true {
					t.Fatal("Hysteria2 compatibility changed")
				}
			}
		}
		if work != up {
			t.Fatal("work outbound availability differs from snapshot")
		}
		workDNS := false
		for _, server := range cfg.DNS.Servers {
			if server["tag"] == "dns-work" {
				workDNS = true
				if server["server"] != "10.23.0.53" || server["detour"] != "work" {
					t.Fatal("DNS differs from snapshot")
				}
			}
		}
		if workDNS != up {
			t.Fatal("DNS availability differs from snapshot")
		}
		corporateRoute := false
		for _, rule := range cfg.Route.Rules {
			if rule["outbound"] == "work" {
				corporateRoute = true
			}
		}
		if corporateRoute != up {
			t.Fatal("routes differ from snapshot")
		}
		if up {
			tun := cfg.Inbounds[0]
			if !strings.Contains(string(body), "203.0.113.8/32") || tun["exclude_interface"].([]any)[0] != "tun9" {
				t.Fatal("TUN exclusions differ from snapshot")
			}
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("config generation read live OS state: %v", err)
	}
}

func TestCoexistWithoutDNSDoesNotWriteBrokenConfig(t *testing.T) {
	st := config.DefaultSettings(t.TempDir())
	path := filepath.Join(st.DataDir, "sing-box.json")
	if err := os.WriteFile(path, []byte("working"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []RouterNetworkSnapshot{
		{SystemVPNUp: true, SystemInterface: "tun9"},
		{SystemVPNUp: true, SystemDNS: "10.23.0.53"},
		{SystemVPNUp: true, SystemInterface: "tun100", SystemDNS: "10.23.0.53"},
		{SystemVPNUp: true, SystemInterface: "wlan0", MainInterface: "wlan0", SystemDNS: "10.23.0.53"},
	} {
		if err := WriteRouterConfigWithSnapshot(path, nil, st, "tun100", snapshot); err == nil {
			t.Fatal("incomplete coexist snapshot was accepted")
		}
		body, err := os.ReadFile(path)
		if err != nil || string(body) != "working" {
			t.Fatalf("working config overwritten: %v", err)
		}
	}
	var servers []map[string]any
	rules := buildDNSRules(st, false, true, "", &servers)
	for _, rule := range rules {
		if rule["server"] == "dns-work" {
			t.Fatal("rule references missing DNS server")
		}
	}
}
