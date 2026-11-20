package singbox_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/singbox"
)

func TestConfigCheckerUsesCheckOnlyAndReportsErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "invalid"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "fake-core")
			cfg := filepath.Join(dir, "candidate.json")
			script := "#!/bin/sh\n[ \"$1\" = check ] && [ \"$2\" = -c ] && [ -f \"$3\" ] || exit 99\n"
			if fail {
				script += "echo 'invalid DNS reference' >&2\nexit 1\n"
			}
			if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cfg, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := singbox.ValidateConfigContext(context.Background(), bin, cfg)
			if (err != nil) != fail || (fail && !strings.Contains(err.Error(), "invalid DNS reference")) {
				t.Fatalf("unexpected check result: %v", err)
			}
		})
	}
}

func TestConfigCheckerHonorsDeadline(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "slow-core")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := singbox.ValidateConfigContext(ctx, bin, "unused")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("checker did not stop on deadline: %v", err)
	}
}
