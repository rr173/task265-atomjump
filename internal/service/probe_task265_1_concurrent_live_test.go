package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"task265-atomjump/internal/sampling"
)

func TestConcurrentIngestDoesNotCorruptLiveSnapshot(t *testing.T) {
	svc := newTestService(t)
	id, _ := seedJumpClock(t, svc, "cs-race")
	clk, err := svc.Store.Clocks.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errCh := make(chan error, 24)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, err := svc.IngestSample(context.Background(), sampling.IngestInput{
				ClockID: id,
				Seq:     80 + int64(n),
				TakenAt: time.Now().UTC().Add(time.Duration(n) * time.Second),
				FreqHz:  clk.NominalHz,
			})
			if err != nil {
				errCh <- err
			}
		}(i + 1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_, err := svc.AnalyzeClock(id)
		if err != nil {
			errCh <- err
		}
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent ingest/analyze: %v", err)
	}
	wins, err := svc.Store.Windows.ListByClock(id)
	if err != nil {
		t.Fatalf("windows: %v", err)
	}
	if len(wins) == 0 {
		t.Fatal("analyze produced no windows under concurrent ingest")
	}
}
