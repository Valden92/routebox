package singbox

import (
	"testing"
)

func TestUniqueStrings(t *testing.T) {
	got := uniqueStrings("tun0", "", "tun0", "tun1")
	if len(got) != 2 || got[0] != "tun0" || got[1] != "tun1" {
		t.Fatalf("%v", got)
	}
}

func TestRulePriority(t *testing.T) {
	if rulePriority("9210:\tfrom all lookup 20221") != 9210 {
		t.Fatal(rulePriority("9210:\tfrom all lookup 20221"))
	}
	if rulePriority("bad") != 0 {
		t.Fatal("want 0")
	}
}
