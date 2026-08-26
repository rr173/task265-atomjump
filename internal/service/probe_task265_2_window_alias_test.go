package service

import (
	"testing"
	"time"
)

func TestWindowsKeepDistinctScanCopies(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-alias")
	res, err := svc.AnalyzeClock(id)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(res.Windows) < 2 {
		t.Fatalf("need at least 2 windows, got %d", len(res.Windows))
	}
	seen := map[int64]time.Time{}
	ids := map[int64]bool{}
	for _, w := range res.Windows {
		if w == nil {
			t.Fatal("nil window")
		}
		if ids[w.ID] {
			t.Fatalf("duplicate window id %d — scan results likely share one struct", w.ID)
		}
		ids[w.ID] = true
		if prev, ok := seen[w.ID]; ok && !prev.Equal(w.StartAt) {
			t.Fatalf("window %d start mutated", w.ID)
		}
		seen[w.ID] = w.StartAt
	}
	first, last := res.Windows[0], res.Windows[len(res.Windows)-1]
	if first.ID == last.ID || first.StartAt.Equal(last.StartAt) {
		t.Fatalf("windows collapsed to one row: id %d/%d start %v/%v", first.ID, last.ID, first.StartAt, last.StartAt)
	}
	stored, err := svc.Store.Windows.ListByClock(id)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(stored) != len(res.Windows) {
		t.Fatalf("in-memory windows %d != stored %d", len(res.Windows), len(stored))
	}
	for i := range stored {
		if stored[i].ID != res.Windows[i].ID || !stored[i].StartAt.Equal(res.Windows[i].StartAt) {
			t.Fatalf("analysis snapshot diverged from store at %d: mem=%+v db=%+v", i, res.Windows[i], stored[i])
		}
	}
}
