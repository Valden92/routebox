//go:build linux

package apps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func Scan(ctx context.Context) ([]NetworkApp, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ss", "-H", "-O", "-n", "-t", "-u", "-a", "-p")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("чтение сетевых сокетов (ss): %w", err)
	}
	return ParseSocketSnapshot(string(out), procInfo), nil
}

func procInfo(pid int) (name, path string) {
	exe, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err == nil {
		path = exe
		name = filepath.Base(exe)
		return name, path
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err == nil {
		name = strings.TrimSpace(string(cmdline))
	}
	return name, path
}
