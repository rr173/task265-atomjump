package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"task265-atomjump/internal/model"
)

func TestCanceledDetectReleasesWindowRows(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-defer")
	if _, err := svc.AnalyzeClock(id); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.Detect.DetectCtx(ctx, id)
	if !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("want ErrCanceled, got %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.Store.Windows.ListByClock(id)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("list windows after canceled detect: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("window query hung; canceled detect leaked an open Rows")
	}
}
