package singbox

import (
	"os"
	"os/exec"
	"path/filepath"
)

// ResolveBin — всегда предпочитаем ~/.local/bin/sing-box (туда ставится setcap).
func ResolveBin(configured string) string {
	if configured != "" {
		if filepath.IsAbs(configured) {
			return configured
		}
		if abs, err := exec.LookPath(configured); err == nil {
			return abs
		}
		return configured
	}
	if home, err := os.UserHomeDir(); err == nil {
		local := filepath.Join(home, ".local", "bin", "sing-box")
		if st, err := os.Stat(local); err == nil && !st.IsDir() {
			return local
		}
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p
	}
	return "sing-box"
}
