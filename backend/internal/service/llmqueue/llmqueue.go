// Package llmqueue serialises every task that talks to the language model.
//
// The local model behind Settings only serves one request at a time, so
// starting a goroutine per article (or letting translation and classification
// compete) only produces a pile of waiters that cannot be steered. Instead,
// callers enqueue jobs here: a single worker goroutine runs them one at a time
// in FIFO order, and every job executes under a context that can be cancelled —
// from a type-wide "stop translating" button, by job id, or globally at
// shutdown. Cancelling a job aborts its in-flight HTTP request immediately and
// unblocks anyone queued behind it.
package llmqueue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Job kinds. They map to the cancel buttons in the settings page.
const (
	KindTranslate = "translate"
	KindClassify  = "classify"
)

var (
	// ErrCancelled is reported by Job.Wait when the job was cancelled —
	// either while queued or mid-flight — before it could finish.
	ErrCancelled = errors.New("job cancelled")
	// ErrStopped is reported when the queue was shut down.
	ErrStopped = errors.New("llm queue stopped")
)

// JobInfo describes one queued or running job. It is the JSON view the UI
// polls to show "queued N · current: translate".
type JobInfo struct {
	ID        uint64    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	StartedAt time.Time `json:"startedAt"`       // zero while the job is still queued
	Item      string    `json:"item,omitempty"`  // title of the article being worked on right now
	Done      int       `json:"done,omitempty"`  // items finished so far (monitor progress)
	Total     int       `json:"total,omitempty"` // items the job announced it will process
}

// Status is a snapshot of the queue for the settings page.
type Status struct {
	Queued  int      `json:"queued"`
	Current *JobInfo `json:"current"`
}

// Job is a handle to one enqueued unit of LLM work.
type Job struct {
	info     JobInfo
	fn       func(ctx context.Context) error
	cancel   context.CancelFunc // set while the job runs
	finished chan struct{}      // closed when the job is done or dropped
	err      error              // written before finished is closed
}

// Info returns the job's descriptor.
func (j *Job) Info() JobInfo { return j.info }

// Wait blocks until the job finishes (fn returned) or is cancelled/dropped,
// and reports the outcome. ErrCancelled / ErrStopped mean it did not run to
// completion.
func (j *Job) Wait() error {
	<-j.finished
	return j.err
}

// Queue is the single-worker FIFO executor for LLM jobs.
type Queue struct {
	mu         sync.Mutex
	pending    []*Job
	current    *Job
	active     map[string]bool // "kind/name" -> queued or running (dedup guard)
	nextID     uint64
	stopped    bool
	rootCtx    context.Context
	rootCancel context.CancelFunc
	wake       chan struct{} // buffered 1: worker wakeup
	stopCh     chan struct{} // closed by Stop
	wg         sync.WaitGroup
	log        Log // task monitor feed (own mutex, safe under q.mu)
}

// New creates a queue and starts its worker goroutine.
func New() *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		active:     make(map[string]bool),
		rootCtx:    ctx,
		rootCancel: cancel,
		wake:       make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
	}
	q.wg.Add(1)
	go q.worker()
	return q
}

// Enqueue appends a job. It returns ok=false (without an error) when a job
// with the same kind+name is already queued or running — that is the
// double-click guard for the settings buttons. fn runs under a cancellable
// context owned by the queue; it must honour ctx to make cancel effective.
func (q *Queue) Enqueue(kind, name string, fn func(ctx context.Context) error) (*Job, bool, error) {
	key := kind + "/" + name
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return nil, false, ErrStopped
	}
	if q.active[key] {
		return nil, false, nil
	}
	q.nextID++
	j := &Job{
		info:     JobInfo{ID: q.nextID, Kind: kind, Name: name},
		fn:       fn,
		finished: make(chan struct{}),
	}
	q.active[key] = true
	q.pending = append(q.pending, j)
	q.log.append(Event{JobID: j.info.ID, Kind: kind, JobName: name, Type: EvtJobQueued})
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return j, true, nil
}

// Events returns up to limit newest monitor events in ascending id order
// (limit <= 0: everything buffered).
func (q *Queue) Events(limit int) []Event {
	return q.log.Recent(limit)
}

// ClearEvents empties the monitor feed.
func (q *Queue) ClearEvents() {
	q.log.clear()
}

// Cancel drops a queued job or interrupts the running one. It reports whether
// the id was known to the queue.
func (q *Queue) Cancel(id uint64) bool {
	q.mu.Lock()
	if q.current != nil && q.current.info.ID == id {
		c := q.current.cancel
		q.mu.Unlock()
		if c != nil {
			c()
		}
		return true
	}
	for i, j := range q.pending {
		if j.info.ID == id {
			q.pending = append(q.pending[:i], q.pending[i+1:]...)
			q.dropLocked(j)
			q.mu.Unlock()
			return true
		}
	}
	q.mu.Unlock()
	return false
}

