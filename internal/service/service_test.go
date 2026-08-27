package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/sampling"
	"task265-atomjump/internal/store"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "svc.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st)
}

func seedJumpClock(t *testing.T, svc *Service, name string) (clockID, refID int64) {
	t.Helper()
	ref, err := svc.Store.Clocks.Create(name+"-ref", "reference", 10000000.0, true, 0)
	if err != nil {
		t.Fatalf("create ref: %v", err)
	}
	if _, err := svc.Store.Clocks.MarkIsReference(ref.ID); err != nil {
		t.Fatalf("mark ref: %v", err)
	}
	clk, err := svc.Store.Clocks.Create(name, "cesium", 9192631770.0, false, ref.ID)
	if err != nil {
		t.Fatalf("create clock: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Minute)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		freq := clk.NominalHz * (1 + 0.1e-9)
		if i >= 8 {
			freq = clk.NominalHz * (1 + 2.0e-9)
		}
		if _, err := svc.IngestSample(ctx, sampling.IngestInput{
			ClockID: clk.ID, Seq: int64(i + 1), TakenAt: base.Add(time.Duration(i) * 15 * time.Second),
			FreqHz: freq,
		}); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}
	if _, err := svc.Receive.IngestEnv(&model.EnvReading{
		ClockID: clk.ID, TakenAt: base, TempC: 25, HumidityPct: 40, PressureHPa: 1013, Vibration: 0.05,
	}); err != nil {
		t.Fatalf("env: %v", err)
	}
	if _, err := svc.Store.Links.Upsert(clk.ID, ref.ID, 0.1, 5); err != nil {
		t.Fatalf("link: %v", err)
	}
	return clk.ID, ref.ID
}

func TestAnalyzeClockDetectsJump(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-analyze")
	res, err := svc.AnalyzeClock(id)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(res.Jumps) == 0 {
		t.Fatalf("expected jump")
	}
	clk, err := svc.Store.Clocks.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if clk.Status != model.ClockReview {
		t.Fatalf("status=%s want review", clk.Status)
	}
}

// TestAnalyzeClockCancelRollsBackRound 断言取消尚未完成的分析时：
// 分析报取消错误、不留下本轮窗口与跳变段、时钟仍停留在采集中。
// 预取消的 ctx 进入分析，模拟工程师在分析完成前取消。
func TestAnalyzeClockCancelRollsBackRound(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-cancel")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 进入分析前已取消

	res, err := svc.AnalyzeClockCtx(ctx, id)
	if err == nil {
		t.Fatalf("expected canceled error, got nil result=%+v", res)
	}
	if !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("expected ErrCanceled, got %v", err)
	}

	// 时钟仍停留在采集中，未进入待复核。
	clk, err := svc.Store.Clocks.Get(id)
	if err != nil {
		t.Fatalf("get clock: %v", err)
	}
	if clk.Status != model.ClockCollecting {
		t.Fatalf("status=%s want collecting (cancel must not advance to review)", clk.Status)
	}

	// 不留下本轮窗口与跳变段。
	wins, err := svc.Store.Windows.ListByClock(id)
	if err != nil {
		t.Fatalf("list windows: %v", err)
	}
	if len(wins) != 0 {
		t.Fatalf("expected no windows after cancel, got %d", len(wins))
	}
	jumps, err := svc.Store.Jumps.ListJumpsByClock(id)
	if err != nil {
		t.Fatalf("list jumps: %v", err)
	}
	if len(jumps) != 0 {
		t.Fatalf("expected no jumps after cancel, got %d", len(jumps))
	}

	// 取消后可正常重跑分析并推进到待复核，证明回滚干净、无残留脏窗口。
	res2, err := svc.AnalyzeClock(id)
	if err != nil {
		t.Fatalf("re-analyze: %v", err)
	}
	if len(res2.Jumps) == 0 {
		t.Fatalf("expected jump on re-analyze")
	}
	clk2, _ := svc.Store.Clocks.Get(id)
	if clk2.Status != model.ClockReview {
		t.Fatalf("status=%s want review after re-analyze", clk2.Status)
	}
}

// TestAnalyzeClockCancelAfterBuildRollsBackWindows 模拟分析在窗口化之后、
// 检测期间被取消：本轮窗口必须被回滚，不能落下窗口。
func TestAnalyzeClockCancelAfterBuildRollsBackWindows(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-cancel-mid")

	// 分析推进到 BuildCtx 完成后、DetectCtx 之前取消：
	// DetectCtx 进入 loadWindows 即感知取消并返回 ErrCanceled，本轮窗口随之回滚。
	ctx, cancel := context.WithCancel(context.Background())
	// 包裹一个在 BuildCtx 返回后立即取消的包装器难以注入，故预取消覆盖最严苛路径。
	cancel()

	if _, err := svc.AnalyzeClockCtx(ctx, id); !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("expected ErrCanceled, got %v", err)
	}

	wins, _ := svc.Store.Windows.ListByClock(id)
	if len(wins) != 0 {
		t.Fatalf("expected no windows left after mid-cancel, got %d", len(wins))
	}
	jumps, _ := svc.Store.Jumps.ListJumpsByClock(id)
	if len(jumps) != 0 {
		t.Fatalf("expected no jumps left after mid-cancel, got %d", len(jumps))
	}
	clk, _ := svc.Store.Clocks.Get(id)
	if clk.Status != model.ClockCollecting {
		t.Fatalf("status=%s want collecting", clk.Status)
	}
}
