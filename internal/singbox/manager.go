package singbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/network"
	"github.com/Valden92/routebox/internal/routing"
	"github.com/Valden92/routebox/internal/subscription"
)

type Manager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	running   bool
	lastError string
	cfgPath   string
	binPath   string
	logPath   string
	waitDone  chan struct{}
}

const ClashAPIAddr = "127.0.0.1:47893"
const PersonalProbeProxyURL = "http://127.0.0.1:47894"

// TUN perf: system stack + ниже MTU под OpenVPN overhead; auto_redirect на Linux.
const (
	tunStack = "system"
	tunMTU   = 1400
)

func NewManager(binPath, cfgPath string) *Manager {
	logPath := filepath.Join(filepath.Dir(cfgPath), "sing-box.log")
	return &Manager{binPath: binPath, cfgPath: cfgPath, logPath: logPath}
}

func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.aliveLocked()
}

func (m *Manager) LastError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.aliveLocked() {
		return ""
	}
	if m.lastError == "" {
		return ""
	}
	return resolveErrorMessage(m.lastError, tailLogFatal(m.logPath))
}

func (m *Manager) aliveLocked() bool {
	if !m.running {
		return false
	}
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	if m.cmd.ProcessState != nil && m.cmd.ProcessState.Exited() {
		m.running = false
		return false
	}
	return true
}

func (m *Manager) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.aliveLocked() {
		return nil
	}
	bin := ResolveBin(m.binPath)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("sing-box not found: install via scripts/install-sing-box.sh")
	}
	if err := RequireTUNCapability(bin); err != nil {
		return err
	}
	if err := validateConfigIfChanged(bin, m.cfgPath); err != nil {
		m.lastError = err.Error()
		return err
	}
	// Убрать залипшие ip rule/table после аварийного stop (иначе: add rule … file exists).
	CleanupStaleTUNRules()
	// Не привязываем к HTTP-запросу: иначе процесс умирает после ответа API
	m.cmd = exec.Command(bin, "run", "-c", m.cfgPath)
	logFile, err := os.OpenFile(m.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		m.cmd.Stderr = os.Stderr
	} else {
		_, _ = logFile.WriteString(fmt.Sprintf("\n--- start %s ---\n", time.Now().Format(time.RFC3339)))
		m.cmd.Stderr = logFile
	}
	m.lastError = ""
	if err := m.cmd.Start(); err != nil {
		return err
	}
	m.running = true
	m.waitDone = make(chan struct{})
	go m.waitProcess(m.cmd)
	time.Sleep(300 * time.Millisecond)
	if !m.aliveLocked() {
		hint := resolveErrorMessage(m.lastError, tailLogFatal(m.logPath))
		m.lastError = hint
		RestoreAfterPersonalVPN()
		return fmt.Errorf("sing-box не запустился: %s", hint)
	}
	return nil
}

func (m *Manager) waitProcess(cmd *exec.Cmd) {
	defer func() {
		close(m.waitDone)
	}()
	waitErr := cmd.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	if waitErr != nil {
		m.lastError = waitErr.Error()
	} else if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
		m.lastError = fmt.Sprintf("sing-box exited: %s", cmd.ProcessState.String())
	}
	if hint := tailLogFatal(m.logPath); hint != "" {
		m.lastError = hint
	}
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	if !m.running || m.cmd == nil || m.cmd.Process == nil {
		m.mu.Unlock()
		return nil
	}
	proc := m.cmd.Process
	done := m.waitDone
	m.mu.Unlock()

	_ = proc.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = proc.Kill()
		<-done
	}
	// Дать ядру/sing-box снять ip rule после close TUN.
	time.Sleep(500 * time.Millisecond)
	CleanupStaleTUNRules()

	m.mu.Lock()
	m.running = false
	m.cmd = nil
	m.lastError = ""
	m.mu.Unlock()
	return nil
}

func WriteConfig(path string, node subscription.Node, st config.Settings, tunName string) error {
	return WriteRouterConfig(path, &node, st, tunName)
}

