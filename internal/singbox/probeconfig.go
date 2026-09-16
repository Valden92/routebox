package singbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/subscription"
)

// Порты пробного инстанса (автовыбор узла): не должны совпадать с основным
// (clash API 47893, probe proxy 47894), чтобы перебор не рвал работающий туннель.
const (
	ProbeClashAPIAddr = "127.0.0.1:47895"
	ProbeProxyPort    = 47896
	ProbeProxyURL     = "http://127.0.0.1:47896"
)

// ProbeNodeTag — тег узла в конфиге пробного инстанса (selector их переключает).
func ProbeNodeTag(i int) string { return fmt.Sprintf("probe-%d", i) }

// WriteProbeConfig пишет лёгкий конфиг sing-box для автовыбора узла:
// mixed-inbound на 127.0.0.1 → selector со всеми узлами подписки.
// Без TUN и маршрутов — не влияет на сетевые настройки хоста.
// Фрагментация TLS — как в настройках личного VPN (проверяем узел в реальных условиях).
func WriteProbeConfig(path string, nodes []subscription.Node, st config.Settings) error {
	var outbounds []any
	var endpoints []any
	var tags []string
	for i, n := range nodes {
		tag := ProbeNodeTag(i)
		o, isEndpoint, err := BuildOutbound(n, tag, st.PersonalVPN.FragmentTLS())
		if err != nil {
			// Неподдерживаемый протокол/transport — пропускаем, автовыбор идёт по остальным.
			continue
		}
		if isEndpoint {
			endpoints = append(endpoints, o)
		} else {
			outbounds = append(outbounds, o)
		}
		tags = append(tags, tag)
	}
	outbounds = append(outbounds, map[string]any{
		"type":      "selector",
		"tag":       "proxy",
		"outbounds": append([]any{"direct"}, toAny(tags)...),
	})
	if len(tags) == 0 {
		return fmt.Errorf("в подписке нет узлов, поддерживаемых sing-box")
	}
	cfg := map[string]any{
		"log": map[string]any{"level": "warn"},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": ProbeClashAPIAddr,
			},
		},
		"dns": map[string]any{
			"servers": []map[string]any{
				dnsTLSServer("dns-direct", "1.1.1.1", "direct"),
			},
			"final": "dns-direct",
		},
		"inbounds": []map[string]any{
			{
				"type":        "mixed",
				"tag":         "probe-in",
				"listen":      "127.0.0.1",
				"listen_port": ProbeProxyPort,
			},
		},
		"outbounds": append([]any{buildDirectOutbound(st)}, outbounds...),
		"route": map[string]any{
			"rules": []map[string]any{
				{"inbound": []string{"probe-in"}, "outbound": "proxy"},
			},
			"final":                   "direct",
			"default_domain_resolver": "dns-direct",
		},
	}
	if len(endpoints) > 0 {
		cfg["endpoints"] = endpoints
	}
	return writeJSON(path, cfg)
}

func toAny(vals []string) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}
	return out
}

var clashAPIClient = &http.Client{Timeout: 5 * time.Second}

// WaitClashAPI ждёт готовности clash API пробного/основного инстанса.
func WaitClashAPI(ctx context.Context, controllerAddr string) error {
	u := "http://" + controllerAddr + "/version"
	var last error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		resp, err := clashAPIClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			last = fmt.Errorf("clash API HTTP %s", resp.Status)
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("clash API не ответил: %w (%v)", ctx.Err(), last)
			}
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// ClashSelectProxy переключает активный outbound селектора через clash API
// пробного инстанса (PUT /proxies/{selector}).
func ClashSelectProxy(ctx context.Context, controllerAddr, selector, name string) error {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		"http://"+controllerAddr+"/proxies/"+selector, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := clashAPIClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("clash API %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
