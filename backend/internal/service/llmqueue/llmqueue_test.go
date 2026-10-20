package llmqueue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestFIFOOrder proves jobs run one at a time in enqueue order — the whole
// point of the queue for a model that serves a single request.
func TestFIFOOrder(t *testing.T) {
	q := New()
	defer q.Stop()

	var order []int
	started := make(chan struct{}, 8)
	// First job blocks the worker so the other two stay queued.
	release := make(chan struct{})
	j1, ok, err := q.Enqueue(KindTranslate, "a", func(ctx context.Context) error {
		started <- struct{}{}
		<-release
		order = append(order, 1)
		return nil
	})
	if err != nil || !ok {
		t.Fatalf("enqueue: ok=%v err=%v", ok, err)
	}
	j2, ok, err := q.Enqueue(KindTranslate, "b", func(context.Context) error {
		order = append(order, 2)
		return nil
	})
	if err != nil || !ok {
		t.Fatalf("enqueue: ok=%v err=%v", ok, err)
	}
	j3, ok, err := q.Enqueue(KindClassify, "c", func(context.Context) error {
		order = append(order, 3)
		return nil
	})
	if err != nil || !ok {
		t.Fatalf("enqueue: ok=%v err=%v", ok, err)
	}

	<-started // job 1 is running; 2 and 3 must still be queued
	if st := q.Status(); st.Queued != 2 || st.Current == nil || st.Current.Name != "a" {
		t.Fatalf("status while running: %+v", st)
	}
	close(release)

	for _, j := range []*Job{j1, j2, j3} {
		if err := j.Wait(); err != nil {
			t.Fatalf("job %s: %v", j.Info().Name, err)
		}
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("execution order %v, want [1 2 3]", order)
	}
}

// TestEnqueueDedup covers the double-click guard: same kind+name twice is
// refused while active, accepted again after the first finishes.
func TestEnqueueDedup(t *testing.T) {
	q := New()
	defer q.Stop()

	release := make(chan struct{})
	_, ok, err := q.Enqueue(KindClassify, "unclassified", func(ctx context.Context) error {
		<-release
		return nil
	})
	if err != nil || !ok {
		t.Fatalf("first enqueue: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := q.Enqueue(KindClassify, "unclassified", func(context.Context) error { return nil }); ok {
		t.Fatal("duplicate enqueue accepted, want refused")
	}
	// Different kind but same name is a different job.
	if _, ok, _ := q.Enqueue(KindTranslate, "unclassified", func(context.Context) error { return nil }); !ok {
		t.Fatal("different kind refused")
	}
	close(release)
}

// TestCancelQueued removes a waiting job without ever running it.
func TestCancelQueued(t *testing.T) {
	q := New()
	defer q.Stop()

	release := make(chan struct{})
	running, _, _ := q.Enqueue(KindTranslate, "block", func(ctx context.Context) error {
		<-release
		return nil
	})
	// Hold the worker on the first job so the victim is provably still queued.
	deadline := time.Now().Add(2 * time.Second)
	for {
		st := q.Status()
		if st.Current != nil && st.Current.Name == "block" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never started: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
	queued, _, _ := q.Enqueue(KindTranslate, "victim", func(context.Context) error {
		t.Error("cancelled job must not run")
		return nil
	})
	if n := q.Cancel(queued.Info().ID); n != true {
		t.Fatal("Cancel(queued) reported job unknown")
	}
	if err := queued.Wait(); !errors.Is(err, ErrCancelled) {
		t.Fatalf("queued.Wait = %v, want ErrCancelled", err)
	}
	close(release)
	if err := running.Wait(); err != nil {
		t.Fatalf("running job: %v", err)
	}
	if err := queued.Wait(); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Wait after completion = %v, want ErrCancelled", err)
	}
}

// TestCancelKindInterruptsRunning proves the "取消翻译" button stops the
// in-flight job: fn observes ctx.Done and Wait reports ErrCancelled.
func TestCancelKindInterruptsRunning(t *testing.T) {
	q := New()
	defer q.Stop()

	entered := make(chan struct{})
	j, _, _ := q.Enqueue(KindTranslate, "batch", func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	<-entered
	if n := q.CancelKind(KindTranslate); n != 1 {
		t.Fatalf("CancelKind = %d, want 1", n)
	}
	if err := j.Wait(); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Wait = %v, want ErrCancelled", err)
	}
	// A different kind must be untouched by CancelKind.
	j2, ok, _ := q.Enqueue(KindClassify, "keep", func(context.Context) error { return nil })
	if !ok {
		t.Fatal("enqueue after cancel failed")
	}
	if err := j2.Wait(); err != nil {
		t.Fatalf("other kind job: %v", err)
	}
}

// TestCancelRunningUnblocksQueued: cancelling the running job lets the worker
// move on to the next one instead of everyone waiting forever.
func TestCancelRunningUnblocksQueued(t *testing.T) {
	q := New()
	defer q.Stop()

	entered := make(chan struct{})
	_, _, _ = q.Enqueue(KindTranslate, "stuck", func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	next, _, _ := q.Enqueue(KindClassify, "next", func(context.Context) error { return nil })
	<-entered
	// Cancelling only the running kind must let the worker move on to the
	// queued job of the other kind instead of everyone waiting forever.
	if n := q.CancelKind(KindTranslate); n != 1 {
		t.Fatalf("CancelKind = %d, want 1", n)
	}
	if err := next.Wait(); err != nil {
		t.Fatalf("queued job after cancel: %v", err)
	}
}

// TestStopDrainsPending: shutdown finalises queued jobs and interrupts the
// running one instead of hanging.
func TestStopDrainsPending(t *testing.T) {
	q := New()
	running, _, _ := q.Enqueue(KindTranslate, "run", func(ctx context.Context) error {
		<-ctx.Done() // only Stop interrupts it
		return nil
	})
	queued, _, _ := q.Enqueue(KindTranslate, "later", func(context.Context) error { return nil })
	// Wait until "run" is actually executing so "later" is provably pending.
	deadline := time.Now().Add(2 * time.Second)
	for {
		st := q.Status()
		if st.Current != nil && st.Current.Name == "run" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never started: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
	q.Stop()
	if err := queued.Wait(); !errors.Is(err, ErrStopped) {
		t.Fatalf("pending after Stop = %v, want ErrStopped", err)
	}
	if err := running.Wait(); err != nil && !errors.Is(err, ErrCancelled) {
		t.Fatalf("running after Stop = %v, want nil or ErrCancelled", err)
	}
	if _, _, err := q.Enqueue(KindTranslate, "late", func(context.Context) error { return nil }); !errors.Is(err, ErrStopped) {
		t.Fatalf("Enqueue after Stop = %v, want ErrStopped", err)
	}
}

// TestStatusTransition covers the UI polling shape: queued/current counts.
func TestStatusTransition(t *testing.T) {
	q := New()
	defer q.Stop()

	if st := q.Status(); st.Queued != 0 || st.Current != nil {
		t.Fatalf("idle status: %+v", st)
	}
	release := make(chan struct{})
	j, _, _ := q.Enqueue(KindClassify, "audit", func(ctx context.Context) error {
		<-release
		return nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		st := q.Status()
		if st.Current != nil && st.Current.Name == "audit" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never started: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	if err := j.Wait(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		st := q.Status()
		if st.Current == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("current never cleared: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
