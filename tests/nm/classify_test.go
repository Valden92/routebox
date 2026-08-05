package nm_test

import (
	"errors"
	"testing"

	"github.com/Valden92/routebox/internal/nm"
)

func TestHumanVpnType(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"org.freedesktop.NetworkManager.openvpn", "OpenVPN"},
		{"org.freedesktop.NetworkManager.wireguard", "WireGuard"},
		{"openconnect", "OpenConnect"},
		{"customthing", "Customthing"},
	}
	for _, tc := range cases {
		if got := nm.HumanVpnType(tc.in); got != tc.want {
			t.Fatalf("%q → %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyNMState(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"100 (connected)", "connected"},
		{"50", "connecting"},
		{"110", "connecting"},
		{"20", "disconnected"},
		{"activated", "connected"},
		{"activating", "connecting"},
		{"need auth", "connecting"},
		{"подключение", "connected"}, // содержит «подключен»
		{"подключено", "connected"},
		{"unknown-xyz", "disconnected"},
	}
	for _, tc := range cases {
		if got := nm.ClassifyNMState(tc.in); got != tc.want {
			t.Fatalf("%q → %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsBenignErrorsNilAndPlain(t *testing.T) {
	if nm.IsBenignDisconnectErr(nil) || nm.IsBenignConnectErr(nil) {
		t.Fatal("nil")
	}
	if nm.IsBenignDisconnectErr(errors.New("plain")) || nm.IsBenignConnectErr(errors.New("plain")) {
		t.Fatal("plain")
	}
}

func TestHumanizeErr(t *testing.T) {
	if nm.HumanizeErr(nil) != "" {
		t.Fatal()
	}
	msg := nm.HumanizeErr(errors.New("  boom  "))
	if msg == "" {
		t.Fatal("empty")
	}
}
