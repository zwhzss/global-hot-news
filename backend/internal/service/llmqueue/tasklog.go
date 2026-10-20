package llmqueue

import (
	"context"
	"sync"
	"time"
)

// Task event types. They are the vocabulary of the /tasks monitor page:
// job-level lifecycle from the queue itself, article-level progress from the
// reporter that translator and classifier code carries on the job context.
const (
	EvtJobQueued    = "jobQueued"
	EvtJobStart     = "jobStart"
	EvtJobDone      = "jobDone"
	EvtJobError     = "jobError"
	EvtJobCancelled = "jobCancelled"
	EvtItemStart    = "itemStart"
	EvtItemDone     = "itemDone"
	EvtItemSkip     = "itemSkip"
	EvtItemError    = "itemError"
	EvtSummary      = "summary"
)

// Event is one line of the task monitor feed. Fields are structured rather
// than pre-formatted text so the frontend can render them through i18n; Title
// and Detail carry raw data (article title, category names, "zh->en").
type Event struct {
	ID        uint64    `json:"id"`
	Time      time.Time `json:"time"`
	JobID     uint64    `json:"jobId"`
	Kind      string    `json:"kind"`
	JobName   string    `json:"jobName"`
	Type      string    `json:"type"`
	ArticleID uint      `json:"articleId,omitempty"`
	Title     string    `json:"title,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Count     int       `json:"count,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// eventCap bounds the in-memory feed. A 500-article job produces ~1000 item
// events; the window keeps the newest slice, which is all the monitor shows.
const eventCap = 500

// Log is a bounded, goroutine-safe ring of task events. The queue owns one;
// events are only kept in memory (never persisted) and survive neither a
// restart nor a "clear" press — which is exactly what a monitor wants.
type Log struct {
	mu     sync.Mutex
	events []Event
	seq    uint64
}

// append assigns the next id and stamps the time when the caller left it zero.
func (l *Log) append(e Event) Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e.ID = l.seq
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if len(l.events) >= eventCap {
		copy(l.events, l.events[1:])
		l.events[len(l.events)-1] = e
	} else {
		l.events = append(l.events, e)
	}
	return e
}

// Recent returns up to limit newest events in ascending id order. limit <= 0
// means "everything currently buffered".
func (l *Log) Recent(limit int) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.events)
	if limit > 0 && limit < n {
		n = limit
	}
	if n == 0 {
		return nil
	}
	out := make([]Event, n)
	copy(out, l.events[len(l.events)-n:])
	return out
}

// clear drops the whole feed ("clear" button on the monitor page).
func (l *Log) clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = nil
}

type reporterKey struct{}

// Reporter is the per-job handle business code uses to publish article-level
// progress to the monitor feed. It is created by the worker, hung on the job
// context, and nil when the caller is not inside a queue job (direct calls,
// tests) — every method is a no-op on nil so call sites need no guards.
type Reporter struct {
	q   *Queue
	job *Job
}

// ReporterFrom returns the reporter bound to ctx, or nil.
func ReporterFrom(ctx context.Context) *Reporter {
	r, _ := ctx.Value(reporterKey{}).(*Reporter)
	return r
}

func (r *Reporter) publish(e Event) {
	if r == nil {
		return
	}
	e.JobID = r.job.info.ID
	e.Kind = r.job.info.Kind
	e.JobName = r.job.info.Name
	r.q.log.append(e)
}

// ItemStart announces the article the job is working on now. It also updates
// JobInfo.Item so the status snapshot can show "current: 分类《…》".
func (r *Reporter) ItemStart(id uint, title string) {
	if r == nil {
		return
	}
	r.q.mu.Lock()
	r.job.info.Item = title
	r.q.mu.Unlock()
	r.publish(Event{Type: EvtItemStart, ArticleID: id, Title: title})
}

// ItemDone reports a finished article. detail is the outcome in raw form:
// category names ("证券、经济") for classification, the direction ("zh->en")
// for translation; empty means the caller could not produce one.
func (r *Reporter) ItemDone(id uint, title, detail string) {
	r.publish(Event{Type: EvtItemDone, ArticleID: id, Title: title, Detail: detail})
}

// ItemSkip reports an article the pipeline deliberately left alone (no
// category matched, nothing to translate).
func (r *Reporter) ItemSkip(id uint, title string) {
	r.publish(Event{Type: EvtItemSkip, ArticleID: id, Title: title})
}

// ItemError reports a failed article; the job itself keeps going.
func (r *Reporter) ItemError(id uint, title string, err error) {
	if r == nil || err == nil {
		return
	}
	r.publish(Event{Type: EvtItemError, ArticleID: id, Title: title, Error: err.Error()})
}

// Summary reports how many articles the job actually changed.
func (r *Reporter) Summary(n int) {
	r.publish(Event{Type: EvtSummary, Count: n})
}

// SetTotal declares how many items the job will process, arming the
// "12/500" progress display on the monitor page.
func (r *Reporter) SetTotal(total int) {
	if r == nil {
		return
	}
	r.q.mu.Lock()
	r.job.info.Total = total
	r.q.mu.Unlock()
}

// SetProgress updates the finished/total counters on the status snapshot.
// Safe to call from parallel item goroutines.
func (r *Reporter) SetProgress(done, total int) {
	if r == nil {
		return
	}
	r.q.mu.Lock()
	r.job.info.Done = done
	if total > 0 {
		r.job.info.Total = total
	}
	r.q.mu.Unlock()
}