// CancelKind drops every queued job of the kind and interrupts the running one
// if it matches. It returns how many jobs were affected — that is the
// "取消翻译" / "取消分类" button.
func (q *Queue) CancelKind(kind string) int {
	return q.cancelWhere(func(j *Job) bool { return j.info.Kind == kind })
}

// CancelAll cancels the running job and everything queued — the global stop.
func (q *Queue) CancelAll() int {
	return q.cancelWhere(func(*Job) bool { return true })
}

func (q *Queue) cancelWhere(match func(*Job) bool) int {
	q.mu.Lock()
	n := 0
	if q.current != nil && match(q.current) {
		if q.current.cancel != nil {
			q.current.cancel()
		}
		n++
	}
	kept := q.pending[:0]
	for _, j := range q.pending {
		if match(j) {
			q.dropLocked(j)
			n++
			continue
		}
		kept = append(kept, j)
	}
	q.pending = kept
	q.mu.Unlock()
	if n > 0 {
		// Make sure the worker notices any hole we just punched.
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}
	return n
}

// Status reports queue depth and the currently running job.
func (q *Queue) Status() Status {
	q.mu.Lock()
	defer q.mu.Unlock()
	st := Status{Queued: len(q.pending)}
	if q.current != nil {
		info := q.current.info
		st.Current = &info
	}
	return st
}

// Stop cancels everything and waits for the worker to exit. Pending jobs get
// ErrStopped; the running one is interrupted through its context.
func (q *Queue) Stop() {
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return
	}
	q.stopped = true
	for _, j := range q.pending {
		q.terminalLocked(j, ErrStopped)
		q.finalizeLocked(j, ErrStopped)
	}
	q.pending = nil
	q.mu.Unlock()

	close(q.stopCh)
	q.rootCancel()
	q.wg.Wait()
}

// dropLocked finalises a job that will never run (removed from pending).
// Caller must hold q.mu.
func (q *Queue) dropLocked(j *Job) {
	delete(q.active, j.info.Kind+"/"+j.info.Name)
	q.terminalLocked(j, ErrCancelled)
	q.finalizeLocked(j, ErrCancelled)
}

// terminalLocked appends the job's final monitor event (done / cancelled /
// error). Caller must hold q.mu; it runs before finalizeLocked so anyone
// woken by Job.Wait can already observe the event.
func (q *Queue) terminalLocked(j *Job, err error) {
	terminal := Event{JobID: j.info.ID, Kind: j.info.Kind, JobName: j.info.Name, Type: EvtJobDone}
	switch {
	case err == nil:
	case errors.Is(err, ErrCancelled), errors.Is(err, ErrStopped):
		terminal.Type = EvtJobCancelled
	default:
		terminal.Type = EvtJobError
		terminal.Error = err.Error()
	}
	q.log.append(terminal)
}

// finalizeLocked records the outcome and wakes anyone blocked in Job.Wait.
// Caller must hold q.mu.
func (q *Queue) finalizeLocked(j *Job, err error) {
	j.err = err
	close(j.finished)
}

// worker runs jobs one at a time. There is exactly one of these, which is
// what guarantees the model is never asked two things at once.
func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		if q.stopped {
			q.mu.Unlock()
			return
		}
		if len(q.pending) == 0 {
			q.mu.Unlock()
			select {
			case <-q.wake:
			case <-q.stopCh:
			}
			continue
		}
		j := q.pending[0]
		q.pending = q.pending[1:]
		j.info.StartedAt = time.Now()
		j.info.Item = ""
		j.info.Done = 0
		j.info.Total = 0
		ctx, cancel := context.WithCancel(q.rootCtx)
		j.cancel = cancel
		ctx = context.WithValue(ctx, reporterKey{}, &Reporter{q: q, job: j})
		q.current = j
		q.log.append(Event{JobID: j.info.ID, Kind: j.info.Kind, JobName: j.info.Name, Type: EvtJobStart})
		q.mu.Unlock()

		err := runJob(ctx, j)
		cancel()

		q.mu.Lock()
		q.current = nil
		j.info.Item = ""
		delete(q.active, j.info.Kind+"/"+j.info.Name)
		q.terminalLocked(j, err)
		q.finalizeLocked(j, err)
		q.mu.Unlock()

		// Chain to the next job without waiting for another Enqueue.
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}
}

// runJob executes fn with panic containment: a crash inside one job must not
// take the worker down with it.
func runJob(ctx context.Context, j *Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("job %s (%s) panicked: %v", j.info.Kind, j.info.Name, r)
		}
	}()
	if err := j.fn(ctx); err != nil {
		if ctx.Err() != nil {
			// The job was cancelled; its own error is usually a wrapped
			// context error that means nothing to the UI.
			return ErrCancelled
		}
		return err
	}
	if ctx.Err() != nil {
		return ErrCancelled
	}
	return nil
}
