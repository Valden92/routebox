package singbox

import (
	"os"
	"strings"
)

const nmDropIn = "/etc/NetworkManager/conf.d/99-vpn-router.conf"

// NetworkManagerIgnoresTUN — NM не должен «настраивать» tun100 (иначе ломается MFA на tun0).
func NetworkManagerIgnoresTUN() bool {
	b, err := os.ReadFile(nmDropIn)
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "interface-name:tun100") &&
		strings.Contains(s, "unmanaged-devices") &&
		!strings.Contains(s, "type:tun") &&
		!strings.Contains(s, "interface-name:tun0")
}
