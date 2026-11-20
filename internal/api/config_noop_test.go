package api

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestUnchangedCandidateDoesNotReplaceFileOrRuntime(t *testing.T) {
	f, tracker, st := applyStateFixture(t)
	before := tracker.snapshot(st, true)
	fileBefore, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	checked := false
	reapplied, err := tracker.apply(context.Background(), st, f.path, "rules_changed", func(path string) error {
		checked = true
		return prepareRollbackFixture(path)
	}, f.runtime)
	if err != nil || reapplied || !checked || f.starts != 1 || f.stops != 0 || f.checks != 2 {
		t.Fatalf("no-op touched runtime or skipped verification: restart=%v err=%v events=%v", reapplied, err, f.events)
	}
	fileAfter, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(fileBefore, fileAfter) || !fileBefore.ModTime().Equal(fileAfter.ModTime()) {
		t.Fatal("no-op replaced the working file")
	}
	if _, err = os.Stat(f.path + ".previous-working"); !os.IsNotExist(err) {
		t.Fatalf("no-op wrote a backup: %v", err)
	}
	after := tracker.snapshot(st, true)
	if !after.Unchanged || after.Phase != "applied" || after.AppliedVersion != before.AppliedVersion || !after.LastSuccessfulAt.Equal(*before.LastSuccessfulAt) || after.MatchesSavedSettings == nil || !*after.MatchesSavedSettings {
		t.Fatalf("no-op state: %+v", after)
	}
}

func TestNoopRequiresConfirmedLiveVersionAndReadiness(t *testing.T) {
	for _, condition := range []string{"unknown version", "stopped", "disk changed", "readiness failed", "manual reapply", "candidate changed"} {
		t.Run(condition, func(t *testing.T) {
			f, tracker, st := applyStateFixture(t)
			reason := "rules_changed"
			prepare := prepareRollbackFixture
			switch condition {
			case "unknown version":
				tracker = &configApplyTracker{}
			case "stopped":
				f.alive = false
			case "disk changed":
				if err := os.WriteFile(f.path, []byte("external edit"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "readiness failed":
				f.readyErrors[2] = errors.New("fixture health failure")
			case "manual reapply":
				reason = "manual_reapply"
			case "candidate changed":
				prepare = func(path string) error { return os.WriteFile(path, []byte("changed candidate"), 0o600) }
			}
			reapplied, err := tracker.apply(context.Background(), st, f.path, reason, prepare, f.runtime)
			if err != nil || !reapplied || f.starts != 2 || tracker.snapshot(st, true).Unchanged {
				t.Fatalf("necessary restart was skipped: restarted=%v err=%v events=%v", reapplied, err, f.events)
			}
		})
	}
}

func TestUnchangedCandidateStillFailsValidation(t *testing.T) {
	f, tracker, st := applyStateFixture(t)
	failure := errors.New("fixture validation rejection")
	reapplied, err := tracker.apply(context.Background(), st, f.path, "rules_changed", func(path string) error {
		if writeErr := prepareRollbackFixture(path); writeErr != nil {
			return writeErr
		}
		return failure
	}, f.runtime)
	state := tracker.snapshot(st, true)
	if !errors.Is(err, failure) || reapplied || f.starts != 1 || f.stops != 0 || state.Unchanged || state.Phase != "rejected" {
		t.Fatalf("validation failure ignored: restarted=%v err=%v state=%+v", reapplied, err, state)
	}
}

func TestNoopDoesNotTurnCancellationIntoRestart(t *testing.T) {
	f, tracker, st := applyStateFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.runtime.ready = func(context.Context) error {
		cancel()
		return ctx.Err()
	}
	reapplied, err := tracker.apply(ctx, st, f.path, "rules_changed", prepareRollbackFixture, f.runtime)
	if !errors.Is(err, context.Canceled) || reapplied || f.starts != 1 || f.stops != 0 {
		t.Fatalf("canceled no-op triggered runtime changes: restart=%v err=%v events=%v", reapplied, err, f.events)
	}
}

func TestIdenticalGeneratedConfigAcknowledgesEquivalentSavedSettings(t *testing.T) {
	f, tracker, st := applyStateFixture(t)
	// Simulate a settings difference omitted by a generator (e.g. an unavailable path).
	st.DomainRules = []config.DomainRule{{Pattern: "disabled-path.test", Path: config.RoutePersonal, Enabled: true}}
	reapplied, err := tracker.apply(context.Background(), st, f.path, "rules_changed", prepareRollbackFixture, f.runtime)
	state := tracker.snapshot(st, true)
	if err != nil || reapplied || state.MatchesSavedSettings == nil || !*state.MatchesSavedSettings {
		t.Fatalf("equivalent settings not acknowledged: restart=%v err=%v state=%+v", reapplied, err, state)
	}
}
