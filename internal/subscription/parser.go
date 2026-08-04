package subscription

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
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
	_, ok := allowedSchemes[strings.ToLower(n.Protocol)]
	return ok
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
		fmt.Sscanf(p, "%d", &port)
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
