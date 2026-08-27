package jumpdetect

import (
	"context"
	"errors"
	"testing"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

// TestDetectCancelThenWindowsReadable 回归：一次取消的跳变检测之后，
// 查询该钟的频率窗口必须立即返回，而不是因为结果集未关闭而占死
// SQLite 单连接导致永久阻塞。
func TestDetectCancelThenWindowsReadable(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/cancel.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk, _ := st.Clocks.Create("cs-cancel", "cesium", 9192631770.0, false, 0)
	base := time.Now().UTC().Truncate(time.Minute)
	if _, err := st.Windows.Insert(&model.FreqWindow{
		ClockID: clk.ID, StartAt: base, EndAt: base.Add(time.Minute),
		SampleN: 5, MeanPPB: 0.1, StdDevPPB: 0.01, Status: model.WindowStable,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("insert window: %v", err)
	}

	d := NewDetector(st.Windows, st.Jumps)

	// 1) 取消的检测必须立即返回取消错误。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.DetectCtx(ctx, clk.ID); !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("canceled detect: want ErrCanceled, got %v", err)
	}

	// 2) 随后查询窗口必须立刻拿到结果，而不是永久阻塞。
	done := make(chan []*model.FreqWindow, 1)
	errCh := make(chan error, 1)
	go func() {
		wins, err := st.Windows.ListByClock(clk.ID)
		if err != nil {
			errCh <- err
			return
		}
		done <- wins
	}()
	select {
	case wins := <-done:
		if len(wins) != 1 {
			t.Fatalf("expected 1 window after canceled detect, got %d", len(wins))
		}
	case err := <-errCh:
		t.Fatalf("list windows after cancel: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatalf("list windows blocked after canceled detect: connection leaked by unclosed rows")
	}
}
