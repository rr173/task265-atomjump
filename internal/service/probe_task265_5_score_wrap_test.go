package service

import (
	"context"
	"errors"
	"testing"

	"task265-atomjump/internal/model"
)

func TestCanceledScoreDoesNotCommitPartialCandidates(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-score")
	res, err := svc.AnalyzeClock(id)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(res.Jumps) == 0 {
		t.Fatal("expected jump")
	}
	jumpID := res.Jumps[0].ID
	if err := svc.Store.Jumps.DeleteCandidatesForJump(jumpID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.Score.ScoreCtx(ctx, jumpID)
	if !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("want ErrCanceled, got %v", err)
	}
	cands, err := svc.Store.Jumps.ListCandidates(jumpID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("canceled score left %d candidates", len(cands))
	}
	clk, err := svc.Store.Clocks.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if clk.Status != model.ClockReview {
		t.Fatalf("pre-existing review status lost: %s", clk.Status)
	}
}
