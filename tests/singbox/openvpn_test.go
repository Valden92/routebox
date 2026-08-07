package singbox_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestWriteRouterConfigOpenVPNEndpoint(t *testing.T) {
	dir := t.TempDir()
	st := config.DefaultSettings(dir)
	st.SystemVPN.NMConnectionID = "PTsecurity-nonexistent-ci-test"
	st.PersonalVPN.Enabled = true
	st.MainInterface = "lo"
	node := subscription.Node{
		Protocol:     "openvpn",
		Host:         "vpn.example.com",
		Port:         1194,
		Network:      "udp",
		ConfigText:   "client",
		AuthUserPass: true,
		Username:     "u",
		Password:     "p",
		CA:           []string{"-----BEGIN CERTIFICATE-----", "AA==", "-----END CERTIFICATE-----"},
		Cert:         []string{"-----BEGIN CERTIFICATE-----", "BB==", "-----END CERTIFICATE-----"},
		Key:          []string{"-----BEGIN PRIVATE KEY-----", "CC==", "-----END PRIVATE KEY-----"},
		TLSAuth:      []string{"-----BEGIN OpenVPN Static key V1-----", "ff", "-----END OpenVPN Static key V1-----"},
		KeyDirection: "1",
	}
	path := filepath.Join(dir, "sing-box.json")
	if err := singbox.WriteRouterConfig(path, &node, st, "tun100"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"type": "openvpn-client"`) || !strings.Contains(s, `"tag": "proxy"`) {
		t.Fatalf("missing openvpn endpoint:\n%s", s)
	}
	if !strings.Contains(s, `"route_no_pull": true`) || !strings.Contains(s, `"system": false`) {
		t.Fatalf("missing route_no_pull/system:\n%s", s)
	}
	if !strings.Contains(s, `"type": "tls"`) || strings.Contains(s, `"address": "tls://`) {
		t.Fatalf("expected typed DNS, not legacy address:\n%s", s)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg["endpoints"]; !ok {
		t.Fatal("endpoints missing")
	}
	outs, _ := cfg["outbounds"].([]any)
	for _, o := range outs {
		m, _ := o.(map[string]any)
		if m["tag"] == "proxy" {
			t.Fatal("proxy must be endpoint, not outbound")
		}
	}
	if err := checkSingBoxConfig(t, path); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRouterConfigTypedDNS(t *testing.T) {
	dir := t.TempDir()
	st := config.DefaultSettings(dir)
	st.SystemVPN.NMConnectionID = "PTsecurity-nonexistent-ci-test"
	st.PersonalVPN.Enabled = true
	st.MainInterface = "lo"
	node := subscription.Node{
		Host:   "node.example.com",
		Port:   443,
		RawURI: "vless://aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa@node.example.com:443?encryption=none&security=tls&sni=node.example.com#T",
	}
	path := filepath.Join(dir, "sing-box.json")
	if err := singbox.WriteRouterConfig(path, &node, st, "tun100"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if strings.Contains(s, `"address": "tls://`) || strings.Contains(s, `"address": "udp://`) {
		t.Fatalf("legacy DNS address present:\n%s", s)
	}
	if !strings.Contains(s, `"type": "tls"`) || !strings.Contains(s, `"tag": "dns-direct"`) {
		t.Fatalf("typed DNS missing:\n%s", s)
	}
	if !strings.Contains(s, `"dns_mode": "disabled"`) {
		t.Fatalf("dns_mode disabled missing:\n%s", s)
	}
	if !strings.Contains(s, `"iproute2_table_index": 20221`) {
		t.Fatalf("custom table index missing:\n%s", s)
	}
	if err := checkSingBoxConfig(t, path); err != nil {
		t.Fatal(err)
	}
}

func checkSingBoxConfig(t *testing.T, path string) error {
	t.Helper()
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		home := os.Getenv("HOME")
		cand := filepath.Join(home, ".local", "bin", "sing-box")
		if st, e := os.Stat(cand); e == nil && !st.IsDir() {
			bin = cand
		} else {
			t.Log("sing-box not in PATH — skip check")
			return nil
		}
	}
	out, err := exec.Command(bin, "check", "-c", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sing-box check: %w\n%s", err, out)
	}
	return nil
}
