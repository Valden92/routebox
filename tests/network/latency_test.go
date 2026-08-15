package network_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/network"
)

func TestParsePingTimeMs(t *testing.T) {
	cases := []struct {
		out  string
		want float64
		ok   bool
	}{
		{"64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time=12.3 ms", 12.3, true},
		{"64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time<1 ms", 1, true},
		{"PING 1.1.1.1 (1.1.1.1) 56(84) bytes of data.\n", 0, false},
		{"time=0.421 ms", 0.421, true},
	}
	for _, tc := range cases {
		got, ok := network.ParsePingTimeMs([]byte(tc.out))
		if ok != tc.ok {
			t.Fatalf("%q: ok=%v want %v", tc.out, ok, tc.ok)
		}
		if ok && got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.out, got, tc.want)
		}
	}
}
