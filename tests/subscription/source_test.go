package subscription_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/subscription"
)

func TestInferSource(t *testing.T) {
	if got := subscription.InferSource("https://x.example/sub", nil); got != "url" {
		t.Fatalf("url %q", got)
	}
	if got := subscription.InferSource("", []subscription.Node{{Protocol: "openvpn", Host: "h", Port: 1}}); got != "ovpn" {
		t.Fatalf("ovpn %q", got)
	}
	if got := subscription.InferSource("", []subscription.Node{{
		Protocol: "vless", Host: "h", Port: 443, RawURI: "vless://x@h:443",
	}}); got != "uri" {
		t.Fatalf("uri %q", got)
	}
	if got := subscription.InferSource("", []subscription.Node{
		{Protocol: "vless", Host: "a", Port: 1},
		{Protocol: "ss", Host: "b", Port: 2},
	}); got != "text" {
		t.Fatalf("text %q", got)
	}
}

func TestNormalizeSource(t *testing.T) {
	if subscription.NormalizeSource("FILE") != "text" {
		t.Fatal("file→text")
	}
	if subscription.NormalizeSource("ovpn") != "ovpn" {
		t.Fatal("ovpn")
	}
	if subscription.NormalizeSource("clash") != "clash" {
		t.Fatal("clash")
	}
}
