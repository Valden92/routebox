package singbox

import (
	"context"
	"fmt"
	"time"

	"github.com/Valden92/routebox/internal/ping"
	"github.com/Valden92/routebox/internal/subscription"
)

// ProbeProxyReachable проверяет, что до VPN-сервера можно достучаться с хоста (до полного туннеля).
// Свой таймаут, не контекст HTTP-запроса — иначе «operation was canceled» при отмене r.Context().
// OpenVPN/UDP — ICMP до host (порт не слушает TCP); остальные протоколы — TCP к host:port.
func ProbeProxyReachable(node subscription.Node, mainIface string) error {
	if node.Host == "" || node.Port == 0 {
		return fmt.Errorf("некорректный узел подписки")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res := ping.TCPBatch(ctx, mainIface, []subscription.Node{node}, 1)
	if len(res) == 0 {
		return fmt.Errorf("VPN-сервер %s:%d недоступен", node.Host, node.Port)
	}
	if !res[0].OK {
		msg := res[0].Error
		if msg == "" {
			msg = "недоступен"
		}
		return fmt.Errorf("VPN-сервер %s:%d недоступен: %s", node.Host, node.Port, msg)
	}
	return nil
}
