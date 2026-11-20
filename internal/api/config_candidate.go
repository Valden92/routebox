package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// prepare must generate and validate the candidate without changing the runtime.
// Commit the checked file atomically before invoking any stop/start operation.
// Failed starts still need runtime rollback (the next apply stage).
func applyCandidateConfig(ctx context.Context, path string, prepare func(string) error, apply func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sing-box-candidate-*")
	if err != nil {
		return err
	}
	candidate := f.Name()
	defer func() { _ = os.Remove(candidate) }()
	if err := f.Close(); err != nil {
		return err
	}
	if err := prepare(candidate); err != nil {
		return fmt.Errorf("проверка конфигурации до применения: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(candidate, path); err != nil {
		return fmt.Errorf("сохранение проверенной конфигурации: %w", err)
	}
	return apply()
}
