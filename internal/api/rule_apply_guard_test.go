package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/config"
)

func TestRuleApplyDoesNotRestartStoppedRouter(t *testing.T) {
	s := routingSettingsFixture(t, true)
	s.ruleApplies.wait = func(context.Context, time.Time) { t.Fatal("stopped router queued an apply") }
	if restarted, err := s.reapplyPersonalIfRunning(context.Background()); err != nil || restarted {
		t.Fatalf("stopped router scheduled work: restart=%v err=%v", restarted, err)
	}
	if restarted, err := s.applyRuleBatch(context.Background(), "rules_changed"); err != nil || restarted {
		t.Fatalf("queued group restarted a stopped router: restart=%v err=%v", restarted, err)
	}
	if s.configApply.snapshot(s.Store.Get(), false).Attempt != 0 {
		t.Fatal("stopped router attempted candidate generation")
	}
}

func TestRuleApplyHonorsShutdownBeforeTouchingRuntime(t *testing.T) {
	s := &Server{shuttingDown: true}
	// Nil runtime/store prove the guard runs before host/config access.
	if restarted, err := s.applyRuleBatch(context.Background(), "rules_changed"); err == nil || restarted {
		t.Fatalf("shutdown guard ignored: restart=%v err=%v", restarted, err)
	}
}

func TestRuleApplyHonorsLockedSettingsBeforeTouchingRuntime(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Lock("fixture-password"); err != nil {
		t.Fatal(err)
	}
	locked, err := config.NewStore(store.Get().DataDir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: locked}
	if restarted, applyErr := s.applyRuleBatch(context.Background(), "rules_changed"); !errors.Is(applyErr, config.ErrLocked) || restarted {
		t.Fatalf("locked config guard ignored: restart=%v err=%v", restarted, applyErr)
	}
}

func TestCanceledApplyDoesNotAccessHostAfterWaitingForSerialization(t *testing.T) {
	s := &Server{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if restarted, err := s.applyRuleBatch(ctx, "rules_changed"); !errors.Is(err, context.Canceled) || restarted {
		t.Fatalf("canceled batch touched host: restart=%v err=%v", restarted, err)
	}
	if restarted, err := s.ensureRouterResult(ctx); !errors.Is(err, context.Canceled) || restarted {
		t.Fatalf("canceled serialized apply touched host: restart=%v err=%v", restarted, err)
	}
}
