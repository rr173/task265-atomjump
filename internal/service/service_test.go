package service

import (
	"context"
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
