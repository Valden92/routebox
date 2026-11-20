package subscription_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestClashHysteria2RetainsCertificatePinIntent(t *testing.T) {
	for _, field := range []string{"fingerprint: AA:BB:CC", "fingerprint: \"\"", "fingerprint: null", "fingerprint: []", "fingerprint: AA:BB:CC\n    skip-cert-verify: true"} {
		t.Run(field, func(t *testing.T) {
			body := "proxies:\n  - name: pinned\n    type: hysteria2\n    server: hy2.example\n    port: 443\n    password: secret\n    " + field + "\n"
			nodes, skipped, err := subscription.ParseClash([]byte(body))
			if err != nil || skipped != 0 || len(nodes) != 1 {
				t.Fatalf("import: nodes=%d skipped=%d err=%v", len(nodes), skipped, err)
			}
			u, err := url.Parse(nodes[0].RawURI)
			if err != nil {
				t.Fatal(err)
			}
			if _, present := u.Query()["pinSHA256"]; !present {
				t.Fatal("import silently dropped certificate pin")
			}
			if out, err := singbox.URIToOutbound(nodes[0]); !errors.Is(err, singbox.ErrUnsupportedCertificatePin) || out != nil {
				t.Fatalf("unsupported imported pin became usable: outbound=%v err=%v", out, err)
			}
		})
	}
}
