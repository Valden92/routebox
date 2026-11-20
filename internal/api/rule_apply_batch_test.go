package api

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type batchTestResult struct {
	reapplied bool
	err       error
}

func newBatchFixture(t *testing.T) (*ruleApplyBatcher, chan chan struct{}) {
	t.Helper()
	windows := make(chan chan struct{}, 10)
	b := &ruleApplyBatcher{wait: func(ctx context.Context, _ time.Time) {
		release := make(chan struct{})
		windows <- release
		select {
		case <-release:
		case <-ctx.Done():
		}
	}}
	return b, windows
}

func waitBatchValue[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("fixture timed out")
		var zero T
		return zero
	}
}

func waitBatchMembers(t *testing.T, b *ruleApplyBatcher, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		count := 0
		if b.pending != nil {
			count = b.pending.waiters
		}
		b.mu.Unlock()
		if count == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("requests did not join pending batch")
}

func requestBatch(t *testing.T, request func() (bool, error)) <-chan batchTestResult {
	t.Helper()
	results := make(chan batchTestResult, 1)
	go func() {
		reapplied, err := request()
		results <- batchTestResult{reapplied, err}
	}()
	return results
}

func TestRuleChangesShareOneApplyAndResult(t *testing.T) {
	b, windows := newBatchFixture(t)
	var calls, desired atomic.Int32
	desired.Store(1)
	seen := make(chan int32, 1)
	apply := func(context.Context) (bool, error) {
		calls.Add(1)
		seen <- desired.Load()
		return true, nil
	}
	results := []<-chan batchTestResult{requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })}
	release := waitBatchValue(t, windows)
	for range 9 {
		desired.Add(1)
		results = append(results, requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) }))
	}
	waitBatchMembers(t, b, 10)
	close(release)
	for _, result := range results {
		if got := waitBatchValue(t, result); got.err != nil || !got.reapplied {
			t.Fatalf("shared result: %+v", got)
		}
	}
	if calls.Load() != 1 || waitBatchValue(t, seen) != 10 {
		t.Fatal("batch did not apply the latest saved state once")
	}
}

func TestChangesDuringApplyUseOneFollowingBatch(t *testing.T) {
	b, windows := newBatchFixture(t)
	var calls, desired, active atomic.Int32
	desired.Store(1)
	started := make(chan int32, 3)
	finishFirst := make(chan struct{})
	apply := func(context.Context) (bool, error) {
		if active.Add(1) != 1 {
			t.Error("parallel applies")
		}
		defer active.Add(-1)
		call := calls.Add(1)
		started <- desired.Load()
		if call == 1 {
			<-finishFirst
		}
		return true, nil
	}
	first := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	close(waitBatchValue(t, windows))
	if waitBatchValue(t, started) != 1 {
		t.Fatal("wrong first snapshot")
	}
	desired.Store(2)
	second := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	desired.Store(3)
	third := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	waitBatchMembers(t, b, 2)
	if calls.Load() != 1 {
		t.Fatal("new group ran before active apply finished")
	}
	close(finishFirst)
	if got := waitBatchValue(t, first); got.err != nil {
		t.Fatal(got.err)
	}
	close(waitBatchValue(t, windows))
	if waitBatchValue(t, started) != 3 {
		t.Fatal("following batch read stale settings")
	}
	for _, result := range []<-chan batchTestResult{second, third} {
		if got := waitBatchValue(t, result); got.err != nil {
			t.Fatal(got.err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("changes during apply produced redundant batches")
	}
}

func TestCancelOneRequestKeepsOtherBatchMembers(t *testing.T) {
	b, windows := newBatchFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	apply := func(shared context.Context) (bool, error) { return true, shared.Err() }
	first := requestBatch(t, func() (bool, error) { return b.request(ctx, apply) })
	release := waitBatchValue(t, windows)
	second := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	waitBatchMembers(t, b, 2)
	cancel()
	if got := waitBatchValue(t, first); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("caller cancellation lost: %+v", got)
	}
	close(release)
	if got := waitBatchValue(t, second); got.err != nil || !got.reapplied {
		t.Fatalf("one cancellation aborted another caller: %+v", got)
	}
}

func TestCanceledPendingBatchDoesNotApplyOrPoisonNextBatch(t *testing.T) {
	b, windows := newBatchFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	apply := func(context.Context) (bool, error) { calls.Add(1); return true, nil }
	first := requestBatch(t, func() (bool, error) { return b.request(ctx, apply) })
	_ = waitBatchValue(t, windows)
	cancel()
	if got := waitBatchValue(t, first); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("caller cancellation lost: %+v", got)
	}
	second := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	close(waitBatchValue(t, windows))
	if got := waitBatchValue(t, second); got.err != nil || calls.Load() != 1 {
		t.Fatalf("canceled group applied or poisoned next group: %+v, calls=%d", got, calls.Load())
	}
}