func WriteRouterConfig(path string, node *subscription.Node, st config.Settings, tunName string) error {
	if tunName == "" {
		tunName = "tun100"
	}
	personalAvailable := st.PersonalVPN.Enabled && node != nil
	var outbound map[string]any
	var endpoint map[string]any
	if personalAvailable {
		switch strings.ToLower(node.Protocol) {
		case "openvpn":
			ep, err := openvpnEndpoint("proxy", *node)
			if err != nil {
				return err
			}
			endpoint = ep
		default:
			o, err := uriToOutbound(*node)
			if err != nil {
				return err
			}
			outbound = o
		}
	}
	// DNS schema 1.12+: typed servers (не legacy address).
	// Не используем server "local" — иначе systemd-resolved → 127.0.0.1:53.
	dnsServers := []map[string]any{
		dnsTLSServer("dns-direct", "1.1.1.1", "direct"),
	}
	if personalAvailable {
		dnsServers = append(dnsServers, dnsTLSServer("dns-proxy", "1.1.1.1", "proxy"))
	}
	sysUp := systemVPNUp(st)
	sysIface := systemTunIface(st)
	dnsRules := buildDNSRules(st, personalAvailable, sysUp, sysIface, &dnsServers)
	routeExclude := routeExcludeAddresses(sysIface, sysUp, systemVPNEndpointCIDRs(st))
	tunInbound := map[string]any{
		"type":                  "tun",
		"tag":                   "tun-in",
		"interface_name":        tunName,
		"address":               []string{"172.19.0.1/30"},
		"mtu":                   tunMTU,
		"auto_route":            true,
		"strict_route":          !sysUp,
		"route_exclude_address": routeExclude,
		"stack":                 tunStack,
		// 1.14 default dns_mode=hijack → systemd-resolved (polkit-пароли) + лишние ip rule.
		// DNS уже через route hijack-dns + секцию dns.
		"dns_mode":             "disabled",
		"iproute2_table_index": 20221,
		"iproute2_rule_index":  9210,
	}
	// Linux: nftables auto_redirect быстрее tproxy и лучше уживается с Docker.
	// strict_route при auto_redirect затягивает SO_BINDTODEVICE обратно в TUN —
	// ломает direct с bind_interface (петля). Поэтому strict_route выключаем.
	if runtime.GOOS == "linux" {
		tunInbound["auto_redirect"] = true
		tunInbound["strict_route"] = false
	}
	inbounds := []map[string]any{tunInbound}
	if personalAvailable {
		inbounds = append(inbounds, map[string]any{
			"type":        "mixed",
			"tag":         "personal-probe-in",
			"listen":      "127.0.0.1",
			"listen_port": 47894,
		})
	}
	// Системный VPN: без перехвата tun0.
	if sysUp {
		tunInbound["route_address"] = []string{"0.0.0.0/1", "128.0.0.0/1"}
		tunInbound["exclude_interface"] = uniqueStrings(sysIface, "tun0", "tun1")
	}
	cfg := map[string]any{
		"log": map[string]any{"level": "warn"},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": ClashAPIAddr,
			},
		},
		"dns": map[string]any{
			"servers":        dnsServers,
			"strategy":       "ipv4_only",
			"cache_capacity": 4096,
			"rules":          dnsRules,
			"final":          "dns-direct",
		},
		"inbounds":  inbounds,
		"outbounds": buildOutbounds(outbound, st),
		"route":     buildRoute(st, node, sysUp, sysIface),
	}
	if endpoint != nil {
		cfg["endpoints"] = []any{endpoint}
	}
	return writeJSON(path, cfg)
}

func dnsTLSServer(tag, server, detour string) map[string]any {
	return map[string]any{
		"type":   "tls",
		"tag":    tag,
		"server": server,
		"detour": detour,
	}
}

func dnsUDPServer(tag, server, detour string) map[string]any {
	return map[string]any{
		"type":   "udp",
		"tag":    tag,
		"server": server,
		"detour": detour,
	}
}

