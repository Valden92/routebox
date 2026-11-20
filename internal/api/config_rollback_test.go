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
)

func TestIncompleteNetworkSnapshotPreservesRouter(t *testing.T) {
	f := newRollbackFixture(t, true, true)
	st := config.DefaultSettings(filepath.Dir(f.path))
	snapshot := singbox.RouterNetworkSnapshot{SystemVPNUp: true, SystemInterface: "tun9"}
	err := applyRouterConfig(context.Background(), f.path, func(candidate string) error {
		return singbox.WriteRouterConfigWithSnapshot(candidate, nil, st, "tun100", snapshot)
	}, f.runtime)
	if err == nil || len(f.events) != 0 || !f.alive {
		t.Fatalf("incomplete snapshot changed runtime: %v %v", err, f.events)
	}
	assertRollbackFile(t, f.path, "old")
}

type rollbackFixture struct {
	path                                 string
	alive                                bool
	starts, stops, checks                int
	events                               []string
	startErrors, stopErrors, readyErrors map[int]error
	leaveStopped                         bool
	partialStart                         bool
	dieOnReady                           bool
	onStart                              func(int)
	runtime                              routerRuntime
}

func newRollbackFixture(t *testing.T, running, exists bool) *rollbackFixture {
	t.Helper()
	f := &rollbackFixture{path: filepath.Join(t.TempDir(), "sing-box.json"), alive: running, startErrors: map[int]error{}, stopErrors: map[int]error{}, readyErrors: map[int]error{}}
	if exists {
		if err := os.WriteFile(f.path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.runtime = routerRuntime{
		running: func() bool { return f.alive },
		stop: func() error {
			f.stops++
			f.events = append(f.events, "stop")
			if err := f.stopErrors[f.stops]; err != nil {
				if f.leaveStopped {
					f.alive = false
				}
				return err
			}
			f.alive = false
			return nil
		},
		start: func(ctx context.Context) error {
			f.starts++
			if ctx.Err() != nil {
				t.Fatalf("recovery inherited cancellation: %v", ctx.Err())
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("start has no bounded context")
			}
			body, err := os.ReadFile(f.path)
			if err != nil {
				return err
			}
			f.events = append(f.events, "start:"+string(body))
			if f.onStart != nil {
				f.onStart(f.starts)
			}
			if err := f.startErrors[f.starts]; err != nil {
				f.alive = f.partialStart
				return err
			}
			f.alive = true
			return nil
		},
		ready: func(ctx context.Context) error {
			f.checks++
			f.events = append(f.events, "ready")
			if ctx.Err() != nil {
				t.Fatalf("ready context already canceled: %v", ctx.Err())
			}
			if f.dieOnReady && f.checks == 1 {
				f.alive = false
			}
			return f.readyErrors[f.checks]
		},
	}
	return f
}

func prepareRollbackFixture(path string) error { return os.WriteFile(path, []byte("new"), 0o600) }

func assertRollbackFile(t *testing.T, path, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil || string(body) != want {
		t.Fatalf("file %s: body=%q error=%v", filepath.Base(path), body, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private file mode: %v %v", info, err)
	}
}

func TestRouterTransactionSuccess(t *testing.T) {
	f := newRollbackFixture(t, true, true)
	if err := applyRouterConfig(context.Background(), f.path, prepareRollbackFixture, f.runtime); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.events, ",") != "stop,start:new,ready" || !f.alive {
		t.Fatalf("runtime order: %v", f.events)
	}
	assertRollbackFile(t, f.path, "new")
	assertRollbackFile(t, f.path+".previous-working", "old")
}

func TestRouterTransactionRestoresAfterFailedStartOrReadiness(t *testing.T) {
	for _, phase := range []string{"start", "partial start", "ready", "process exited"} {
		t.Run(phase, func(t *testing.T) {
			f := newRollbackFixture(t, true, true)
			failure := errors.New("candidate rejected")
			switch phase {
			case "start":
				f.startErrors[1] = failure
			case "partial start":
				f.startErrors[1] = failure
				f.partialStart = true
			case "ready":
				f.readyErrors[1] = failure
			case "process exited":
				f.dieOnReady = true
			}
			err := applyRouterConfig(context.Background(), f.path, prepareRollbackFixture, f.runtime)
			if err == nil || !strings.Contains(err.Error(), "процесс восстановлены") || !f.alive || f.starts != 2 {
				t.Fatalf("rollback result: %v events=%v", err, f.events)
			}
			if phase != "process exited" && !errors.Is(err, failure) {
				t.Fatalf("original error lost: %v", err)
			}
			if (phase == "ready" || phase == "partial start") && f.stops != 2 {
				t.Fatalf("candidate was not stopped: %v", f.events)
			}
			assertRollbackFile(t, f.path, "old")
			assertRollbackFile(t, f.path+".previous-working", "old")
		})
	}
}

func TestRouterRecoverySurvivesRequestCancellation(t *testing.T) {
	f := newRollbackFixture(t, true, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onStart = func(n int) {
		if n == 1 {
			cancel()
		}
	}
	f.startErrors[1] = context.Canceled
	err := applyRouterConfig(ctx, f.path, prepareRollbackFixture, f.runtime)
	if !errors.Is(err, context.Canceled) || !f.alive || f.starts != 2 {
		t.Fatalf("request cancellation prevented recovery: %v %v", err, f.events)
	}
	assertRollbackFile(t, f.path, "old")
}

func TestRouterRollbackFailuresAreExplicit(t *testing.T) {
	for _, phase := range []string{"stop candidate", "restore file", "restart old", "old readiness"} {
		t.Run(phase, func(t *testing.T) {
			f := newRollbackFixture(t, true, true)
			candidateErr := errors.New("candidate failed")
			recoveryErr := errors.New("recovery failed")
			f.readyErrors[1] = candidateErr
			switch phase {
			case "stop candidate":
				f.stopErrors[2] = recoveryErr
			case "restart old":
				f.startErrors[2] = recoveryErr
			case "old readiness":
				f.readyErrors[2] = recoveryErr
			case "restore file":
				f.onStart = func(n int) {
					if n == 1 {
						if err := os.Remove(f.path); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(f.path, 0o700); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			err := applyRouterConfig(context.Background(), f.path, prepareRollbackFixture, f.runtime)
			if err == nil || !errors.Is(err, candidateErr) || strings.Contains(err.Error(), "процесс восстановлены") {
				t.Fatalf("false recovery success: %v", err)
			}
			if phase != "restore file" && !errors.Is(err, recoveryErr) {
				t.Fatalf("recovery cause lost: %v", err)
			}
			assertRollbackFile(t, f.path+".previous-working", "old")
			if phase == "stop candidate" || phase == "restore file" {
				if f.starts != 1 {
					t.Fatalf("unsafe restart after failed cleanup: %v", f.events)
				}
			} else {
				assertRollbackFile(t, f.path, "old")
			}
		})
	}
}

func TestFirstRouterStartFailureRestoresFileWithoutUnexpectedRestart(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "no file", true: "old file"}[exists], func(t *testing.T) {
			f := newRollbackFixture(t, false, exists)
			f.startErrors[1] = errors.New("initial failure")
			err := applyRouterConfig(context.Background(), f.path, prepareRollbackFixture, f.runtime)
			if err == nil || f.starts != 1 || f.alive {
				t.Fatalf("unexpected initial recovery: %v %v", err, f.events)
			}
			if exists {
				assertRollbackFile(t, f.path, "old")
			} else {
				if _, err := os.Stat(f.path); !os.IsNotExist(err) {
					t.Fatalf("initial candidate remained: %v", err)
				}
			}
		})
	}
}

func TestRouterStopFailureDoesNotReplaceLiveProcess(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "still running", true: "already stopped"}[stopped], func(t *testing.T) {
			f := newRollbackFixture(t, true, true)
			failure := errors.New("stop failed")
			f.stopErrors[1] = failure
			f.leaveStopped = stopped
			err := applyRouterConfig(context.Background(), f.path, prepareRollbackFixture, f.runtime)
			if !errors.Is(err, failure) || !f.alive {
				t.Fatalf("old process lost: %v %v", err, f.events)
			}
			if (!stopped && f.starts != 0) || (stopped && f.starts != 1) {
				t.Fatalf("unexpected restart: %v", f.events)
			}
			assertRollbackFile(t, f.path, "old")
		})
	}
}

func TestRollbackPreflightErrorsLeaveRuntimeAlone(t *testing.T) {
	for _, phase := range []string{"missing old file", "unreadable old file", "bad candidate", "backup failure", "canceled"} {
		t.Run(phase, func(t *testing.T) {
			f := newRollbackFixture(t, true, phase != "missing old file")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "unreadable old file" {
				if err := os.Remove(f.path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(f.path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "backup failure" {
				if err := os.Mkdir(f.path+".previous-working", 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "canceled" {
				cancel()
			}
			err := applyRouterConfig(ctx, f.path, func(path string) error {
				if phase == "bad candidate" {
					return errors.New("invalid config")
				}
				return prepareRollbackFixture(path)
			}, f.runtime)
			if err == nil || len(f.events) != 0 || !f.alive {
				t.Fatalf("preflight touched runtime: %v %v", err, f.events)
			}
			if phase != "missing old file" && phase != "unreadable old file" {
				assertRollbackFile(t, f.path, "old")
			}
		})
	}
}
