package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Valden92/routebox/internal/config"
)

const ruleApplyWindow = 150 * time.Millisecond

type ruleApplyBatch struct {
	ctx       context.Context
	cancel    context.CancelFunc
	queuedAt  time.Time
	done      chan struct{}
	waiters   int
	apply     func(context.Context) (bool, error)
	reapplied bool
	err       error
}

// One active batch and at most one pending batch. New changes during an apply
// wait for the next pass, whose callback reads fresh saved settings.
type ruleApplyBatcher struct {
	mu      sync.Mutex
	pending *ruleApplyBatch
	worker  bool
	// Tests replace only the batching window, never runtime/network operations.
	wait func(context.Context, time.Time)
}

func (b *ruleApplyBatcher) request(ctx context.Context, apply func(context.Context) (bool, error)) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	if b.pending == nil || b.pending.ctx.Err() != nil {
		shared, cancel := context.WithCancel(context.Background())
		b.pending = &ruleApplyBatch{ctx: shared, cancel: cancel, queuedAt: time.Now(), done: make(chan struct{}), apply: apply}
	}
	batch := b.pending
	batch.waiters++
	if !b.worker {
		b.worker = true
		go b.run()
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		batch.waiters--
		if batch.waiters == 0 {
			batch.cancel()
		}
		b.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-batch.done:
		return batch.reapplied, batch.err
	}
}

func (b *ruleApplyBatcher) run() {
	for {
		b.mu.Lock()
		batch := b.pending
		if batch == nil {
			b.worker = false
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
		if b.wait != nil {
			b.wait(batch.ctx, batch.queuedAt)
		} else {
			timer := time.NewTimer(max(0, time.Until(batch.queuedAt.Add(ruleApplyWindow))))
			select {
			case <-timer.C:
			case <-batch.ctx.Done():
			}
			timer.Stop()
		}
		b.mu.Lock()
		if b.pending == batch {
			b.pending = nil
		}
		b.mu.Unlock()
		batch.execute()
	}
}

func (batch *ruleApplyBatch) execute() {
	defer func() {
		if recover() != nil {
			batch.reapplied = false
			batch.err = errors.New("внутренняя ошибка применения конфигурации")
		}
		batch.cancel()
		close(batch.done)
	}()
	if err := batch.ctx.Err(); err != nil {
		batch.err = err
	} else {
		batch.reapplied, batch.err = batch.apply(batch.ctx)
	}
}

func (s *Server) applyRuleBatch(ctx context.Context, reason string) (bool, error) {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.shuttingDown {
		return false, errors.New("демон завершает работу")
	}
	if s.Store.IsLocked() {
		return false, config.ErrLocked
	}
	if !s.SingBox.Running() {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return s.publishLiveRules(ctx, reason)
}
