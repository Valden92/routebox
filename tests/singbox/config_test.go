package singbox_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestURIToOutboundVLESS(t *testing.T) {
	node := subscription.Node{
		RawURI: "vless://11111111-1111-1111-1111-111111111111@vless.example.com:443?encryption=none&security=tls&sni=vless.example.com&fp=chrome#N",
	}
	o, err := singbox.URIToOutbound(node)
	if err != nil {
		t.Fatal(err)
	}
	if o["type"] != "vless" || o["tag"] != "proxy" || o["server"] != "vless.example.com" {
		t.Fatalf("%v", o)
	}
	if o["server_port"] != 443 {
		t.Fatalf("port %v", o["server_port"])
	}
	tls, ok := o["tls"].(map[string]any)
	if !ok || tls["server_name"] != "vless.example.com" {
		t.Fatalf("tls %+v", o["tls"])
	}
}

func TestURIToOutboundHysteria2(t *testing.T) {
	node := subscription.Node{
		RawURI: "hysteria2://secret@hy2.example.com:8443?sni=hy2.example.com",
	}
	o, err := singbox.URIToOutbound(node)
	if err != nil {
		t.Fatal(err)
	}
	if o["type"] != "hysteria2" || o["password"] != "secret" || o["server_port"] != 8443 {
		t.Fatalf("%v", o)
	}
}

func TestURIToOutboundShadowsocks(t *testing.T) {
	node := subscription.Node{
		RawURI: "ss://aes-256-gcm:pass@ss.example.com:8388",
	}
	o, err := singbox.URIToOutbound(node)
	if err != nil {
		t.Fatal(err)
	}
	if o["type"] != "shadowsocks" || o["method"] != "aes-256-gcm" || o["password"] != "pass" {
		t.Fatalf("%v", o)
	}
}

func TestURIToOutboundUnsupported(t *testing.T) {
	_, err := singbox.URIToOutbound(subscription.Node{RawURI: "vmess://x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPathTagIfAvailable(t *testing.T) {
	cases := []struct {
		path           config.RoutePath
		personal, work bool
		wantTag        string
		wantOK         bool
	}{
		{config.RouteDirect, false, false, "direct", true},
		{config.RoutePersonal, true, false, "proxy", true},
		{config.RoutePersonal, false, true, "proxy", false},
		{config.RouteWork, false, true, "work", true},
		{config.RouteWork, true, false, "work", false},
	}
	for _, tc := range cases {
		tag, ok := singbox.PathTagIfAvailable(tc.path, tc.personal, tc.work)
		if tag != tc.wantTag || ok != tc.wantOK {
			t.Fatalf("%s p=%v w=%v → %s %v want %s %v",
				tc.path, tc.personal, tc.work, tag, ok, tc.wantTag, tc.wantOK)
		}
	}
}

func TestConfigFileMode(t *testing.T) {
	dir := t.TempDir()
	coexist := filepath.Join(dir, "coexist.json")
	full := filepath.Join(dir, "full.json")
	empty := filepath.Join(dir, "empty.json")
	mustWrite(t, coexist, `{"inbounds":[{"exclude_interface":["tun0"],"route_address":["0.0.0.0/1"]}],"route":{"rules":[{"protocol":"dns","action":"hijack-dns"}]}}`)
	mustWrite(t, full, `{"route":{"rules":[{"protocol":"dns","action":"hijack-dns"}]}}`)
	mustWrite(t, empty, `{}`)

	if m := singbox.ConfigFileMode(coexist); m != singbox.ConfigModeCoexist {
		t.Fatalf("coexist: %q", m)
	}
	if m := singbox.ConfigFileMode(full); m != singbox.ConfigModeFull {
		t.Fatalf("full: %q", m)
	}
	if m := singbox.ConfigFileMode(empty); m != "" {
		t.Fatalf("empty: %q", m)
	}
	if m := singbox.ConfigFileMode(filepath.Join(dir, "missing.json")); m != "" {
		t.Fatalf("missing: %q", m)
	}
}

func TestValidateConfigBytes(t *testing.T) {
	goodCoexist, _ := json.Marshal(map[string]any{
		"inbounds": []map[string]any{{
			"exclude_interface": []string{"tun0"},
			"route_address":     []string{"0.0.0.0/1"},
		}},
		"outbounds": []map[string]any{{"tag": "work"}, {"tag": "direct"}},
		"route": map[string]any{
			"rules": []map[string]any{{"protocol": "dns", "action": "hijack-dns"}},
		},
	})
	if err := singbox.ValidateConfigBytes(goodCoexist, true); err != nil {
		t.Fatal(err)
	}
	if err := singbox.ValidateConfigBytes([]byte(`{"outbounds":[]}`), false); err != nil {
		t.Fatal(err)
	}

	noHijack := []byte(`{"inbounds":[{"exclude_interface":["tun0"],"route_address":["0.0.0.0/1"]}],"outbounds":[{"tag":"work"}]}`)
	if err := singbox.ValidateConfigBytes(noHijack, true); err == nil {
		t.Fatal("expected hijack error")
	}
	noWork := []byte(`{"inbounds":[{"exclude_interface":["tun0"],"route_address":["0.0.0.0/1"]}],"outbounds":[{"tag":"direct"}],"route":{"rules":[{"action":"hijack-dns"}]}}`)
	if err := singbox.ValidateConfigBytes(noWork, true); err == nil {
		t.Fatal("expected work outbound error")
	}
	if err := singbox.ValidateConfigBytes([]byte("{"), true); err == nil {
		t.Fatal("expected json error")
	}
}

func TestWriteRouterConfigFullMode(t *testing.T) {
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
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !containsAll(s, "hijack-dns", `"tag": "proxy"`, "tun100", "personal-probe-in") {
		t.Fatalf("missing expected fields:\n%s", s)
	}
	// Без живого NM — full, не coexist
	if singbox.ConfigFileMode(path) != singbox.ConfigModeFull {
		t.Fatalf("mode %q", singbox.ConfigFileMode(path))
	}
	if err := singbox.ValidateWrittenConfig(path, st); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRouterConfigWithoutPersonal(t *testing.T) {
	dir := t.TempDir()
	st := config.DefaultSettings(dir)
	st.SystemVPN.NMConnectionID = "PTsecurity-nonexistent-ci-test"
	st.PersonalVPN.Enabled = false
	path := filepath.Join(dir, "sing-box.json")
	if err := singbox.WriteRouterConfig(path, nil, st, "tun100"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if containsAll(s, "personal-probe-in") {
		t.Fatal("probe inbound should be absent when personal disabled")
	}
	if containsAll(s, `"tag": "proxy"`) {
		t.Fatal("proxy outbound should be absent")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
