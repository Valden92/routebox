package subscription

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// ParseOvpn разбирает .ovpn с встроенными сертификатами (MVP).
// Внешние пути к ca/cert/key/tls-* отклоняются.
func ParseOvpn(content string, name string) (Node, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		return Node{}, fmt.Errorf("пустой .ovpn")
	}
	low := strings.ToLower(text)
	if strings.Contains(low, "static-challenge") || strings.Contains(low, "auth-retry interact") {
		return Node{}, fmt.Errorf("интерактивный MFA/challenge OpenVPN не поддерживается")
	}

	opts, blocks, err := parseOvpnDocument(text)
	if err != nil {
		return Node{}, err
	}
	if pathErr := rejectExternalPaths(opts); pathErr != nil {
		return Node{}, pathErr
	}

	host, port, network, remErr := firstRemote(opts)
	if remErr != nil {
		return Node{}, remErr
	}
	if port == 0 {
		if p, ok := opts["port"]; ok && len(p) > 0 {
			port, _ = strconv.Atoi(p[0])
		}
	}
	if port == 0 {
		port = 1194
	}
	if network == "" {
		if p, ok := opts["proto"]; ok && len(p) > 0 {
			network = normalizeProto(p[0])
		}
	}
	if network == "" {
		network = "udp"
	}

	ca := blockLines(blocks, "ca")
	if len(ca) == 0 {
		return Node{}, fmt.Errorf("нужен встроенный <ca>…</ca> в .ovpn")
	}
	cert := blockLines(blocks, "cert")
	key := blockLines(blocks, "key")
	if (len(cert) == 0) != (len(key) == 0) {
		return Node{}, fmt.Errorf("client cert и key должны быть оба встроены или оба отсутствовать")
	}

	_, authUserPass := opts["auth-user-pass"]
	display := strings.TrimSpace(name)
	if display == "" {
		display = host
	}
	idSeed := "openvpn://" + host + ":" + strconv.Itoa(port) + "/" + network + "\n" + text
	return Node{
		ID:           uuid.NewSHA1(uuid.NameSpaceURL, []byte(idSeed)).String(),
		Name:         display,
		Protocol:     "openvpn",
		Host:         host,
		Port:         port,
		RawURI:       "openvpn://local",
		ConfigText:   text,
		Network:      network,
		AuthUserPass: authUserPass,
		CA:           ca,
		Cert:         cert,
		Key:          key,
		TLSAuth:      blockLines(blocks, "tls-auth"),
		TLSCrypt:     blockLines(blocks, "tls-crypt"),
		KeyDirection: firstOpt(opts, "key-direction"),
	}, nil
}

func firstOpt(opts map[string][]string, key string) string {
	if v, ok := opts[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

func blockLines(blocks map[string][]string, name string) []string {
	if v, ok := blocks[name]; ok {
		return v
	}
	return nil
}

func normalizeProto(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	switch p {
	case "tcp", "tcp-client":
		return "tcp"
	case "udp", "udp4", "udp6":
		return "udp"
	default:
		return p
	}
}

func firstRemote(opts map[string][]string) (host string, port int, network string, err error) {
	remotes, ok := opts["remote"]
	if !ok || len(remotes) == 0 {
		return "", 0, "", fmt.Errorf("в .ovpn нет remote")
	}
	parts := strings.Fields(remotes[0])
	if len(parts) == 0 {
		return "", 0, "", fmt.Errorf("пустой remote")
	}
	host = parts[0]
	if len(parts) > 1 {
		port, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		network = normalizeProto(parts[2])
	}
	return host, port, network, nil
}

func rejectExternalPaths(opts map[string][]string) error {
	pathKeys := []string{"ca", "cert", "key", "tls-auth", "tls-crypt", "pkcs12", "dh", "extra-certs"}
	for _, k := range pathKeys {
		vals, ok := opts[k]
		if !ok || len(vals) == 0 {
			continue
		}
		// Директива с аргументом-путём (не inline-block).
		arg := strings.TrimSpace(vals[0])
		if arg != "" && !strings.HasPrefix(arg, "<") {
			return fmt.Errorf("нужен .ovpn с встроенными сертификатами (внешний путь %s не поддерживается)", k)
		}
	}
	if vals, ok := opts["auth-user-pass"]; ok && len(vals) > 0 {
		arg := strings.TrimSpace(vals[0])
		if arg != "" {
			return fmt.Errorf("auth-user-pass с файлом не поддерживается — укажите логин/пароль в приложении")
		}
	}
	return nil
}

func parseOvpnDocument(text string) (opts map[string][]string, blocks map[string][]string, err error) {
	opts = map[string][]string{}
	blocks = map[string][]string{}
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">") && !strings.HasPrefix(line, "</") {
			tag := strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">")
			tag = strings.Fields(tag)[0]
			var body []string
			i++
			closed := false
			for ; i < len(lines); i++ {
				l := strings.TrimSpace(lines[i])
				if strings.EqualFold(l, "</"+tag+">") {
					closed = true
					break
				}
				body = append(body, lines[i])
			}
			if !closed {
				return nil, nil, fmt.Errorf("незакрытый блок <%s>", tag)
			}
			blocks[strings.ToLower(tag)] = trimPEMLines(body)
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key := strings.ToLower(fields[0])
		arg := ""
		if len(fields) > 1 {
			arg = strings.Join(fields[1:], " ")
		}
		opts[key] = append(opts[key], arg)
	}
	return opts, blocks, nil
}

func trimPEMLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, strings.TrimRight(l, "\r"))
	}
	return out
}
