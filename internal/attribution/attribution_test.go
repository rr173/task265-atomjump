package attribution

import (
	"context"
	"errors"
	"testing"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

func TestEnvironmentCoherenceDetectsTemperatureSwing(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/attr.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk, _ := st.Clocks.Create("cs-attr", "cesium", 9192631770.0, false, 0)
	base := time.Now().UTC()
	for i, temp := range []float64{25.0, 26.2} {
		reading := &model.EnvReading{
			ClockID:     clk.ID,
			TakenAt:     base.Add(time.Duration(i) * time.Minute),
			TempC:       temp,
			HumidityPct: 40,
			PressureHPa: 1013,
			Vibration:   0.05,
		}
		if err := st.Envs.Insert(reading); err != nil {
			t.Fatalf("insert env: %v", err)
		}
	}
	sc := NewScorer(st.Envs, st.Links, st.Jumps)
	score, _ := sc.environmentCoherence(clk.ID, base, base.Add(2*time.Minute))
	if score <= 0 {
		t.Fatalf("temperature swing should produce environment coherence score, got %v", score)
	}
}

func TestLinkCoherenceUsesSuspectLinks(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/link.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ref, _ := st.Clocks.Create("ref", "reference", 10000000, true, 0)
	clk, _ := st.Clocks.Create("cs-link", "cesium", 9192631770.0, false, ref.ID)
	lk, _ := st.Links.Upsert(clk.ID, ref.ID, 0.1, 5)
	if _, err := st.Links.SetStatus(lk.ID, model.LinkSuspect); err != nil {
		t.Fatalf("set suspect: %v", err)
	}
	sc := NewScorer(st.Envs, st.Links, st.Jumps)
	score, _, err := sc.linkCoherence(clk.ID, time.Now().UTC(), time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("link coherence: %v", err)
	}
	if score <= 0 {
		t.Fatalf("suspect link should raise link coherence score")
	}
}

// seedJumpForScore 准备一台时钟、一条参考比对链路与一个跳变段，返回其 ID。
func seedJumpForScore(t *testing.T, st *store.Store, name string) int64 {
	t.Helper()
	clk, err := st.Clocks.Create(name, "cesium", 9192631770.0, false, 0)
	if err != nil {
		t.Fatalf("create clock: %v", err)
	}
	ref, err := st.Clocks.Create(name+"-ref", "reference", 10000000, true, 0)
	if err != nil {
		t.Fatalf("create ref: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Minute)

	stable := &model.FreqWindow{
		ClockID:   clk.ID,
		StartAt:   base,
		EndAt:     base.Add(time.Minute),
		SampleN:   5,
		MeanPPB:   0.1,
		StdDevPPB: 0.01,
		Status:    model.WindowStable,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := st.Windows.Insert(stable); err != nil {
		t.Fatalf("insert stable window: %v", err)
	}

	jumpWin := &model.FreqWindow{
		ClockID:   clk.ID,
		StartAt:   base.Add(time.Minute),
		EndAt:     base.Add(2 * time.Minute),
		SampleN:   5,
		MeanPPB:   2.0,
		StdDevPPB: 0.01,
		Status:    model.WindowJump,
		CreatedAt: time.Now().UTC(),
	}
	jw, err := st.Windows.Insert(jumpWin)
	if err != nil {
		t.Fatalf("insert jump window: %v", err)
	}

	seg := &model.JumpSegment{
		ClockID:      clk.ID,
		WindowID:     jw.ID,
		StartAt:      base.Add(time.Minute),
		EndAt:        base.Add(2 * time.Minute),
		MagnitudePPB: 1.9,
		Direction:    "up",
		DetectedAt:   time.Now().UTC(),
		Status:       "open",
	}
	sg, err := st.Jumps.InsertJump(seg)
	if err != nil {
		t.Fatalf("insert jump: %v", err)
	}
	if _, err := st.Links.Upsert(clk.ID, ref.ID, 0.1, 5); err != nil {
		t.Fatalf("upsert link: %v", err)
	}
	return sg.ID
}

// TestScoreCtxCanceledReturnsIdentifiableErrorAndLeavesNoCandidates 验证：
// 取消后评分必须返回可被 errors.Is(err, model.ErrCanceled) 识别的错误，
// 且该跳变段下不能留下半写入候选（否则下次幂等检查会把脏候选当已评分）。
func TestScoreCtxCanceledReturnsIdentifiableErrorAndLeavesNoCandidates(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/cancel.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	jumpID := seedJumpForScore(t, st, "cs-cancel")
	sc := NewScorer(st.Envs, st.Links, st.Jumps)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 进入函数前即已取消

	out, err := sc.ScoreCtx(ctx, jumpID)
	if err == nil {
		t.Fatalf("expected cancellation error, got nil (out=%v)", out)
	}
	if !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("error must wrap model.ErrCanceled so callers can identify cancellation, got: %v", err)
	}
	if out != nil {
		t.Fatalf("canceled score must not return candidates, got %d", len(out))
	}
	left, err := st.Jumps.ListCandidates(jumpID)
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("canceled score must not leave candidates on the jump, got %d", len(left))
	}

	// 取消后用正常 ctx 重新评分：应能成功写入候选，证明前次未留下脏幂等状态。
	out2, err := sc.ScoreCtx(context.Background(), jumpID)
	if err != nil {
		t.Fatalf("rescore after cancel: %v", err)
	}
	if len(out2) == 0 {
		t.Fatalf("rescore after cancel should produce candidates")
	}
}

// TestScoreCtxClearsCandidatesKeepsIdempotency 验证：评分写入候选后清空，
// 再次评分应重新写入全部候选且数量一致，证明清除路径不会破坏幂等检查。
func TestScoreCtxClearsCandidatesKeepsIdempotency(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/clear.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	jumpID := seedJumpForScore(t, st, "cs-clear")
	sc := NewScorer(st.Envs, st.Links, st.Jumps)

	full, err := sc.ScoreCtx(context.Background(), jumpID)
	if err != nil {
		t.Fatalf("first score: %v", err)
	}
	if len(full) == 0 {
		t.Fatalf("first score should produce candidates")
	}
	if err := st.Jumps.DeleteCandidatesForJump(jumpID); err != nil {
		t.Fatalf("clear candidates: %v", err)
	}
	out2, err := sc.ScoreCtx(context.Background(), jumpID)
	if err != nil {
		t.Fatalf("rescore after clear: %v", err)
	}
	if len(out2) != len(full) {
		t.Fatalf("rescore candidate count = %d, want %d (clear must not poison idempotency)", len(out2), len(full))
	}
}
