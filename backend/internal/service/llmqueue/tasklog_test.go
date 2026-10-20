package llmqueue

import (
	"context"
	"errors"
	"testing"
)

// TestReporterNilSafe: code outside a queue job (direct calls, tests) gets a
// nil reporter and must be able to call every method without a guard.
func TestReporterNilSafe(t *testing.T) {
	if r := ReporterFrom(context.Background()); r != nil {
		t.Fatalf("ReporterFrom(plain) = %v, want nil", r)
	}
	var r *Reporter
	r.ItemStart(1, "t")
	r.ItemDone(1, "t", "d")
	r.ItemSkip(1, "t")
	r.ItemError(1, "t", nil)
	r.Summary(3)
}

// TestTaskLogJobSequence proves the feed the /tasks page renders: queue and
// lifecycle events frame the reporter's article events, ids ascend, and the
// job metadata (kind/name/id) is stamped on every line.
func TestTaskLogJobSequence(t *testing.T) {
	q := New()
	defer q.Stop()

	j, ok, err := q.Enqueue(KindClassify, "audit-unclassified", func(ctx context.Context) error {
		rep := ReporterFrom(ctx)
		if rep == nil {
			t.Fatal("no reporter on job context")
		}
		rep.ItemStart(42, "Article A")
		rep.ItemDone(42, "Article A", "证券、经济")
		rep.ItemSkip(43, "Article B")
		rep.ItemError(44, "Article C", context.DeadlineExceeded)
		rep.Summary(1)
		return nil
	})
	if !ok || err != nil {
		t.Fatalf("enqueue: ok=%v err=%v", ok, err)
	}
	if err := j.Wait(); err != nil {
		t.Fatal(err)
	}

	want := []string{EvtJobQueued, EvtJobStart, EvtItemStart, EvtItemDone,
		EvtItemSkip, EvtItemError, EvtSummary, EvtJobDone}
	events := q.Events(0)
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(want), events)
	}
	var prevID uint64
	for i, e := range events {
		if e.Type != want[i] {
			t.Errorf("event %d type = %q, want %q", i, e.Type, want[i])
		}
		if e.ID <= prevID {
			t.Errorf("event %d id = %d, want > %d", i, e.ID, prevID)
		}
		prevID = e.ID
		if e.JobID != j.Info().ID || e.Kind != KindClassify || e.JobName != "audit-unclassified" {
			t.Errorf("event %d missing job metadata: %+v", i, e)
		}
	}
	if events[2].ArticleID != 42 || events[2].Title != "Article A" {
		t.Errorf("itemStart = %+v", events[2])
	}
	if events[3].Detail != "证券、经济" {
		t.Errorf("itemDone detail = %q", events[3].Detail)
	}
	if events[6].Count != 1 {
		t.Errorf("summary count = %d, want 1", events[6].Count)
	}
}

// TestReporterUpdatesCurrentItem: ItemStart feeds JobInfo.Item so the status
// snapshot can show which article the running job is on, and SetProgress
// feeds the done/total counters the monitor card renders as "12/500".
func TestReporterUpdatesCurrentItem(t *testing.T) {
	q := New()
	defer q.Stop()

	release := make(chan struct{})
	entered := make(chan struct{})
	_, _, _ = q.Enqueue(KindTranslate, "batch", func(ctx context.Context) error {
		rep := ReporterFrom(ctx)
		rep.ItemStart(7, "Breaking News")
		rep.SetTotal(10)
		rep.SetProgress(3, 10)
		close(entered)
		<-release
		return nil
	})
	<-entered
	st := q.Status()
	if st.Current == nil || st.Current.Item != "Breaking News" {
		t.Fatalf("status current item = %+v, want Breaking News", st.Current)
	}
	if st.Current.Done != 3 || st.Current.Total != 10 {
		t.Fatalf("status progress = %d/%d, want 3/10", st.Current.Done, st.Current.Total)
	}
	close(release)
}

// TestLogRingEvictsOldest: the feed is bounded; with cap+5 entries the oldest
// five are gone and the newest id is still returned.
func TestLogRingEvictsOldest(t *testing.T) {
	var l Log
	for i := 0; i < eventCap+5; i++ {
		l.append(Event{Type: EvtItemDone})
	}
	events := l.Recent(0)
	if len(events) != eventCap {
		t.Fatalf("len = %d, want %d", len(events), eventCap)
	}
	if events[len(events)-1].ID != eventCap+5 {
		t.Errorf("newest id = %d, want %d", events[len(events)-1].ID, eventCap+5)
	}
	if events[0].ID != 6 {
		t.Errorf("oldest id = %d, want 6 (1-5 evicted)", events[0].ID)
	}
}

// TestLogRecentLimit: ascending order and the limit window.
func TestLogRecentLimit(t *testing.T) {
	var l Log
	for i := 0; i < 10; i++ {
		l.append(Event{Type: EvtItemDone})
	}
	got := l.Recent(3)
	if len(got) != 3 || got[0].ID != 8 || got[2].ID != 10 {
		t.Fatalf("Recent(3) = %+v", got)
	}
	l.clear()
	if got := l.Recent(0); got != nil {
		t.Fatalf("after clear = %+v, want nil", got)
	}
}

// TestCancelledJobEvent: a cancelled run ends with jobCancelled, not jobError.
func TestCancelledJobEvent(t *testing.T) {
	q := New()
	defer q.Stop()

	entered := make(chan struct{})
	j, _, _ := q.Enqueue(KindTranslate, "batch", func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	<-entered
	q.CancelKind(KindTranslate)
	if err := j.Wait(); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Wait = %v, want ErrCancelled", err)
	}
	events := q.Events(0)
	last := events[len(events)-1]
	if last.Type != EvtJobCancelled {
		t.Fatalf("last event type = %q, want %q: %+v", last.Type, EvtJobCancelled, last)
	}
}
