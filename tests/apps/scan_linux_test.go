//go:build linux

package apps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/apps"
)

func fakeSS(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ss"), []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
func TestScanUsesReadOnlySocketSnapshotFlags(t *testing.T) {
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("ROUTEBOX_TEST_SS_ARGS", args)
	fakeSS(t, `printf '%s\n' "$@" > "$ROUTEBOX_TEST_SS_ARGS"
printf '%s\n' 'tcp LISTEN 0 5 127.0.0.1:8000 0.0.0.0:* users:(("fixture",pid=2000000000,fd=3))'
`)
	list, err := apps.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].PID != 2000000000 || list[0].Sockets[0].State != "LISTEN" {
		t.Fatalf("bad scan %+v", list)
	}
	got, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "-H\n-O\n-n\n-t\n-u\n-a\n-p\n" {
		t.Fatalf("flags %q", got)
	}
}
func TestScanFailureAndCancellation(t *testing.T) {
	fakeSS(t, "printf 'private output' >&2\nexit 1\n")
	_, err := apps.Scan(context.Background())
	if err == nil || strings.Contains(err.Error(), "private output") {
		t.Fatalf("bad error %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = apps.Scan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
