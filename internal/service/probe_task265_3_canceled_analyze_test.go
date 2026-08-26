package service

import (
	"context"
	"errors"
	"testing"

	"task265-atomjump/internal/model"
)

func TestCanceledAnalyzeRollsBackWindowsAndStatus(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-cancel")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.AnalyzeClockCtx(ctx, id)
	if !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("want ErrCanceled, got %v", err)
	}
	n, err := svc.Store.Windows.CountByClock(id)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("canceled analyze left %d windows", n)
	}
	clk, err := svc.Store.Clocks.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if clk.Status != model.ClockCollecting {
		t.Fatalf("status=%s want collecting", clk.Status)
	}
	jumps, err := svc.Store.Jumps.ListJumpsByClock(id)
	if err != nil {
		t.Fatalf("jumps: %v", err)
	}
	if len(jumps) != 0 {
		t.Fatalf("canceled analyze left %d jumps", len(jumps))
	}
}
