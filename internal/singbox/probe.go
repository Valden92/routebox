package singbox

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Valden92/routebox/internal/network"
	"github.com/Valden92/routebox/internal/subscription"
)

// ProbeProxyReachable проверяет, что до VPN-сервера можно достучаться с хоста (до полного туннеля).
// Свой таймаут, не контекст HTTP-запроса — иначе «operation was canceled» при отмене r.Context().
func ProbeProxyReachable(node subscription.Node, mainIface string) error {
	if node.Host == "" || node.Port == 0 {
		return fmt.Errorf("некорректный узел подписки")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d := net.Dialer{Timeout: 10 * time.Second}
	if mainIface != "" {
		d.Control = network.BindControlExport(mainIface)
	}
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", node.Host, node.Port))
	if err != nil {
		return fmt.Errorf("VPN-сервер %s:%d недоступен: %w", node.Host, node.Port, err)
	}
	_ = conn.Close()
	return nil
}
