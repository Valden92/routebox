// Command vpn-router-netclean снимает залипшие ip rule/table sing-box (личный VPN).
// Ставится в ~/.local/bin с setcap cap_net_admin (make sync).
package main

import (
	"fmt"
	"os"

	"github.com/Valden92/routebox/internal/singbox"
)

func main() {
	singbox.CleanupStaleTUNRules()
	fmt.Fprintln(os.Stderr, "vpn-router-netclean: ok")
}
