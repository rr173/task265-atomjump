package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"task265-atomjump/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestClockLifecycleAndTransitions(t *testing.T) {
	st := newTestStore(t)
	clk, err := st.Clocks.Create("cs-A", "cesium", 9192631770.0, false, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if clk.Status != model.ClockCollecting {
		t.Fatalf("initial status = %s, want collecting", clk.Status)
	}
	// collecting -> analyzing
	clk, err = st.Clocks.SetStatus(clk.ID, model.ClockAnalyzing)
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	// analyzing -> review
	if _, err = st.Clocks.SetStatus(clk.ID, model.ClockReview); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// review -> confirmed
	if _, err = st.Clocks.SetStatus(clk.ID, model.ClockConfirmed); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// confirmed -> sealed
	if _, err = st.Clocks.SetStatus(clk.ID, model.ClockSealed); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// sealed 后拒绝修改
	if _, err = st.Clocks.SetStatus(clk.ID, model.ClockAnalyzing); err == nil {
		t.Fatalf("expected error for sealed clock mutation")
	}
	// 非法流转 collecting -> confirmed
	clk2, _ := st.Clocks.Create("cs-B", "cesium", 9192631770.0, false, 0)
	if _, err = st.Clocks.SetStatus(clk2.ID, model.ClockConfirmed); err == nil {
		t.Fatalf("expected invalid transition error")
	}
	// 重名唯一
	if _, err = st.Clocks.Create("cs-A", "cesium", 1, false, 0); err == nil {
		t.Fatalf("expected unique name violation")
	}
}

func TestSampleIdempotency(t *testing.T) {
	st := newTestStore(t)
	clk, _ := st.Clocks.Create("cs-C", "cesium", 9192631770.0, false, 0)
	sm := &model.Sample{
		ClockID: clk.ID, Seq: 1, TakenAt: time.Now().UTC(),
		FreqHz: 9192631770.0, OffsetPPB: 0, BaselineRef: "nominal", CreatedAt: time.Now().UTC(),
	}
	if err := st.Samples.Insert(sm); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := st.Samples.Insert(sm); err != model.ErrSampleDuplicate {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

// TestInsertBatchCancelReleasesConnection 复现批量上报取消后事务未回滚导致
// 唯一写连接被占用、后续单条上报卡到 busy_timeout 超时的缺陷。
// 修复后：取消批量返回 ErrCanceled 且事务回滚，紧随其后的单条 Insert 必须立刻成功。
func TestInsertBatchCancelReleasesConnection(t *testing.T) {
	st := newTestStore(t)
	clk, _ := st.Clocks.Create("cs-batch", "cesium", 9192631770.0, false, 0)

	now := time.Now().UTC()
	batch := make([]*model.Sample, 0, 5)
	for i := 0; i < 5; i++ {
		batch = append(batch, &model.Sample{
			ClockID: clk.ID, Seq: int64(i + 1), TakenAt: now.Add(time.Duration(i) * time.Second),
			FreqHz: 9192631770.0, OffsetPPB: 0, BaselineRef: "nominal", CreatedAt: now,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消，触发 InsertBatch 的取消路径

	err := st.Samples.InsertBatch(ctx, batch)
	if !errors.Is(err, model.ErrCanceled) {
		t.Fatalf("batch cancel: expected ErrCanceled, got %v", err)
	}

	// 批量被取消，样本不应落库。
	if n, _ := st.Samples.CountByClock(clk.ID); n != 0 {
		t.Fatalf("after canceled batch, sample count = %d, want 0", n)
	}

	// 关键回归点：单条上报必须立即成功，而非卡到 busy_timeout。
	single := &model.Sample{
		ClockID: clk.ID, Seq: 1, TakenAt: now,
		FreqHz: 9192631770.0, OffsetPPB: 0, BaselineRef: "nominal", CreatedAt: now,
	}
	done := make(chan error, 1)
	go func() { done <- st.Samples.Insert(single) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("single insert after canceled batch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("single insert after canceled batch timed out (connection still held by un-rolled-back tx)")
	}
}

func TestLinkSelfLoopRejected(t *testing.T) {
	st := newTestStore(t)
	clk, _ := st.Clocks.Create("cs-D", "cesium", 9192631770.0, false, 0)
	if _, err := st.Links.Upsert(clk.ID, clk.ID, 0, 0); err != model.ErrReferenceSelfLoop {
		t.Fatalf("expected self loop error, got %v", err)
	}
}

func TestSnapshotPublishSupersedes(t *testing.T) {
	st := newTestStore(t)
	clk, _ := st.Clocks.Create("cs-E", "cesium", 9192631770.0, false, 0)
	s1, err := st.Snapshots.Insert(&model.Snapshot{
		ClockID: clk.ID, Status: model.SnapshotDraft, ReferenceID: 0,
		IsolatedLinks: []int64{1}, Summary: "first", JumpIDs: []int64{1},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("insert s1: %v", err)
	}
	s2, err := st.Snapshots.Insert(&model.Snapshot{
		ClockID: clk.ID, Status: model.SnapshotDraft, ReferenceID: 0,
		IsolatedLinks: []int64{}, Summary: "second", JumpIDs: []int64{},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("insert s2: %v", err)
	}
	// 发布 s1
	p1, err := st.Snapshots.Publish(s1.ID)
	if err != nil {
		t.Fatalf("publish s1: %v", err)
	}
	if p1.Status != model.SnapshotPublished {
		t.Fatalf("s1 status = %s", p1.Status)
	}
	// 再次发布 s1 -> 已发布错误
	if _, err = st.Snapshots.Publish(s1.ID); err != model.ErrSnapshotAlreadyPub {
		t.Fatalf("expected already-published error, got %v", err)
	}
	// 发布 s2 -> s1 被替代
	p2, err := st.Snapshots.Publish(s2.ID)
	if err != nil {
		t.Fatalf("publish s2: %v", err)
	}
	if p2.Status != model.SnapshotPublished {
		t.Fatalf("s2 status = %s", p2.Status)
	}
	back1, _ := st.Snapshots.Get(s1.ID)
	if back1.Status != model.SnapshotSuperseded {
		t.Fatalf("s1 should be superseded, got %s", back1.Status)
	}
	if len(back1.IsolatedLinks) != 1 || back1.IsolatedLinks[0] != 1 {
		t.Fatalf("s1 isolated links not preserved: %v", back1.IsolatedLinks)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {	path := filepath.Join(t.TempDir(), "reopen.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	clk, _ := st.Clocks.Create("cs-F", "cesium", 9192631770.0, false, 0)
	if _, err = st.Clocks.SetStatus(clk.ID, model.ClockAnalyzing); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if _, err = st.Clocks.SetStatus(clk.ID, model.ClockConfirmed); err != nil {
		t.Fatalf("set status: %v", err)
	}
	st.Close()

	// 重开
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	got, err := st2.Clocks.Get(clk.ID)
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if got.Status != model.ClockConfirmed {
		t.Fatalf("status after reopen = %s, want confirmed", got.Status)
	}
	_ = os.Remove(path)
}
