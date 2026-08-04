package nm

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SystemVPNStatus — только наблюдение; подключение/отключение — в NetworkManager (MFA).
// State: disconnected | connecting | connected
type SystemVPNStatus struct {
	Connected    bool      `json:"connected"`
	State        string    `json:"state"`
	ConnectionID string    `json:"connectionId"`
	VpnType      string    `json:"vpnType,omitempty"`
	NMState      string    `json:"nmState,omitempty"`
	Interface    string    `json:"interface,omitempty"`
	IPv4         string    `json:"ipv4,omitempty"`
	Gateway      string    `json:"gateway,omitempty"`
	RouteCount   int       `json:"routeCount,omitempty"`
	Routes       []string  `json:"-"` // только для подсказок маршрутов в API
	LastCheck    time.Time `json:"lastCheck"`
	Error        string    `json:"error,omitempty"`
	Message      string    `json:"message,omitempty"`
}

func Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "nmcli", args...)
	cmd.Env = sessionEnv()
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func Status(ctx context.Context, connectionID string) SystemVPNStatus {
	st := SystemVPNStatus{
		ConnectionID: connectionID,
		State:        "disconnected",
		LastCheck:    time.Now(),
	}
	active, err := Run(ctx, "-t", "-f", "NAME,TYPE,DEVICE,STATE", "connection", "show", "--active")
	if err != nil {
		st.Error = HumanizeErr(err)
		return st
	}
	for line := range strings.SplitSeq(active, "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 4 {
			continue
		}
		name, typ, dev, nmState := parts[0], parts[1], parts[2], parts[3]
		if name != connectionID || typ != "vpn" {
			continue
		}
		st.NMState = nmState
		st.Interface = dev
		switch classifyNMState(nmState) {
		case "connected":
			st.State = "connected"
			st.Connected = true
		case "connecting":
			st.State = "connecting"
			st.Connected = false
		default:
			st.State = "connecting"
			st.Connected = false
		}
		break
	}

	// MFA / activating: VPN ещё не в --active, но устройство Wi‑Fi уже «connecting» к профилю
	if st.State == "disconnected" {
		if dev, nmState, ok := deviceForConnection(ctx, connectionID); ok {
			st.NMState = nmState
			st.Interface = dev
			switch classifyNMState(nmState) {
			case "connected":
				st.State = "connected"
				st.Connected = true
			case "connecting":
				st.State = "connecting"
			}
		}
	}
	if st.State == "disconnected" {
		if tun, nmState, ok := vpnDeviceForConnection(ctx, connectionID); ok {
			st.Interface = tun
			st.NMState = nmState
			switch classifyNMState(nmState) {
			case "connected":
				st.State = "connected"
				st.Connected = true
			case "connecting":
				st.State = "connecting"
			}
		}
	}

	if st.State == "connected" || st.State == "connecting" {
		if tun, _, ok := vpnDeviceForConnection(ctx, connectionID); ok {
			st.Interface = tun
		} else if st.Interface == "" || !strings.HasPrefix(st.Interface, "tun") {
			if tun, ok := firstActiveTun(ctx); ok {
				st.Interface = tun
			}
		}
	}
	if st.Connected && st.Interface != "" {
		st.IPv4 = ifaceIPv4(st.Interface)
		st.Routes = tunRoutes(st.Interface)
		st.RouteCount = len(st.Routes)
		st.Gateway = tunDefaultGateway(st.Interface)
	}
	if vt, err := Run(ctx, "-g", "vpn.service-type", "connection", "show", connectionID); err == nil && vt != "" {
		st.VpnType = humanVpnType(vt)
	}
	return st
}

func humanVpnType(serviceType string) string {
	s := strings.TrimSpace(serviceType)
	if s == "" {
		return ""
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	switch strings.ToLower(s) {
	case "openvpn":
		return "OpenVPN"
	case "wireguard":
		return "WireGuard"
	case "openconnect":
		return "OpenConnect"
	case "vpnc":
		return "Cisco VPNC"
	case "pptp":
		return "PPTP"
	case "l2tp":
		return "L2TP"
	default:
		if len(s) == 0 {
			return serviceType
		}
		return strings.ToUpper(s[:1]) + s[1:]
	}
}

func tunDefaultGateway(iface string) string {
	out, err := exec.Command("ip", "-4", "route", "show", "dev", iface).CombinedOutput()
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "default" && fields[1] == "via" {
			return fields[2]
		}
	}
	return ""
}

