package subscription

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	RawURI   string `json:"rawUri"`
	SNI      string `json:"sni,omitempty"`

	// OpenVPN (.ovpn): полный профиль и inline PEM (без внешних путей).
	ConfigText   string   `json:"configText,omitempty"`
	Network      string   `json:"network,omitempty"` // udp|tcp
	AuthUserPass bool     `json:"authUserPass,omitempty"`
	Username     string   `json:"username,omitempty"`
	Password     string   `json:"password,omitempty"`
	CA           []string `json:"ca,omitempty"`
	Cert         []string `json:"cert,omitempty"`
	Key          []string `json:"key,omitempty"`
	TLSAuth      []string `json:"tlsAuth,omitempty"`
	TLSCrypt     []string `json:"tlsCrypt,omitempty"`
	KeyDirection string   `json:"keyDirection,omitempty"`
}

// EndpointLabel — краткая метка узла для UI (локальный .ovpn и т.п.).
func EndpointLabel(n Node) string {
	proto := strings.ToLower(strings.TrimSpace(n.Protocol))
	if proto == "" {
		proto = "node"
	}
	host := strings.TrimSpace(n.Host)
	if host == "" {
		return strings.ToUpper(proto)
	}
	label := fmt.Sprintf("%s · %s:%d", strings.ToUpper(proto), host, n.Port)
	if net := strings.ToUpper(strings.TrimSpace(n.Network)); net != "" {
		label += " · " + net
	}
	if n.AuthUserPass {
		label += " · login"
	}
	return label
}

// NormalizeSource приводит source из API к канону: url|text|uri|ovpn.
func NormalizeSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "url":
		return "url"
	case "uri":
		return "uri"
	case "ovpn":
		return "ovpn"
	case "text", "file":
		return "text"
	default:
		return ""
	}
}

// InferSource угадывает тип для старых подписок без поля source.
func InferSource(subURL string, nodes []Node) string {
	if IsRemoteURL(subURL) {
		return "url"
	}
	if len(nodes) == 0 {
		return "text"
	}
	allOvpn := true
	for _, n := range nodes {
		if !strings.EqualFold(n.Protocol, "openvpn") {
			allOvpn = false
			break
		}
	}
	if allOvpn {
		return "ovpn"
	}
	if len(nodes) == 1 {
		raw := strings.ToLower(strings.TrimSpace(nodes[0].RawURI))
		if strings.Contains(raw, "://") && !strings.HasPrefix(raw, "openvpn://") {
			return "uri"
		}
	}
	return "text"
}

type Cache struct {
	SubscriptionID string    `json:"subscriptionId"`
	Nodes          []Node    `json:"nodes"`
	FetchedAt      time.Time `json:"fetchedAt"`
}

var allowedSchemes = map[string]struct{}{
	"vless": {}, "vmess": {}, "trojan": {}, "ss": {}, "shadowsocks": {},
	"hysteria": {}, "hysteria2": {}, "hy2": {}, "tuic": {}, "wireguard": {}, "ssh": {},
}

func ParseBody(body []byte) ([]Node, error) {
	text := strings.TrimSpace(string(body))
	if looksLikeHTML(text) {
		return nil, fmt.Errorf("подписка вернула HTML (страница входа), а не список серверов — проверьте URL или User-Agent")
	}
	if nodes := parseLines(text); len(nodes) > 0 {
		return nodes, nil
	}
	if dec, err := base64.StdEncoding.DecodeString(text); err == nil {
		if nodes := parseLines(string(dec)); len(nodes) > 0 {
			return nodes, nil
		}
	}
	if dec, err := base64.RawStdEncoding.DecodeString(text); err == nil {
		if nodes := parseLines(string(dec)); len(nodes) > 0 {
			return nodes, nil
		}
	}
	return nil, fmt.Errorf("unsupported subscription format")
}

func looksLikeHTML(text string) bool {
	low := strings.ToLower(text)
	if len(low) > 512 {
		low = low[:512]
	}
	return strings.HasPrefix(low, "<!doctype") ||
		strings.HasPrefix(low, "<html") ||
		strings.Contains(low, "<meta") && strings.Contains(low, "<script")
}

func isSubscriptionLine(line string) bool {
	low := strings.ToLower(line)
	for scheme := range allowedSchemes {
		if strings.HasPrefix(low, scheme+"://") {
			return true
		}
	}
	return false
}

func ValidNode(n Node) bool {
	if n.Host == "" || n.Port <= 0 {
		return false
	}
	proto := strings.ToLower(n.Protocol)
	if proto == "openvpn" {
		return len(n.CA) > 0 && strings.TrimSpace(n.ConfigText) != ""
	}
	_, ok := allowedSchemes[proto]
	return ok
}

func FindNodeByID(nodes []Node, id string) (Node, bool) {
	if id == "" {
		return Node{}, false
	}
	for _, n := range nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// FindNodeByEndpoint ищет тот же узел после смены raw URI (uuid/параметры),
// когда host+port+protocol сохранились.
func FindNodeByEndpoint(nodes []Node, host string, port int, protocol string) (Node, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if host == "" || port <= 0 {
		return Node{}, false
	}
	for _, n := range nodes {
		if strings.EqualFold(n.Host, host) && n.Port == port && strings.EqualFold(n.Protocol, protocol) {
			return n, true
		}
	}
	return Node{}, false
}

func FilterValidNodes(nodes []Node) []Node {
	var out []Node
	for _, n := range nodes {
		if ValidNode(n) {
			out = append(out, n)
		}
	}
	return out
}

func parseLines(text string) []Node {
	var nodes []Node
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !isSubscriptionLine(line) {
			continue
		}
		n, err := parseURI(line)
		if err != nil || !ValidNode(n) {
			continue
		}
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})
	return nodes
}

func parseURI(raw string) (Node, error) {
	scheme := strings.SplitN(raw, "://", 2)
	if len(scheme) != 2 {
		return Node{}, fmt.Errorf("bad uri")
	}
	proto := strings.ToLower(scheme[0])
	if _, ok := allowedSchemes[proto]; !ok {
		return Node{}, fmt.Errorf("unsupported scheme")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Node{}, err
	}
	host := u.Hostname()
	port := 0
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	name := u.Fragment
	if name == "" {
		name = host
	}
	name, _ = url.QueryUnescape(name)
	sni := u.Query().Get("sni")
	return Node{
		ID:       uuid.NewSHA1(uuid.NameSpaceURL, []byte(raw)).String(),
		Name:     name,
		Protocol: proto,
		Host:     host,
		Port:     port,
		RawURI:   raw,
		SNI:      sni,
	}, nil
}
