package llmqueue

import (
	"context"
	"log"
	"sync"
)

// ---- bounded parallelism ----------------------------------------------------

// RunParallel runs fn for the indexes [0, count) with at most conc invocations
// in flight at once. It is how a job fans an article list out to a model that
// accepts several requests at a time (llm_max_concurrency) while keeping the
// total bounded.
//
// Semantics:
//   - Launching stops as soon as ctx is done; in-flight calls are waited for.
//     fn must honour ctx so cancelling the job aborts the model requests too.
//   - conc < 1 is treated as 1 (the old one-article-at-a-time behaviour).
//   - A panic inside one item is recovered and logged: the job-level recover
//     in runJob does not cover goroutines started here, and one bad article
//     must not take the whole process down.
func RunParallel(ctx context.Context, conc, count int, fn func(i int)) error {
	if count <= 0 {
		return ctx.Err()
	}
	if conc < 1 {
		conc = 1
	}
	if conc > count {
		conc = count
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		// Check before parking: a cancel that already happened must stop the
		// launch loop even if a slot just became free.
		if ctx.Err() != nil {
			wg.Wait()
			return ctx.Err()
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				<-sem
				if r := recover(); r != nil {
					log.Printf("llmqueue: parallel item %d panicked: %v", i, r)
				}
			}()
			fn(i)
		}(i)
	}
	wg.Wait()
	return ctx.Err()
}

// ---- shared inference gate --------------------------------------------------

// The gate bounds how many inference requests are on the wire at once across
// every caller (translation, classification, connection probes). Its capacity
// is llm_max_concurrency, refreshed through SetGateMax whenever a caller reads
// that setting; the default of 1 reproduces the old localGate behaviour of
// serialising all model traffic.
var (
	gateMu   sync.Mutex
	gateMax  = 1
	gateCur  int
	gateWait []chan struct{} // FIFO of parked acquirers
)

// SetGateMax updates how many requests may be in flight at once. Values below
// 1 clamp to 1. Raising the limit wakes parked acquirers immediately.
func SetGateMax(n int) {
	gateMu.Lock()
	if n < 1 {
		n = 1
	}
	gateMax = n
	gateGrantLocked()
	gateMu.Unlock()
}

// gateGrantLocked hands a slot to the next waiter while capacity allows.
// Caller must hold gateMu.
func gateGrantLocked() {
	for gateCur < gateMax && len(gateWait) > 0 {
		ch := gateWait[0]
		gateWait = gateWait[1:]
		gateCur++
		close(ch)
	}
}

// AcquireGate blocks until a request slot is free or ctx is done, and returns
// the release function. Callers must release exactly once after the request
// finishes (typically defer).
func AcquireGate(ctx context.Context) (func(), error) {
	gateMu.Lock()
	if gateCur < gateMax {
		gateCur++
		gateMu.Unlock()
		return func() { releaseGate() }, nil
	}
	ch := make(chan struct{})
	gateWait = append(gateWait, ch)
	gateMu.Unlock()

	select {
	case <-ch:
		return func() { releaseGate() }, nil
	case <-ctx.Done():
		gateMu.Lock()
		// Still parked: withdraw. Already granted (channel closed by a
		// releaser racing our cancel): give the slot back instead.
		for i, w := range gateWait {
			if w == ch {
				gateWait = append(gateWait[:i], gateWait[i+1:]...)
				gateMu.Unlock()
				return nil, ctx.Err()
			}
		}
		gateCur--
		gateGrantLocked()
		gateMu.Unlock()
		return nil, ctx.Err()
	}
}

func releaseGate() {
	gateMu.Lock()
	gateCur--
	gateGrantLocked()
	gateMu.Unlock()
}
