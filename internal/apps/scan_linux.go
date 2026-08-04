//go:build linux

package apps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type NetworkApp struct {
	PID         int    `json:"pid"`
	ProcessName string `json:"processName"`
	ExecPath    string `json:"execPath,omitempty"`
	Connections int    `json:"connections"`
}

func Scan(ctx context.Context) ([]NetworkApp, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ss", "-H", "-tnp").CombinedOutput()
	if err != nil {
		return nil, err
	}
	byPID := map[int]*NetworkApp{}
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid := extractPID(line)
		if pid <= 0 {
			continue
		}
		if a, ok := byPID[pid]; ok {
			a.Connections++
			continue
		}
		name, path := procInfo(pid)
		byPID[pid] = &NetworkApp{
			PID:         pid,
			ProcessName: name,
			ExecPath:    path,
			Connections: 1,
		}
	}
	var list []NetworkApp
	for _, a := range byPID {
		list = append(list, *a)
	}
	return list, nil
}

func extractPID(line string) int {
	i := strings.Index(line, "pid=")
	if i < 0 {
		return 0
	}
	rest := line[i+4:]
	end := strings.IndexAny(rest, ",)")
	if end < 0 {
		end = len(rest)
	}
	pid, _ := strconv.Atoi(rest[:end])
	return pid
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
