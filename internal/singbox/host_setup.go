package singbox

// HostReady — setcap на sing-box и drop-in NetworkManager (после make sync).
func HostReady(bin string) bool {
	return NetworkManagerIgnoresTUN() && HasTUNCapability(bin)
}
