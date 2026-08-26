package jumpdetect

import (
	"testing"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

func TestDetectMergesConsecutiveJumpWindows(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/jump.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk, _ := st.Clocks.Create("cs-jump", "cesium", 9192631770.0, false, 0)
	base := time.Now().UTC().Truncate(time.Minute)
	stable, _ := st.Windows.Insert(&model.FreqWindow{
		ClockID: clk.ID, StartAt: base, EndAt: base.Add(time.Minute),
		SampleN: 5, MeanPPB: 0.1, StdDevPPB: 0.01, Status: model.WindowStable,
		CreatedAt: time.Now().UTC(),
	})
	j1, _ := st.Windows.Insert(&model.FreqWindow{
		ClockID: clk.ID, StartAt: base.Add(time.Minute), EndAt: base.Add(2 * time.Minute),
		SampleN: 5, MeanPPB: 2.0, StdDevPPB: 0.01, Status: model.WindowJump,
		CreatedAt: time.Now().UTC(),
	})
	j2, _ := st.Windows.Insert(&model.FreqWindow{
		ClockID: clk.ID, StartAt: base.Add(2 * time.Minute), EndAt: base.Add(3 * time.Minute),
		SampleN: 5, MeanPPB: 2.1, StdDevPPB: 0.01, Status: model.WindowJump,
		CreatedAt: time.Now().UTC(),
	})
	_ = stable
	d := NewDetector(st.Windows, st.Jumps)
	segments, err := d.Detect(clk.ID)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("expected one merged segment, got %d", len(segments))
	}
	if segments[0].WindowID != j1.ID {
		t.Fatalf("segment should anchor at first jump window")
	}
	if segments[0].EndAt != j2.EndAt {
		t.Fatalf("segment should span through last jump window")
	}
}
