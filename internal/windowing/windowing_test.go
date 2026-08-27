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

func TestBuildReturnsDistinctWindowsNoAliasing(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/win-alias.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk, err := st.Clocks.Create("cs-alias", "cesium", 9192631770.0, false, 0)
	if err != nil {
		t.Fatalf("create clock: %v", err)
	}
	// 三个不同窗口：每窗 5 个样本，跨越三个相邻窗口宽度，
	// 形成 stable -> stable -> stable，必须返回三个互不相同的窗口。
	base := time.Now().UTC().Truncate(time.Minute)
	width := baseline.DefaultWindowWidth
	type win struct{ start, end time.Time }
	w0 := win{base, base.Add(width)}
	w1 := win{base.Add(width), base.Add(2 * width)}
	w2 := win{base.Add(2 * width), base.Add(3 * width)}
	want := []win{w0, w1, w2}
	for wi, w := range want {
		for i := 0; i < 5; i++ {
			if err := st.Samples.Insert(&model.Sample{
				ClockID: clk.ID, Seq: int64(wi*5 + i + 1),
				TakenAt: w.start.Add(time.Duration(i) * time.Second),
				FreqHz: clk.NominalHz * (1 + 0.1e-9), OffsetPPB: 0.1,
				BaselineRef: "nominal", CreatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatalf("insert sample: %v", err)
			}
		}
	}
	b := NewBuilder(st.Clocks, st.Samples, st.Windows, width)
	wins, err := b.Build(clk.ID)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(wins) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(wins))
	}
	// 回归：所有指针必须指向各自的值，不能被最后一窗覆盖。
	for i, w := range wins {
		if w.StartAt != want[i].start || w.EndAt != want[i].end {
			t.Fatalf("window %d: want %s..%s, got %s..%s",
				i, want[i].start, want[i].end, w.StartAt, w.EndAt)
		}
		if w.ID == 0 {
			t.Fatalf("window %d: expected nonzero ID", i)
		}
	}
	// 所有 ID 必须互异（最后一窗覆盖会导致三者同 ID）。
	seen := map[int64]bool{}
	for _, w := range wins {
		if seen[w.ID] {
			t.Fatalf("duplicate window ID %d across returned windows", w.ID)
		}
		seen[w.ID] = true
	}
}
