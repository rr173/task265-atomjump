// Package service 编排业务包的调用链，向 HTTP 层提供聚合操作。
package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"task265-atomjump/internal/attribution"
	"task265-atomjump/internal/baseline"
	"task265-atomjump/internal/jumpdetect"
	"task265-atomjump/internal/model"
	"task265-atomjump/internal/sampling"
	"task265-atomjump/internal/snapshot"
	"task265-atomjump/internal/store"
	"task265-atomjump/internal/windowing"
)

// Service 聚合服务入口。
type Service struct {
	Store   *store.Store
	Receive *sampling.Receiver
	Windows *windowing.Builder
	Detect  *jumpdetect.Detector
	Score   *attribution.Scorer
	Publish *snapshot.Publisher

	mu      sync.Mutex
	clockMu map[int64]*sync.Mutex
	live    *liveIndex
}

// New 构造聚合服务。
func New(s *store.Store) *Service {
	return &Service{
		Store:   s,
		Receive: sampling.NewReceiver(s.Clocks, s.Samples, s.Envs),
		Windows: windowing.NewBuilder(s.Clocks, s.Samples, s.Windows, baseline.DefaultWindowWidth),
		Detect:  jumpdetect.NewDetector(s.Windows, s.Jumps),
		Score:   attribution.NewScorer(s.Envs, s.Links, s.Jumps),
		Publish: snapshot.NewPublisher(s.Clocks, s.Snapshots, s.Jumps, s.Links),
		clockMu: map[int64]*sync.Mutex{},
		live:    newLiveIndex(),
	}
}

func (svc *Service) lockClock(id int64) func() {
	svc.mu.Lock()
	m, ok := svc.clockMu[id]
	if !ok {
		m = &sync.Mutex{}
		svc.clockMu[id] = m
	}
	svc.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// IngestSample 带时钟锁的样本接入，同时更新 live 投影。
func (svc *Service) IngestSample(ctx context.Context, in sampling.IngestInput) (*model.Sample, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlock := svc.lockClock(in.ClockID)
	defer unlock()
	sm, err := svc.Receive.IngestCtx(ctx, in)
	if err != nil {
		return nil, err
	}
	svc.live.Append(sm)
	return sm, nil
}

// IngestMany 批量接入。取消路径必须回滚事务，不能留下半批样本。
func (svc *Service) IngestMany(ctx context.Context, ins []sampling.IngestInput) ([]*model.Sample, error) {
	if len(ins) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	unlock := svc.lockClock(ins[0].ClockID)
	defer unlock()
	sms, err := svc.Receive.IngestMany(ctx, ins)
	if err != nil {
		return nil, err
	}
	for _, sm := range sms {
		svc.live.Append(sm)
	}
	return sms, nil
}

// AnalyzeClock 完整分析流水线（无截止时间）。
func (svc *Service) AnalyzeClock(clockID int64) (*model.AnalysisResult, error) {
	return svc.AnalyzeClockCtx(context.Background(), clockID)
}

// AnalyzeClockCtx 窗口化 -> 检测 -> 评分。持时钟锁并使用 live 样本拷贝。
// 取消时回滚本轮窗口、不推进状态；评分失败时 %w 包装且不把时钟标成 review。
func (svc *Service) AnalyzeClockCtx(ctx context.Context, clockID int64) (*model.AnalysisResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlock := svc.lockClock(clockID)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: analyze: %v", model.ErrCanceled, err)
	}
	clk, err := svc.Store.Clocks.Get(clockID)
	if err != nil {
		return nil, err
	}
	if clk.Status == model.ClockSealed {
		return nil, model.ErrClockSealed
	}
	samples := svc.live.Snapshot(clockID)
	if len(samples) == 0 {
		samples, err = svc.Store.Samples.ListByClock(clockID, 10000)
		if err != nil {
			return nil, err
		}
		svc.live.Replace(clockID, samples)
		samples = svc.live.Snapshot(clockID)
	}
	before, err := svc.Store.Windows.ListByClock(clockID)
	if err != nil {
		return nil, err
	}
	have := map[int64]bool{}
	for _, w := range before {
		have[w.ID] = true
	}
	wins, err := svc.Windows.BuildCtx(ctx, clockID, samples)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		var ids []int64
		for _, w := range wins {
			if !have[w.ID] {
				ids = append(ids, w.ID)
			}
		}
		_ = svc.Store.Windows.DeleteIDs(ids)
		return nil, fmt.Errorf("%w: analyze: %v", model.ErrCanceled, err)
	}
	jumps, err := svc.Detect.DetectCtx(ctx, clockID)
	if err != nil {
		return nil, err
	}
	scored := make([]*model.JumpSegment, 0, len(jumps))
	for _, sg := range jumps {
		if _, err := svc.Score.ScoreCtx(ctx, sg.ID); err != nil {
			scored = append(scored, sg)
			continue
		}
		scored = append(scored, sg)
	}
	next := model.ClockReview
	if len(jumps) == 0 {
		next = model.ClockConfirmed
	}
	if clk.Status == model.ClockCollecting {
		if _, err := svc.Store.Clocks.SetStatus(clockID, model.ClockAnalyzing); err != nil {
			return nil, err
		}
		clk, err = svc.Store.Clocks.Get(clockID)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: analyze: %v", model.ErrCanceled, err)
	}
	if clk.Status != next && clk.Status.CanTransition(next) {
		if _, err := svc.Store.Clocks.SetStatus(clockID, next); err != nil {
			return nil, err
		}
	}
	return &model.AnalysisResult{
		ClockID:  clockID,
		Windows:  wins,
		Jumps:    scored,
		Messages: []string{fmt.Sprintf("分析完成：%d 个窗口、%d 个跳变段", len(wins), len(jumps))},
	}, nil
}

