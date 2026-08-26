package service

import (
	"context"
	"testing"

	"task265-atomjump/internal/model"
)

func TestRecoverResumesAnalyzingClock(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-rec")
	if _, err := svc.Store.Clocks.SetStatus(id, model.ClockAnalyzing); err != nil {
		t.Fatalf("set analyzing: %v", err)
	}
	fresh := New(svc.Store)
	if err := fresh.Recover(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}
	clk, err := fresh.Store.Clocks.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if clk.Status != model.ClockReview && clk.Status != model.ClockConfirmed {
		t.Fatalf("after recover status=%s, want review or confirmed", clk.Status)
	}
	n, err := fresh.Store.Windows.CountByClock(id)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n == 0 {
		t.Fatal("recover did not rebuild windows for analyzing clock")
	}
	jumps, err := fresh.Store.Jumps.ListJumpsByClock(id)
	if err != nil {
		t.Fatalf("jumps: %v", err)
	}
	if len(jumps) == 0 {
		t.Fatal("recover did not detect jumps")
	}
}
