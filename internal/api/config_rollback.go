package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Runtime callbacks keep transaction tests independent of host routes/processes.
type routerRuntime struct {
	running  func() bool
	stop     func() error
	start    func(context.Context) error
	ready    func(context.Context) error
	progress func(string)
}

func applyRouterConfig(ctx context.Context, path string, prepare func(string) error, runtime routerRuntime) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wasRunning := runtime.running()
	previous, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("чтение предыдущей конфигурации: %w", err)
	}
	if wasRunning && !existed {
		return fmt.Errorf("рабочий процесс сохранён: отсутствует конфигурация для отката")
	}
	return applyCandidateConfig(ctx, path, func(candidate string) error {
		if err := prepare(candidate); err != nil {
			return err
		}
		if wasRunning {
			return writePrivateConfig(path+".previous-working", previous)
		}
		return nil
	}, func() error {
		if runtime.progress != nil {
			runtime.progress("applying")
		}
		if wasRunning {
			if stopErr := runtime.stop(); stopErr != nil {
				if runtime.running() {
					if restoreErr := restoreRouterFile(path, previous, existed); restoreErr != nil {
						return &routerApplyError{cause: fmt.Errorf("остановка и восстановление файла не удались: %w", errors.Join(stopErr, restoreErr)), rollback: "failed"}
					}
					return &routerApplyError{cause: fmt.Errorf("рабочий процесс не заменён, предыдущий файл восстановлен: %w", stopErr), rollback: "preserved"}
				}
				return rollbackRouterConfig(path, previous, existed, wasRunning, runtime, stopErr)
			}
		}
		if startErr := startRouterReady(ctx, runtime); startErr != nil {
			return rollbackRouterConfig(path, previous, existed, wasRunning, runtime, startErr)
		}
		return nil
	})
}

func startRouterReady(ctx context.Context, runtime routerRuntime) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := runtime.start(ctx); err != nil {
		return err
	}
	if err := runtime.ready(ctx); err != nil {
		return fmt.Errorf("маршрутизатор не подтвердил готовность: %w", err)
	}
	if !runtime.running() {
		return fmt.Errorf("маршрутизатор завершился во время проверки готовности")
	}
	return nil
}

func rollbackRouterConfig(path string, previous []byte, existed, wasRunning bool, runtime routerRuntime, applyErr error) error {
	if runtime.progress != nil {
		runtime.progress("rolling_back")
	}
	// Closing the UI/request must not cancel recovery of the user's previous router.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if runtime.running() {
		if err := runtime.stop(); err != nil {
			return &routerApplyError{cause: fmt.Errorf("применение не удалось; остановка нового процесса для отката не удалась: %w", errors.Join(applyErr, err)), rollback: "failed"}
		}
	}
	if err := restoreRouterFile(path, previous, existed); err != nil {
		return &routerApplyError{cause: fmt.Errorf("применение не удалось; восстановление файла не удалось: %w", errors.Join(applyErr, err)), rollback: "failed"}
	}
	if !wasRunning {
		return &routerApplyError{cause: fmt.Errorf("применение не удалось; прежнее состояние файла восстановлено, ранее запущенного маршрутизатора не было: %w", applyErr), rollback: "file_restored"}
	}
	if err := startRouterReady(ctx, runtime); err != nil {
		return &routerApplyError{cause: fmt.Errorf("применение не удалось; предыдущий файл восстановлен, но повторный запуск не удался: %w", errors.Join(applyErr, err)), rollback: "failed"}
	}
	return &routerApplyError{cause: fmt.Errorf("применение не удалось; предыдущая рабочая конфигурация и процесс восстановлены: %w", applyErr), rollback: "restored", restoredVersion: configVersion(previous)}
}

func restoreRouterFile(path string, previous []byte, existed bool) error {
	if existed {
		return writePrivateConfig(path, previous)
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func writePrivateConfig(path string, body []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".sing-box-restore-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(body)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	return os.Rename(f.Name(), path)
}
