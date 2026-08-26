package service

import (
	"testing"

	"task265-atomjump/internal/snapshot"
)

func TestGetSnapshotKeepsFrozenMagnitudeAfterLiveMutation(t *testing.T) {
	svc := newTestService(t)
	id, refID := seedJumpClock(t, svc, "cs-liveget")
	if _, err := svc.AnalyzeClock(id); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	jumps, err := svc.Store.Jumps.ListJumpsByClock(id)
	if err != nil || len(jumps) == 0 {
		t.Fatalf("jumps: %v", err)
	}
	origMag := jumps[0].MagnitudePPB
	draft, err := svc.Publish.CreateDraft(snapshot.DraftInput{ClockID: id, ReferenceID: refID, Summary: "get-freeze"})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	pub, err := svc.Publish.Publish(draft.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := svc.Store.DB().Exec(`UPDATE jump_segments SET magnitude_ppb=? WHERE id=?`, origMag+11, jumps[0].ID); err != nil {
		t.Fatalf("mutate live: %v", err)
	}
	got, err := svc.Store.Snapshots.Get(pub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Evidence == nil || len(got.Evidence.Jumps) == 0 {
		t.Fatal("get returned no frozen evidence")
	}
	if got.Evidence.Jumps[0].MagnitudePPB != origMag {
		t.Fatalf("get followed live magnitude %v, want frozen %v", got.Evidence.Jumps[0].MagnitudePPB, origMag)
	}
}
