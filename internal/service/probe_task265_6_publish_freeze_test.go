package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/sampling"
	"task265-atomjump/internal/snapshot"
)

func TestPublishFreezesEvidenceAndSealsClock(t *testing.T) {
	svc := newTestService(t)
	id, refID := seedJumpClock(t, svc, "cs-pub")
	if _, err := svc.AnalyzeClock(id); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	links, err := svc.Store.Links.ListBySubject(id)
	if err != nil || len(links) == 0 {
		t.Fatalf("links: %v", err)
	}
	if _, err := svc.LinkAction(links[0].ID, true); err != nil {
		t.Fatalf("isolate: %v", err)
	}
	jumps, err := svc.Store.Jumps.ListJumpsByClock(id)
	if err != nil || len(jumps) == 0 {
		t.Fatalf("jumps: %v", err)
	}
	origMag := jumps[0].MagnitudePPB
	draft, err := svc.Publish.CreateDraft(snapshot.DraftInput{ClockID: id, ReferenceID: refID, Summary: "freeze"})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	pub, err := svc.Publish.Publish(draft.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.Status != model.SnapshotPublished {
		t.Fatalf("status=%s", pub.Status)
	}
	if pub.Evidence == nil || len(pub.Evidence.Jumps) == 0 {
		t.Fatal("published snapshot missing frozen jump evidence")
	}
	if pub.Evidence.Jumps[0].MagnitudePPB != origMag {
		t.Fatalf("frozen mag=%v want %v", pub.Evidence.Jumps[0].MagnitudePPB, origMag)
	}
	if _, err := svc.Store.DB().Exec(`UPDATE jump_segments SET magnitude_ppb=? WHERE id=?`, origMag+9, jumps[0].ID); err != nil {
		t.Fatalf("mutate live: %v", err)
	}
	got, err := svc.Store.Snapshots.Get(pub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Evidence == nil || got.Evidence.Jumps[0].MagnitudePPB != origMag {
		t.Fatal("published evidence followed live jump table")
	}
	if _, err := svc.IngestSample(context.Background(), sampling.IngestInput{
		ClockID: id, Seq: 99, TakenAt: time.Now().UTC(), FreqHz: 9192631770.0,
	}); !errors.Is(err, model.ErrClockSealed) {
		t.Fatalf("want sealed reject after publish, got %v", err)
	}
}
