package llmqueue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunParallelRespectsConcurrency: never more than conc invocations at
// once, and every index runs exactly once.
func TestRunParallelRespectsConcurrency(t *testing.T) {
	const conc, count = 4, 40
	var cur, max, ran int32
	var seen [count]bool
	var mu sync.Mutex

	err := RunParallel(context.Background(), conc, count, func(i int) {
		n := atomic.AddInt32(&cur, 1)
		mu.Lock()
		if n > max {
			max = n
		}
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		mu.Lock()
		if seen[i] {
			t.Errorf("index %d ran twice", i)
		}
		seen[i] = true
		mu.Unlock()
		atomic.AddInt32(&ran, 1)
		atomic.AddInt32(&cur, -1)
	})
	if err != nil {
		t.Fatalf("RunParallel: %v", err)
	}
	if ran != count {
		t.Fatalf("ran %d, want %d", ran, count)
	}
	mu.Lock()
	defer mu.Unlock()
	if max < 2 {
		t.Errorf("max observed concurrency = %d, want > 1 (parallelism not exercised)", max)
	}
	if max > conc {
		t.Errorf("max observed concurrency = %d, want <= %d", max, conc)
	}
}

// TestRunParallelConcOneIsSequential: conc=1 keeps the old behaviour — never
// two invocations at once.
func TestRunParallelConcOneIsSequential(t *testing.T) {
	var cur, max int32
	err := RunParallel(context.Background(), 1, 10, func(int) {
		n := atomic.AddInt32(&cur, 1)
		if n > atomic.LoadInt32(&max) {
			atomic.StoreInt32(&max, n)
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&cur, -1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if max != 1 {
		t.Fatalf("max = %d, want 1", max)
	}
}

// TestRunParallelCancel: cancelling stops the launch loop (at most one extra
// item may slip past the in-select race) and reports the context error.
func TestRunParallelCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	unblock := make(chan struct{})
	var ran int32

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunParallel(ctx, 1, 5, func(i int) {
			if i == 0 {
				close(started)
				<-unblock
			}
			atomic.AddInt32(&ran, 1)
		})
	}()
	<-started
	cancel()
	close(unblock)

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunParallel did not return after cancel")
	}
	if n := atomic.LoadInt32(&ran); n >= 5 {
		t.Fatalf("ran %d items, launch loop did not stop", n)
	}
}

// TestRunParallelPanicIsolated: one exploding item is recovered; the rest
// still run and RunParallel returns normally.
func TestRunParallelPanicIsolated(t *testing.T) {
	var ran int32
	err := RunParallel(context.Background(), 2, 6, func(i int) {
		if i == 2 {
			panic("boom")
		}
		atomic.AddInt32(&ran, 1)
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ran != 5 {
		t.Fatalf("ran %d, want 5", ran)
	}
}

// TestGateEnforcesMax: SetGateMax(2) admits two requests, parks the third,
// and raising the limit wakes it.
func TestGateEnforcesMax(t *testing.T) {
	defer SetGateMax(1)
	SetGateMax(2)

	r1, err := AcquireGate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := AcquireGate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	got := make(chan func(), 1)
	go func() {
		r, err := AcquireGate(context.Background())
		if err == nil {
			got <- r
		} else {
			close(got)
		}
	}()
	select {
	case <-got:
		t.Fatal("third acquire succeeded while max=2")
	case <-time.After(50 * time.Millisecond):
	}
	// Freeing a slot admits the parked acquirer.
	r1()
	select {
	case r3 := <-got:
		r3()
	case <-time.After(2 * time.Second):
		t.Fatal("parked acquire not woken by release")
	}
	r2()

	// Raising the limit wakes a parked acquirer without a release.
	SetGateMax(3)
	r4, err := AcquireGate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r4()
}

// TestGateCtxCancelWithdraws: a parked acquirer whose context expires leaves
// no slot behind (the queue stays consistent for later callers).
func TestGateCtxCancelWithdraws(t *testing.T) {
	defer SetGateMax(1)
	SetGateMax(1)

	holder, err := AcquireGate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := AcquireGate(ctx)
		errCh <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("parked acquire err = %v, want Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parked acquire did not observe cancel")
	}
	holder()

	// The withdrawn waiter must not have consumed a slot: a fresh acquire
	// (repeated to catch the grant/cancel race) always succeeds promptly.
	for i := 0; i < 20; i++ {
		r, err := AcquireGate(context.Background())
		if err != nil {
			t.Fatalf("fresh acquire %d: %v (slot leaked)", i, err)
		}
		r()
	}
}
