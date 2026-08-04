package network

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type LinkStatus struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	IPv4    string `json:"ipv4,omitempty"`
	IPv6    string `json:"ipv6,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

type InternetStatus struct {
	Up          bool        `json:"up"`
	Interface   string      `json:"interface"`
	PublicIP    string        `json:"publicIp,omitempty"`
	PublicIPv6  string        `json:"publicIpv6,omitempty"`
	DNS         []string    `json:"dns,omitempty"`
	LatencyMs   float64     `json:"latencyMs,omitempty"`
	Link        LinkStatus  `json:"link"`
	LastCheck   time.Time   `json:"lastCheck"`
	Error       string      `json:"error,omitempty"`
}

func DefaultMainIface() string {
	if v := os.Getenv("VPN_ROUTER_MAIN_IFACE"); v != "" {
		return v
	}
	return "wlp0s20f3"
}

func LinkByName(name string) (LinkStatus, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return LinkStatus{Name: name, State: "down"}, err
	}
	st := LinkStatus{Name: name}
	if iface.Flags&net.FlagUp != 0 {
		st.State = "up"
	} else {
		st.State = "down"
	}
	addrs, _ := iface.Addrs()
	for _, a := range addrs {
		ip, ok := a.(*net.IPNet)
		if !ok || ip.IP == nil {
			continue
		}
		if v4 := ip.IP.To4(); v4 != nil {
			st.IPv4 = fmt.Sprintf("%s/%d", v4, ones(ip))
		} else if ip.IP.To4() == nil {
			st.IPv6 = ip.String()
		}
	}
	return st, nil
}

func ones(ip *net.IPNet) int {
	ones, _ := ip.Mask.Size()
	return ones
}

func PublicIP(ctx context.Context, bindIface string) (v4, v6 string, err error) {
	// Через IP, без DNS: при личном VPN резолв иначе идёт в tun100 и даёт ложный «нет интернета»
	client := httpClient(bindIface, 4*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://1.1.1.1/cdn-cgi/trace", nil)
	if err != nil {
		return "", "", err
	}
	req.Host = "one.one.one.one"
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "ip="); ok {
			return strings.TrimSpace(after), "", nil
		}
	}
	return "", "", fmt.Errorf("не удалось прочитать IP из ответа проверки")
}

func CheckInternet(ctx context.Context, iface string) InternetStatus {
	st := InternetStatus{
		Interface: iface,
		LastCheck: time.Now(),
	}
	link, err := LinkByName(iface)
	st.Link = link
	if err != nil || link.State != "up" {
		st.Up = false
		if err != nil {
			st.Error = err.Error()
		} else {
			st.Error = "interface is down"
		}
		return st
	}
	st.DNS, _ = ResolveDNS()
	start := time.Now()
	v4, _, err := PublicIP(ctx, iface)
	st.LatencyMs = float64(time.Since(start).Milliseconds())
	if err != nil {
		st.Up = link.State == "up"
		if st.Up {
			st.Error = "Wi‑Fi поднят, внешняя проверка недоступна (часто из‑за VPN): " + err.Error()
		} else {
			st.Error = err.Error()
		}
		return st
	}
	st.Up = true
	st.PublicIP = v4
	return st
}

func ResolveDNS() ([]string, error) {
	b, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	var out []string
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver ") {
			out = append(out, strings.TrimPrefix(line, "nameserver "))
		}
	}
	return out, nil
}

func httpClient(bindIface string, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
	if bindIface != "" {
		dialer.Control = bindControl(bindIface)
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
	}
}
