package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestCandidateRejectsBeforeRuntimeOrFileChanges(t *testing.T) {
	for _, phase := range []string{"generation", "schema", "core", "cancel", "rename"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "sing-box.json")
			if err := os.WriteFile(path, []byte("working"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			called := false
			err := applyCandidateConfig(ctx, path, func(candidate string) error {
				if phase == "generation" {
					return errors.New("generation failed")
				}
				if err := os.WriteFile(candidate, []byte("candidate"), 0o600); err != nil {
					return err
				}
				if phase == "schema" {
					return singbox.ValidateConfigBytes([]byte(`{"outbounds":[]}`), true)
				}
				if phase == "core" {
					return singbox.ValidateConfigContext(ctx, filepath.Join(dir, "missing-core"), candidate)
				}
				if phase == "cancel" {
					cancel()
				}
				if phase == "rename" {
					return os.Remove(candidate)
				}
				return nil
			}, func() error { called = true; return nil })
			if err == nil || called {
				t.Fatalf("invalid candidate reached runtime: err=%v apply=%v", err, called)
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil || string(body) != "working" {
				t.Fatalf("working file changed: %s %v", body, readErr)
			}
			files, globErr := filepath.Glob(filepath.Join(dir, ".sing-box-candidate-*"))
			if globErr != nil || len(files) != 0 {
				t.Fatalf("candidate leaked: %v %v", files, globErr)
			}
		})
	}
}

func TestCandidateCommitPrecedesRuntimeAndKeepsPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sing-box.json")
	stages := []string{}
	failure := errors.New("start failed")
	err := applyCandidateConfig(context.Background(), path, func(candidate string) error {
		stages = append(stages, "validate")
		info, err := os.Stat(candidate)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("candidate mode %o", info.Mode().Perm())
		}
		return os.WriteFile(candidate, []byte("validated"), 0o600)
	}, func() error {
		stages = append(stages, "apply")
		body, err := os.ReadFile(path)
		if err != nil || string(body) != "validated" {
			t.Fatalf("runtime saw uncommitted config: %s %v", body, err)
		}
		return failure
	})
	if !errors.Is(err, failure) || strings.Join(stages, ",") != "validate,apply" {
		t.Fatalf("order=%v error=%v", stages, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode: %v %v", info, err)
	}
}

func TestCancelledCandidateDoesNoPreparation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := applyCandidateConfig(ctx, filepath.Join(t.TempDir(), "sub", "config.json"), func(string) error { t.Fatal("prepared after cancellation"); return nil }, func() error { t.Fatal("applied after cancellation"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestCandidateSuccessAndStorageFailure(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "blocked parent"}[blocked], func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "config")
			if blocked {
				if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(parent, "sing-box.json")
			prepared, applied := false, false
			err := applyCandidateConfig(context.Background(), path, func(candidate string) error {
				prepared = true
				return os.WriteFile(candidate, []byte("valid"), 0o600)
			}, func() error { applied = true; return nil })
			if (err != nil) != blocked || prepared == blocked || applied == blocked {
				t.Fatalf("blocked=%v prepare=%v apply=%v err=%v", blocked, prepared, applied, err)
			}
		})
	}
}

func TestCandidateGenerationErrorPreservesExistingConfig(t *testing.T) {
	st := config.DefaultSettings(t.TempDir())
	st.PersonalVPN.Enabled = true
	path := filepath.Join(st.DataDir, "sing-box.json")
	if err := os.WriteFile(path, []byte("working"), 0o600); err != nil {
		t.Fatal(err)
	}
	node := &subscription.Node{RawURI: "unsupported://test@example.com:443"}
	err := applyCandidateConfig(context.Background(), path, func(candidate string) error {
		return singbox.WriteRouterConfig(candidate, node, st, "tun100")
	}, func() error { t.Fatal("invalid outbound reached stop/start"); return nil })
	body, readErr := os.ReadFile(path)
	if err == nil || readErr != nil || string(body) != "working" {
		t.Fatalf("err=%v body=%s read=%v", err, body, readErr)
	}
}
