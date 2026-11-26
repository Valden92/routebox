package daemonlock_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Valden92/routebox/internal/daemonlock"
)

func TestExclusiveOwnershipAndStaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.lock")
	first, err := daemonlock.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if other, acquireErr := daemonlock.Acquire(path); !errors.Is(acquireErr, daemonlock.ErrHeld) {
		if other != nil {
			_ = other.Close()
		}
		t.Fatalf("second owner: %v", acquireErr)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("loser changed owner record")
	}
	if closeErr := first.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	next, err := daemonlock.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("lock file is not private")
	}
}

func TestRejectsSymlinkAndSharedInode(t *testing.T) {
	for _, hard := range []bool{false, true} {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		path := filepath.Join(dir, "lock")
		if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		if hard {
			err = os.Link(target, path)
		} else {
			err = os.Symlink(target, path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if lock, acquireErr := daemonlock.Acquire(path); acquireErr == nil {
			_ = lock.Close()
			t.Fatal("unsafe lock accepted")
		}
		body, err := os.ReadFile(target)
		if err != nil || string(body) != "preserve" {
			t.Fatal("unsafe lock modified target")
		}
	}
}

func TestCrashReleasesOwnershipWithoutDeletingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	child := exec.Command(os.Args[0], "-test.run=^TestLockHelper$")
	child.Env = append(os.Environ(), "ROUTEBOX_LOCK_HELPER="+path)
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	if !bufio.NewScanner(out).Scan() {
		t.Fatal("helper did not acquire lock")
	}
	if lock, acquireErr := daemonlock.Acquire(path); !errors.Is(acquireErr, daemonlock.ErrHeld) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("child ownership missing: %v", acquireErr)
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	lock, err := daemonlock.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
}

func TestLockHelper(t *testing.T) {
	path := os.Getenv("ROUTEBOX_LOCK_HELPER")
	if path == "" {
		return
	}
	lock, err := daemonlock.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	fmt.Println("ready")
	<-sig
}

func TestOwnershipPathIgnoresDataAndConfigEnvironment(t *testing.T) {
	before := daemonlock.Path()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if daemonlock.Path() != before {
		t.Fatal("environment changed the host router ownership path")
	}
}