func classifyNMState(nmState string) string {
	s := strings.ToLower(strings.TrimSpace(nmState))
	if code := parseNMStateCode(s); code >= 0 {
		switch {
		case code == 100:
			return "connected"
		case code >= 40 && code < 100:
			return "connecting"
		case code == 110:
			return "connecting"
		default:
			return "disconnected"
		}
	}
	switch s {
	case "activated":
		return "connected"
	case "activating", "deactivating", "ip-config", "ip-check", "secondaries", "checking":
		return "connecting"
	}
	if strings.Contains(s, "need") && strings.Contains(s, "auth") {
		return "connecting"
	}
	if strings.Contains(s, "аутентиф") || strings.Contains(s, "подключ") {
		if strings.Contains(s, "подключен") || strings.Contains(s, "актив") {
			return "connected"
		}
		return "connecting"
	}
	return "disconnected"
}

func parseNMStateCode(s string) int {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(fields[0], "%d", &n); err == nil {
		return n
	}
	return -1
}

func deviceForConnection(ctx context.Context, connectionID string) (device, nmState string, ok bool) {
	out, err := Run(ctx, "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device", "status")
	if err != nil {
		return "", "", false
	}
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 4 {
			continue
		}
		dev, state, conn := parts[0], parts[2], parts[3]
		if conn != connectionID {
			continue
		}
		return dev, state, true
	}
	return "", "", false
}

func vpnDeviceForConnection(ctx context.Context, connectionID string) (iface, nmState string, ok bool) {
	for _, cand := range []string{"tun0", "tun1"} {
		conn, err := Run(ctx, "-g", "GENERAL.CONNECTION", "device", "show", cand)
		if err != nil {
			continue
		}
		conn = strings.TrimSpace(conn)
		if conn != connectionID {
			continue
		}
		state, err := Run(ctx, "-g", "GENERAL.STATE", "device", "show", cand)
		if err != nil {
			continue
		}
		state = strings.TrimSpace(state)
		if state == "" || state == "unavailable" || state == "disconnected" {
			continue
		}
		return cand, state, true
	}
	return "", "", false
}

func firstActiveTun(ctx context.Context) (string, bool) {
	for _, cand := range []string{"tun0", "tun1"} {
		state, err := Run(ctx, "-g", "GENERAL.STATE", "device", "show", cand)
		if err != nil {
			continue
		}
		state = strings.TrimSpace(state)
		switch classifyNMState(state) {
		case "connected", "connecting":
			return cand, true
		}
	}
	return "", false
}

func ifaceIPv4(name string) string {
	out, err := exec.Command("ip", "-4", "-br", "addr", "show", name).CombinedOutput()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) >= 3 {
		return fields[2]
	}
	return ""
}

func tunRoutes(iface string) []string {
	out, err := exec.Command("ip", "route", "show", "dev", iface).CombinedOutput()
	if err != nil {
		return nil
	}
	var routes []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			routes = append(routes, line)
		}
	}
	return routes
}

func Connect(ctx context.Context, connectionID string) error {
	cur := Status(ctx, connectionID)
	if cur.Connected {
		return nil
	}
	if cur.State == "connecting" {
		return nil
	}
	// Не --wait: MFA идёт через системный агент NM в сессии пользователя
	_, err := Run(ctx, "connection", "up", connectionID)
	if err == nil {
		return nil
	}
	if IsBenignConnectErr(err) {
		return nil
	}
	return err
}

func Disconnect(ctx context.Context, connectionID string) error {
	var lastErr error
	// Сначала рвём привязку устройства (важно при зависшем MFA / activating)
	if dev, _, ok := deviceForConnection(ctx, connectionID); ok && dev != "" {
		if _, err := Run(ctx, "device", "disconnect", dev); err != nil && !IsBenignDisconnectErr(err) {
			lastErr = err
		}
	}
	if tun, _, ok := vpnDeviceForConnection(ctx, connectionID); ok && tun != "" {
		if _, err := Run(ctx, "device", "disconnect", tun); err != nil && !IsBenignDisconnectErr(err) {
			lastErr = err
		}
	}
	if _, err := Run(ctx, "connection", "down", connectionID); err != nil {
		if IsBenignDisconnectErr(err) {
			return lastErr
		}
		return err
	}
	return lastErr
}

func IsBenignDisconnectErr(err error) bool {
	code := nmcliExitCode(err)
	// 4: не активен / отмена; 5: деактивация не нужна; 6: уже отключается
	return code == 4 || code == 5 || code == 6
}

func IsBenignConnectErr(err error) bool {
	code := nmcliExitCode(err)
	// 4: уже подключается или уже активен (зависит от фазы)
	return code == 4
}

func nmcliExitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func HumanizeErr(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	code := nmcliExitCode(err)
	switch code {
	case 4:
		return "VPN не активен или подключение отменено"
	case 5:
		return "VPN уже отключён"
	case 6:
		return "VPN отключается"
	case 3:
		return "Таймаут NetworkManager"
	case 1:
		if strings.Contains(msg, "exit status") {
			return "Ошибка NetworkManager"
		}
	}
	if strings.Contains(msg, "exit status") {
		return fmt.Sprintf("NetworkManager: %s", msg)
	}
	return msg
}