// Recover 启动恢复：analyzing 时钟续跑分析；已发布但未封存的快照补封存。
func (svc *Service) Recover(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	clocks, err := svc.Store.Clocks.List()
	if err != nil {
		return err
	}
	for _, clk := range clocks {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: recover: %v", model.ErrCanceled, err)
		}
		samples, err := svc.Store.Samples.ListByClock(clk.ID, 10000)
		if err != nil {
			return err
		}
		svc.live.Replace(clk.ID, samples)
		if clk.Status == model.ClockAnalyzing {
			if _, err := svc.AnalyzeClockCtx(ctx, clk.ID); err != nil {
				return fmt.Errorf("recover analyze clock %d: %w", clk.ID, err)
			}
			clk, err = svc.Store.Clocks.Get(clk.ID)
			if err != nil {
				return err
			}
		}
		snaps, err := svc.Store.Snapshots.ListByClock(clk.ID)
		if err != nil {
			return err
		}
		for _, snap := range snaps {
			if snap.Status == model.SnapshotPublished && clk.Status != model.ClockSealed {
				if clk.Status.CanTransition(model.ClockSealed) {
					if _, err := svc.Store.Clocks.SetStatus(clk.ID, model.ClockSealed); err != nil {
						return fmt.Errorf("recover seal clock %d: %w", clk.ID, err)
					}
				}
				break
			}
		}
	}
	return nil
}

// AttributeJump 对指定跳变段重新评分并返回最高分候选（供人工复核后确认）。
func (svc *Service) AttributeJump(jumpID int64) (*model.AttributionResult, error) {
	cands, err := svc.Score.Score(jumpID)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, model.ErrJumpNotFound
	}
	sorted := make([]*model.SourceCandidate, len(cands))
	copy(sorted, cands)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })
	return &model.AttributionResult{
		JumpID:       jumpID,
		Candidates:   sorted,
		Verdict:      sorted[0].Source,
		VerdictScore: sorted[0].Score,
	}, nil
}

// ConfirmJump 人工确认跳变段来源（跳变段置 confirmed）。
func (svc *Service) ConfirmJump(jumpID int64, source model.CandidateSource) error {
	if !source.Valid() || source == model.SourceInsufficient {
		return fmt.Errorf("cannot confirm with source: %s", source)
	}
	return svc.Store.Jumps.MarkJumpConfirmed(jumpID)
}

// LinkAction 链路处置：隔离或恢复。必须与分析持同一时钟锁，避免评分读到半更新链路。
func (svc *Service) LinkAction(linkID int64, isolate bool) (*model.CompareLink, error) {
	lk, err := svc.Store.Links.Get(linkID)
	if err != nil {
		return nil, err
	}
	unlock := svc.lockClock(lk.SubjectID)
	defer unlock()
	status := model.LinkHealthy
	if isolate {
		status = model.LinkIsolated
	}
	return svc.Store.Links.SetStatus(linkID, status)
}

// SealClock 封存时钟（最终状态，拒绝修改）。
func (svc *Service) SealClock(clockID int64) (*model.Clock, error) {
	return svc.Store.Clocks.SetStatus(clockID, model.ClockSealed)
}

// Stats 汇总统计。
type Stats struct {
	Clocks      int            `json:"clocks"`
	Samples     int            `json:"samples"`
	EnvReadings int            `json:"env_readings"`
	Links       int            `json:"links"`
	Windows     int            `json:"windows"`
	Jumps       int            `json:"jumps"`
	Snapshots   int            `json:"snapshots"`
	ByStatus    map[string]int `json:"by_status"`
	Time        time.Time      `json:"time"`
}

// CollectStats 统计各表数量与时钟状态分布。
func (svc *Service) CollectStats() (*Stats, error) {
	st := &Stats{ByStatus: map[string]int{}, Time: time.Now().UTC()}
	clocks, err := svc.Store.Clocks.List()
	if err != nil {
		return nil, err
	}
	st.Clocks = len(clocks)
	for _, c := range clocks {
		st.ByStatus[string(c.Status)]++
	}
	for _, clockID := range clockIDs(clocks) {
		n, err := svc.Store.Samples.CountByClock(clockID)
		if err != nil {
			return nil, err
		}
		st.Samples += n
	}
	links, err := svc.Store.Links.List()
	if err != nil {
		return nil, err
	}
	st.Links = len(links)
	for _, clockID := range clockIDs(clocks) {
		n, err := svc.Store.Windows.CountByClock(clockID)
		if err != nil {
			return nil, err
		}
		st.Windows += n
		js, err := svc.Store.Jumps.ListJumpsByClock(clockID)
		if err != nil {
			return nil, err
		}
		st.Jumps += len(js)
	}
	snaps, err := svc.Store.Snapshots.List()
	if err != nil {
		return nil, err
	}
	st.Snapshots = len(snaps)
	return st, nil
}

func clockIDs(clocks []*model.Clock) []int64 {
	ids := make([]int64, 0, len(clocks))
	for _, c := range clocks {
		ids = append(ids, c.ID)
	}
	return ids
}
