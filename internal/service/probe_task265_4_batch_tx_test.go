package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/sampling"
)

func TestCanceledBatchDoesNotPinSQLiteConnection(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-batch")
	clk, err := svc.Store.Clocks.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	base := time.Now().UTC().Add(time.Hour)
	ins := make([]sampling.IngestInput, 0, 8)
	for i := 0; i < 8; i++ {
		ins = append(ins, sampling.IngestInput{
			ClockID: id, Seq: int64(300 + i), TakenAt: base.Add(time.Duration(i) * time.Second),
			FreqHz: clk.NominalHz,
		})
	}
	if _, err := svc.IngestMany(ctx, ins); !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("want ErrCanceled, got %v", err)
	}
	n, err := svc.Store.Samples.CountByClock(id)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 12 {
		t.Fatalf("canceled batch persisted samples, have %d want 12", n)
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.IngestSample(context.Background(), sampling.IngestInput{
			ClockID: id, Seq: 400, TakenAt: base.Add(2 * time.Hour), FreqHz: clk.NominalHz,
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("follow-up ingest after canceled batch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow-up ingest hung; canceled batch leaked a SQLite transaction")
	}
}
