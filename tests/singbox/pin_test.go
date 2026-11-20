package singbox_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestHysteria2TLSVerificationRequiresExplicitInsecure(t *testing.T) {
	for _, tc := range []struct {
		query    string
		insecure bool
	}{
		{"", false}, {"insecure=0", false}, {"insecure=false", false},
		{"insecure=1", true}, {"insecure=true", true}, {"allowInsecure=1", true},
		{"allow_insecure=true", true}, {"skipCertVerify=true", true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			node := subscription.Node{RawURI: "hy2://secret@hy2.example:8443?sni=sni.example&obfs=salamander&obfs-password=x&alpn=h3,h3-29&" + tc.query}
			o, err := singbox.URIToOutbound(node)
			if err != nil {
				t.Fatal(err)
			}
			tls, ok := o["tls"].(map[string]any)
			if !ok || tls["enabled"] != true || tls["server_name"] != "sni.example" || (tls["insecure"] == true) != tc.insecure {
				t.Fatalf("TLS policy: %v", tls)
			}
			if !tc.insecure {
				if _, present := tls["insecure"]; present {
					t.Fatal("verification was overridden without explicit request")
				}
			}
			if alpn, hasALPN := tls["alpn"].([]string); !hasALPN || strings.Join(alpn, ",") != "h3,h3-29" {
				t.Fatalf("ALPN lost: %v", tls["alpn"])
			}
			obfs, ok := o["obfs"].(map[string]any)
			if !ok || obfs["type"] != "salamander" || obfs["password"] != "x" {
				t.Fatalf("obfuscation lost: %v", o["obfs"])
			}
		})
	}
}

func TestHysteria2RejectsMalformedQueryWithoutLeakingSecrets(t *testing.T) {
	for _, query := range []string{"pinSHA256=%zz&insecure=1", "pinSHA256=secret-pin;insecure=1", "pinSHA256=secret-pin&other=%zz"} {
		t.Run(query, func(t *testing.T) {
			o, err := singbox.URIToOutbound(subscription.Node{RawURI: "hy2://auth-secret@hy2.example:443?" + query})
			if err == nil || o != nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("malformed query not safely rejected: %v %v", o, err)
			}
		})
	}
}

func TestPinnedHysteria2CannotReplaceRouterConfig(t *testing.T) {
	st := config.DefaultSettings(t.TempDir())
	st.PersonalVPN.Enabled = true
	path := filepath.Join(st.DataDir, "sing-box.json")
	before := []byte("existing working config")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	node := subscription.Node{RawURI: "hy2://secret@hy2.example:443?pinSHA256=unsupported"}
	if err := singbox.WriteRouterConfig(path, &node, st, "tun100"); !errors.Is(err, singbox.ErrUnsupportedCertificatePin) {
		t.Fatalf("expected pin refusal: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("existing config changed: %v", err)
	}
}

func TestProbeConfigExcludesPinnedHysteria2(t *testing.T) {
	st := config.DefaultSettings(t.TempDir())
	path := filepath.Join(st.DataDir, "probe.json")
	pinned := subscription.Node{RawURI: "hy2://secret@hy2.example:443?pinSHA256=unsupported"}
	plain := subscription.Node{RawURI: "hy2://secret@hy2.example:443?sni=hy2.example"}
	if err := singbox.WriteProbeConfig(path, []subscription.Node{pinned}, st); err == nil {
		t.Fatal("probe accepted only unsupported pinned nodes")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed probe wrote a config: %v", err)
	}
	if err := singbox.WriteProbeConfig(path, []subscription.Node{pinned, plain}, st); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, out := range cfg.Outbounds {
		if out["tag"] == singbox.ProbeNodeTag(0) {
			t.Fatal("pinned node was emitted as a probe outbound")
		}
		if out["tag"] == singbox.ProbeNodeTag(1) {
			found = true
		}
		if out["type"] == "selector" {
			for _, tag := range out["outbounds"].([]any) {
				if tag == singbox.ProbeNodeTag(0) {
					t.Fatal("selector can choose a rejected pin")
				}
			}
		}
	}
	if !found {
		t.Fatal("ordinary Hysteria2 node was lost")
	}
}
