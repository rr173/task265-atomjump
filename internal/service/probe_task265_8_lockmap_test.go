package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"task265-atomjump/internal/sampling"
)

func TestConcurrentClocksDoNotRaceLockTable(t *testing.T) {
	svc := newTestService(t)
	type clock struct {
		id       int64
		nominal  float64
	}
	clocks := make([]clock, 0, 20)
	for i := 0; i < 20; i++ {
		clk, err := svc.Store.Clocks.Create(fmt.Sprintf("cs-lk-%d", i), "cesium", 9192631770.0, false, 0)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		clocks = append(clocks, clock{id: clk.ID, nominal: clk.NominalHz})
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errCh := make(chan error, 40)
	for _, c := range clocks {
		wg.Add(1)
		go func(clockID int64, nominal float64) {
			defer wg.Done()
			<-start
			_, err := svc.IngestSample(context.Background(), sampling.IngestInput{
				ClockID: clockID,
				Seq:     1,
				TakenAt: time.Now().UTC(),
				FreqHz:  nominal,
			})
			if err != nil {
				errCh <- err
			}
		}(c.id, c.nominal)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent ingest across clocks: %v", err)
	}
}
