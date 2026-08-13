package subscription_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/subscription"
)

func TestParseUserinfo(t *testing.T) {
	up, down, total, exp, ok := subscription.ParseUserinfo(
		"upload=100; download=200; total=1024; expire=1735689600",
	)
	if !ok || up != 100 || down != 200 || total != 1024 || exp != 1735689600 {
		t.Fatalf("got up=%d down=%d total=%d exp=%d ok=%v", up, down, total, exp, ok)
	}
}

func TestParseUserinfoPartialAndNoise(t *testing.T) {
	up, down, total, exp, ok := subscription.ParseUserinfo("download=50; foo=bar; total=abc; upload=10")
	if !ok || up != 10 || down != 50 || total != 0 || exp != 0 {
		t.Fatalf("got up=%d down=%d total=%d exp=%d ok=%v", up, down, total, exp, ok)
	}
	if _, _, _, _, ok := subscription.ParseUserinfo(""); ok {
		t.Fatal("empty should not ok")
	}
	if _, _, _, _, ok := subscription.ParseUserinfo("nonsense"); ok {
		t.Fatal("nonsense should not ok")
	}
}

func TestMetaFromHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Subscription-Userinfo", "upload=1; download=2; total=10; expire=1700000000")
	h.Set("Profile-Title", "base64:UXVhdHRybw==") // "Quattro"
	h.Set("Announce", "base64:0J/QvtC00L/QuNGB0LrQsDogNDMxOTk3NzY2")
	h.Set("Support-Url", "https://support.example")
	h.Set("Profile-Update-Interval", "24")
	m := subscription.MetaFromHeaders(h)
	if !m.HasUserinfo || m.Upload != 1 || m.Download != 2 || m.Total != 10 || m.ExpireUnix != 1700000000 {
		t.Fatalf("userinfo %+v", m)
	}
	if m.ProfileTitle != "Quattro" {
		t.Fatalf("title %q", m.ProfileTitle)
	}
	if !strings.Contains(m.Announce, "Подписка:") {
		t.Fatalf("announce %q", m.Announce)
	}
	if m.SupportURL != "https://support.example" {
		t.Fatalf("support %q", m.SupportURL)
	}
	if m.ProfileUpdateIntervalHours != 24 {
		t.Fatalf("interval %d", m.ProfileUpdateIntervalHours)
	}
	got := m.ExpireTime()
	want := time.Unix(1700000000, 0).UTC()
	if !got.Equal(want) {
		t.Fatalf("expire %v want %v", got, want)
	}
}

func TestDecodeHeaderText(t *testing.T) {
	if got := subscription.DecodeHeaderText("plain"); got != "plain" {
		t.Fatalf("%q", got)
	}
	if got := subscription.DecodeHeaderText("base64:SGVsbG8="); got != "Hello" {
		t.Fatalf("%q", got)
	}
	if got := subscription.DecodeHeaderText("BASE64:SGVsbG8"); got != "Hello" { // raw / pad
		t.Fatalf("pad %q", got)
	}
}

func TestMetaFromHeadersEmpty(t *testing.T) {
	m := subscription.MetaFromHeaders(http.Header{})
	if m.HasUserinfo || m.ProfileTitle != "" || !m.ExpireTime().IsZero() {
		t.Fatalf("%+v", m)
	}
}
