package subscription_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Valden92/routebox/internal/subscription"
)

func TestParseOvpnInline(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "sample.ovpn"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := subscription.ParseOvpn(string(raw), "Work OVPN")
	if err != nil {
		t.Fatal(err)
	}
	if n.Protocol != "openvpn" || n.Host != "vpn.example.com" || n.Port != 1194 {
		t.Fatalf("%+v", n)
	}
	if n.Network != "udp" || !n.AuthUserPass {
		t.Fatalf("net/auth %+v", n)
	}
	if len(n.CA) == 0 || len(n.Cert) == 0 || len(n.Key) == 0 || len(n.TLSAuth) == 0 {
		t.Fatalf("missing pem blocks ca=%d cert=%d key=%d tls=%d", len(n.CA), len(n.Cert), len(n.Key), len(n.TLSAuth))
	}
	if n.KeyDirection != "1" {
		t.Fatalf("key-direction %q", n.KeyDirection)
	}
	if n.Name != "Work OVPN" {
		t.Fatalf("name %q", n.Name)
	}
	if !subscription.ValidNode(n) {
		t.Fatal("ValidNode false")
	}
}

func TestParseOvpnRejectsExternalPaths(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "external-paths.ovpn"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = subscription.ParseOvpn(string(raw), "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseOvpnRejectsChallenge(t *testing.T) {
	_, err := subscription.ParseOvpn("client\nremote a 1194\nstatic-challenge foo 1\n<ca>\nX\n</ca>\n", "x")
	if err == nil {
		t.Fatal("expected error")
	}
}
