package singbox

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
)

// The subprocess is this test binary, not sing-box; it has no network activity.
func TestManagerStopProcess(t *testing.T) {
	if os.Getenv("ROUTEBOX_TEST_STOP_PROCESS") != "1" {
		return
	}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGTERM)
	fmt.Println("ready")
	<-stopped
	os.Exit(0)
}

func newRunningStopTestManager(t *testing.T) *Manager {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestManagerStopProcess$")
	cmd.Env = append(os.Environ(), "ROUTEBOX_TEST_STOP_PROCESS=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	m := NewManager("unused", filepath.Join(t.TempDir(), "sing-box.json"))
	m.cmd = cmd
	m.running = true
	m.waitDone = make(chan struct{})
	go m.waitProcess(cmd)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-m.waitDone
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("test child did not become ready: line=%q err=%v", line, err)
	}
	return m
}

func TestManagerStopCleanupOwnership(t *testing.T) {
	for _, tc := range []struct {
		name      string
		probe     bool
		wantCalls int
	}{
		{name: "router cleans its routes", wantCalls: 1},
		{name: "probe never cleans router routes", probe: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRunningStopTestManager(t)
			if tc.probe {
				m.SetProbeMode()
			}
			calls := 0
			cleanup := func() { calls++ }
			if err := m.stop(cleanup); err != nil {
				t.Fatal(err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("cleanup called %d times, want %d", calls, tc.wantCalls)
			}
			if m.Running() || m.cmd != nil || m.LastError() != "" {
				t.Fatal("stopped manager retained process state")
			}
			if err := m.stop(cleanup); err != nil {
				t.Fatal(err)
			}
			if calls != tc.wantCalls {
				t.Fatal("repeated stop performed cleanup again")
			}
		})
	}
}

func TestManagerStopWithoutProcess(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    *Manager
	}{
		{name: "never started", m: &Manager{}},
		{name: "missing command", m: &Manager{running: true}},
		{name: "missing process", m: &Manager{running: true, cmd: &exec.Cmd{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.m.stop(func() { t.Fatal("cleanup without an owned process") }); err != nil {
				t.Fatal(err)
			}
		})
	}
}
