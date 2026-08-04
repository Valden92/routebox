package nm

import (
	"fmt"
	"os"
	"strings"
)

// sessionEnv дополняет окружение для nmcli: без сессии D-Bus MFA/секреты не доходят до агента NM.
func sessionEnv() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(kv string) {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, kv)
	}
	for _, e := range os.Environ() {
		add(e)
	}
	uid := os.Getuid()
	runtimeDir := fmt.Sprintf("/run/user/%d", uid)
	add("XDG_RUNTIME_DIR=" + runtimeDir)
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		add("DBUS_SESSION_BUS_ADDRESS=unix:path=" + runtimeDir + "/bus")
	}
	if os.Getenv("PATH") == "" {
		add("PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	if h := os.Getenv("HOME"); h == "" {
		if home, err := os.UserHomeDir(); err == nil {
			add("HOME=" + home)
		}
	}
	// systemd user session (если демон запущен без полного окружения рабочего стола)
	if os.Getenv("XDG_SESSION_ID") == "" {
		if sid := systemdSessionID(uid); sid != "" {
			add("XDG_SESSION_ID=" + sid)
		}
	}
	return out
}

func systemdSessionID(uid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/run/systemd/users/%d", uid))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if after, ok := strings.CutPrefix(line, "SESSION-ID="); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}
