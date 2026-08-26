// Package snapshot 负责诊断快照的发布与替代。
//
// 快照状态机：draft -> published -> superseded。
// 不可变约束：published 快照不允许任何修改；新快照发布时旧 published 快照自动置为 superseded，
// 且快照固定参考钟配置、已隔离链路与跳变幅度，形成不可变证据。
package snapshot

import (
	"context"
	"fmt"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

// Publisher 快照发布器。
type Publisher struct {
	clocks    *store.ClockStore
	snapshots *store.SnapshotStore
	jumps     *store.JumpStore
	links     *store.LinkStore
}

// NewPublisher 构造发布器。
func NewPublisher(c *store.ClockStore, s *store.SnapshotStore, j *store.JumpStore, l *store.LinkStore) *Publisher {
	return &Publisher{clocks: c, snapshots: s, jumps: j, links: l}
}

// DraftInput 创建草稿的输入。
type DraftInput struct {
	ClockID     int64
	ReferenceID int64
	Summary     string
}

// CreateDraft 创建诊断草稿：自动收集该时钟当前已隔离链路与未确认跳变段。
func (p *Publisher) CreateDraft(in DraftInput) (*model.Snapshot, error) {
	clk, err := p.clocks.Get(in.ClockID)
	if err != nil {
		return nil, err
	}
	if clk.Status == model.ClockSealed {
		return nil, model.ErrClockSealed
	}
	var isolated []int64
	links, err := p.links.ListBySubject(in.ClockID)
	if err != nil {
		return nil, err
	}
	for _, lk := range links {
		if lk.Status == model.LinkIsolated {
			isolated = append(isolated, lk.ID)
		}
	}
	var jumpIDs []int64
	jumps, err := p.jumps.ListJumpsByClock(in.ClockID)
	if err != nil {
		return nil, err
	}
	for _, sg := range jumps {
		if sg.Status == "open" {
			jumpIDs = append(jumpIDs, sg.ID)
		}
	}
	ref := in.ReferenceID
	if ref == 0 {
		ref = clk.ReferenceID
	}
	return p.snapshots.Insert(&model.Snapshot{
		ClockID:       in.ClockID,
		Status:        model.SnapshotDraft,
		ReferenceID:   ref,
		IsolatedLinks: isolated,
		Summary:       in.Summary,
		JumpIDs:       jumpIDs,
		CreatedAt:     time.Now().UTC(),
	})
}

func (p *Publisher) freeze(clockID int64) (*model.FrozenEvidence, error) {
	clk, err := p.clocks.Get(clockID)
	if err != nil {
		return nil, err
	}
	links, err := p.links.ListBySubject(clockID)
	if err != nil {
		return nil, err
	}
	var isolated []int64
	for _, lk := range links {
		if lk.Status == model.LinkIsolated {
			isolated = append(isolated, lk.ID)
		}
	}
	jumps, err := p.jumps.ListJumpsByClock(clockID)
	if err != nil {
		return nil, err
	}
	frozen := make([]model.FrozenJump, 0, len(jumps))
	for _, sg := range jumps {
		frozen = append(frozen, model.FrozenJump{
			ID:           sg.ID,
			MagnitudePPB: sg.MagnitudePPB,
			Direction:    sg.Direction,
			Status:       sg.Status,
		})
	}
	return &model.FrozenEvidence{
		ClockStatus:   clk.Status,
		IsolatedLinks: isolated,
		Jumps:         frozen,
	}, nil
}

// Publish 发布草稿：同一事务写入冻结证据并封存时钟，避免 live 表随后被改写导致证据分叉。
func (p *Publisher) Publish(snapshotID int64) (*model.Snapshot, error) {
	return p.PublishCtx(context.Background(), snapshotID)
}

// PublishCtx 带取消的发布。取消发生在事务开始前则不落库。
func (p *Publisher) PublishCtx(ctx context.Context, snapshotID int64) (*model.Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: publish snapshot: %v", model.ErrCanceled, err)
	}
	snap, err := p.snapshots.Get(snapshotID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: publish snapshot: %v", model.ErrCanceled, err)
	}
	return p.snapshots.Publish(snap.ID)
}
