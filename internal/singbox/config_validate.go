package singbox

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Valden92/routebox/internal/config"
)

// ConfigMode — как sing-box.json был собран.
type ConfigMode string

const (
	ConfigModeCoexist ConfigMode = "coexist"
	ConfigModeFull    ConfigMode = "full"
)

func ConfigModeFor(st config.Settings) ConfigMode {
	if systemVPNUp(st) {
		return ConfigModeCoexist
	}
	return ConfigModeFull
}

// ValidateWrittenConfig проверяет sing-box.json после WriteConfig.
func ValidateWrittenConfig(path string, st config.Settings) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return ValidateConfigBytes(b, systemVPNUp(st))
}

// ValidateConfigBytes проверяет содержимое sing-box.json при известном sysUp (удобно для тестов).
func ValidateConfigBytes(b []byte, sysUp bool) error {
	var raw struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("sing-box.json: %w", err)
	}
	body := string(b)
	hasHijack := strings.Contains(body, "hijack-dns")
	hasCoexist := strings.Contains(body, "exclude_interface") &&
		strings.Contains(body, "route_address")

	if sysUp {
		if !hasHijack {
			return fmt.Errorf(
				"системный VPN активен, но DNS hijack не включён — переподключите личный VPN",
			)
		}
		if !hasCoexist {
			return fmt.Errorf(
				"системный VPN активен, но режим coexist не применился — пересоберите: make stop && make build && make dev",
			)
		}
		hasWork := false
		for _, o := range raw.Outbounds {
			if o.Tag == "work" {
				hasWork = true
				break
			}
		}
		if !hasWork {
			return fmt.Errorf("системный VPN активен, но outbound work отсутствует в sing-box.json")
		}
	}
	return nil
}

// ConfigFileMode читает фактический sing-box.json на диске.
func ConfigFileMode(path string) ConfigMode {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(b)
	if strings.Contains(s, "exclude_interface") && strings.Contains(s, "route_address") {
		return ConfigModeCoexist
	}
	if strings.Contains(s, "hijack-dns") {
		return ConfigModeFull
	}
	return ""
}
