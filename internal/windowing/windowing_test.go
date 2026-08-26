package windowing

import (
	"testing"
	"time"

	"task265-atomjump/internal/baseline"
	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

func TestClassifyStableAndJump(t *testing.T) {
	if classify(2, 0.1, 0.01, 0.1, true) != model.WindowGap {
		t.Fatalf("insufficient samples should be gap")
	}
	if classify(5, 0.1, 0.01, 0.1, true) != model.WindowStable {
		t.Fatalf("small deviation should be stable")
	}
	if classify(5, 2.0, 0.01, 0.1, true) != model.WindowJump {
		t.Fatalf("large mean shift should be jump")
	}
	if classify(5, 0.1, baseline.StdDevThresholdPPB+0.1, 0.1, true) != model.WindowJump {
		t.Fatalf("high stddev should be jump")
	}
}

func TestBuildCreatesWindowsFromSamples(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/win.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk, err := st.Clocks.Create("cs-win", "cesium", 9192631770.0, false, 0)
	if err != nil {
		t.Fatalf("create clock: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 6; i++ {
		if err := st.Samples.Insert(&model.Sample{
			ClockID: clk.ID, Seq: int64(i + 1),
			TakenAt: base.Add(time.Duration(i*10) * time.Second),
			FreqHz: clk.NominalHz * (1 + 0.1e-9), OffsetPPB: 0.1,
			BaselineRef: "nominal", CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("insert sample: %v", err)
		}
	}
	b := NewBuilder(st.Clocks, st.Samples, st.Windows, baseline.DefaultWindowWidth)
	wins, err := b.Build(clk.ID)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(wins) == 0 {
		t.Fatalf("expected at least one window")
	}
}