func uniqueStrings(vals ...string) []string {
	seen := make(map[string]struct{}, len(vals))
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func buildDirectOutbound(st config.Settings) map[string]any {
	o := map[string]any{"type": "direct", "tag": "direct"}
	iface := st.MainInterface
	if iface == "" {
		iface = network.DefaultMainIface()
	}
	if iface != "" {
		o["bind_interface"] = iface
	}
	return o
}

func buildOutbounds(proxy map[string]any, st config.Settings) []any {
	out := []any{
		buildDirectOutbound(st),
	}
	if proxy != nil {
		out = append(out, proxy)
	}
	if systemVPNUp(st) {
		iface := systemTunIface(st)
		out = append(out, map[string]any{"type": "direct", "tag": "work", "bind_interface": iface})
	}
	return out
}

func proxyBypassRules(node subscription.Node) []map[string]any {
	if node.Host == "" {
		return nil
	}
	if ip := net.ParseIP(node.Host); ip != nil {
		return []map[string]any{{
			"ip_cidr":  []string{ip.String() + "/32"},
			"outbound": "direct",
		}}
	}
	return []map[string]any{{
		"domain":   []string{node.Host},
		"outbound": "direct",
	}}
}

func buildDNSRules(st config.Settings, personalAvailable, sysUp bool, sysIface string, dnsServers *[]map[string]any) []map[string]any {
	var rules []map[string]any
	if personalAvailable {
		for _, r := range routing.CollapseDomainRules(enabledDomainRules(st.DomainRules)) {
			if r.Path != config.RoutePersonal {
				continue
			}
			rules = append(rules, domainMatchRule(r.Pattern, map[string]any{"server": "dns-proxy"}))
		}
	}
	if sysUp {
		if corpDNS := systemVPNDNS(sysIface); corpDNS != "" {
			*dnsServers = append(*dnsServers, dnsUDPServer("dns-work", corpDNS, "work"))
		}
		rules = append(rules, map[string]any{
			"domain_suffix": routing.DefaultCorpSuffixes(),
			"server":        "dns-work",
		})
	}
	rules = append(rules, map[string]any{
		"domain_suffix": []string{".ru", ".рф"},
		"server":        "dns-direct",
	})
	return rules
}

func enabledDomainRules(rules []config.DomainRule) []config.DomainRule {
	out := make([]config.DomainRule, 0, len(rules))
	for _, r := range rules {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out
}

func domainMatchRule(pattern string, base map[string]any) map[string]any {
	rule := make(map[string]any, len(base)+1)
	for k, v := range base {
		rule[k] = v
	}
	if stringsHasPrefix(pattern, "*.") {
		rule["domain_suffix"] = []string{stringsTrimPrefix(pattern, "*.")}
	} else {
		rule["domain"] = []string{pattern}
	}
	return rule
}

func buildRoute(st config.Settings, node *subscription.Node, sysUp bool, sysIface string) map[string]any {
	personalAvailable := st.PersonalVPN.Enabled && node != nil
	rules := []map[string]any{
		{"action": "sniff", "timeout": "1s"},
		{"protocol": "dns", "action": "hijack-dns"},
		{"ip_cidr": []string{"127.0.0.0/8"}, "outbound": "direct"},
	}
	if personalAvailable {
		rules = append(rules, map[string]any{
			"inbound":  []string{"personal-probe-in"},
			"outbound": "proxy",
		})
	}
	if node != nil {
		rules = append(rules, proxyBypassRules(*node)...)
	}
	if sysUp {
		for _, cidr := range WorkRoutesFromOS(sysIface) {
			rules = append(rules, map[string]any{
				"ip_cidr":  []string{cidr},
				"outbound": "work",
			})
		}
	}
	for _, r := range routing.CollapseDomainRules(enabledDomainRules(st.DomainRules)) {
		tag, ok := pathTagIfAvailable(r.Path, personalAvailable, sysUp)
		if !ok {
			continue
		}
		rules = append(rules, domainMatchRule(r.Pattern, map[string]any{"outbound": tag}))
	}
	for _, a := range st.AppRules {
		if !a.Enabled || a.ProcessName == "" {
			continue
		}
		tag, ok := pathTagIfAvailable(a.Path, personalAvailable, sysUp)
		if !ok {
			continue
		}
		rules = append(rules, map[string]any{
			"process_name": []string{a.ProcessName},
			"outbound":     tag,
		})
	}
	rules = append(rules,
		map[string]any{"ip_is_private": true, "outbound": "direct"},
	)
	return map[string]any{
		"rules":                   rules,
		"final":                   "direct",
		"auto_detect_interface":   true,
		"default_domain_resolver": "dns-direct",
	}
}

func pathTagIfAvailable(p config.RoutePath, personalAvailable, workAvailable bool) (string, bool) {
	switch p {
	case config.RouteWork:
		return "work", workAvailable
	case config.RoutePersonal:
		return "proxy", personalAvailable
	default:
		return "direct", true
	}
}

// PathTagIfAvailable — outbound tag для правила, если путь доступен.
func PathTagIfAvailable(p config.RoutePath, personalAvailable, workAvailable bool) (string, bool) {
	return pathTagIfAvailable(p, personalAvailable, workAvailable)
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func tailLogFatal(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	lastStart := -1
	for i, line := range lines {
		if strings.Contains(line, "--- start ") {
			lastStart = i
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if lastStart >= 0 && i <= lastStart {
			break
		}
		line := strings.TrimSpace(stripANSI(lines[i]))
		if strings.Contains(line, "FATAL") || strings.Contains(line, "ERROR") {
			return parseSingBoxLogLine(line)
		}
	}
	return ""
}

func parseSingBoxLogLine(line string) string {
	line = stripANSI(line)
	for _, tag := range []string{"FATAL", "ERROR"} {
		if idx := strings.Index(line, tag); idx >= 0 {
			rest := line[idx+len(tag):]
			if j := strings.Index(rest, "[0000] "); j >= 0 {
				return strings.TrimSpace(rest[j+8:])
			}
			if j := strings.LastIndex(rest, "] "); j >= 0 {
				return strings.TrimSpace(rest[j+2:])
			}
		}
	}
	return strings.TrimSpace(line)
}

// ParseSingBoxLogLine вытаскивает текст FATAL/ERROR из строки лога sing-box.
func ParseSingBoxLogLine(line string) string {
	return parseSingBoxLogLine(line)
}

func isGenericExit(msg string) bool {
	return strings.HasPrefix(msg, "exit status ")
}

func resolveErrorMessage(raw, logHint string) string {
	msg := strings.TrimSpace(logHint)
	if msg == "" {
		msg = strings.TrimSpace(raw)
	}
	if msg == "" {
		return ""
	}
	if isGenericExit(msg) && logHint != "" {
		msg = logHint
	}
	if isGenericExit(msg) {
		return "sing-box не запустился — нажмите «Включить» снова; если сеть пропала: make recover-network"
	}
	if strings.Contains(msg, "operation not permitted") {
		return "Нет прав на TUN — выполните: make sync, затем снова «Включить»"
	}
	return msg
}

// ResolveErrorMessage нормализует ошибку старта sing-box для UI.
func ResolveErrorMessage(raw, logHint string) string {
	return resolveErrorMessage(raw, logHint)
}

func validateConfigIfChanged(bin, cfgPath string) error {
	bin = ResolveBin(bin)
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	h := sha256.Sum256(cfg)
	key := filepath.Join(filepath.Dir(cfgPath), ".sing-box-config.sha256")
	prev, _ := os.ReadFile(key)
	if string(prev) == hex.EncodeToString(h[:]) {
		return nil
	}
	if err := ValidateConfig(bin, cfgPath); err != nil {
		return err
	}
	_ = os.WriteFile(key, []byte(hex.EncodeToString(h[:])), 0o600)
	return nil
}

func ValidateConfig(bin, cfgPath string) error {
	bin = ResolveBin(bin)
	out, err := exec.Command(bin, "check", "-c", cfgPath).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("конфиг sing-box: %s", msg)
	}
	return nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func stringsTrimPrefix(s, prefix string) string {
	if stringsHasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}