func TestLastCanceledCallerCancelsActiveApply(t *testing.T) {
	b, windows := newBatchFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, stopped := make(chan struct{}), make(chan struct{})
	apply := func(shared context.Context) (bool, error) {
		close(started)
		<-shared.Done()
		close(stopped)
		return false, shared.Err()
	}
	result := requestBatch(t, func() (bool, error) { return b.request(ctx, apply) })
	close(waitBatchValue(t, windows))
	_ = waitBatchValue(t, started)
	cancel()
	_ = waitBatchValue(t, stopped)
	if got := waitBatchValue(t, result); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("last waiter did not cancel work: %+v", got)
	}
}

func TestBatchFailureIsSharedWithoutAutomaticRetry(t *testing.T) {
	b, windows := newBatchFixture(t)
	failure := errors.New("fixture validation failure")
	var calls atomic.Int32
	apply := func(context.Context) (bool, error) { calls.Add(1); return false, failure }
	first := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	release := waitBatchValue(t, windows)
	second := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	waitBatchMembers(t, b, 2)
	close(release)
	for _, result := range []<-chan batchTestResult{first, second} {
		if got := waitBatchValue(t, result); !errors.Is(got.err, failure) || got.reapplied {
			t.Fatalf("batch failure lost: %+v", got)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("failed batch retried without a new request")
	}
}

func TestDefaultBatchWindowAndAlreadyCanceledRequest(t *testing.T) {
	var b ruleApplyBatcher
	var calls atomic.Int32
	apply := func(context.Context) (bool, error) { calls.Add(1); return false, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.request(ctx, apply); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatal("already canceled request started work")
	}
	result := waitBatchValue(t, requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) }))
	if result.err != nil || result.reapplied || calls.Load() != 1 {
		t.Fatalf("default window failed to propagate no-op: %+v", result)
	}
}

func TestPanicReleasesBatchWaitersAndKeepsWorkerAvailable(t *testing.T) {
	b, windows := newBatchFixture(t)
	apply := func(context.Context) (bool, error) { panic("fixture private password") }
	first := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	release := waitBatchValue(t, windows)
	second := requestBatch(t, func() (bool, error) { return b.request(context.Background(), apply) })
	waitBatchMembers(t, b, 2)
	close(release)
	for _, result := range []<-chan batchTestResult{first, second} {
		got := waitBatchValue(t, result)
		if got.err == nil || got.reapplied || strings.Contains(got.err.Error(), "password") {
			t.Fatalf("panic was hidden, leaked or left requests waiting: %+v", got)
		}
	}
	next := requestBatch(t, func() (bool, error) {
		return b.request(context.Background(), func(context.Context) (bool, error) { return true, nil })
	})
	close(waitBatchValue(t, windows))
	if got := waitBatchValue(t, next); got.err != nil || !got.reapplied {
		t.Fatalf("panic poisoned following request: %+v", got)
	}
}
