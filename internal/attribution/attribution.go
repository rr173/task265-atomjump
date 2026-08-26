// Package attribution 对跳变段进行来源判别评分。
//
// 三个候选来源类别：
//   - environment：跳变时段内环境读数（温度/湿度/气压）出现超过阈值的波动（时间相干性）；
//   - link：仅部分比对链路出现不一致（链路相关）；
//   - device：全部链路一致跳变且环境稳定（设备自身状态切换）。
//
// 评分基于时间相干性与链路一致性证据；证据不足时给出 insufficient 候选。
package attribution

import (
	"context"
	"fmt"
	"math"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

// 环境相干性阈值。
const (
	TempThreshC     = 0.5  // 温度波动阈值（摄氏度）
	HumidityThresh  = 2.0  // 湿度波动阈值（%）
	PressureThresh  = 1.0  // 气压波动阈值（hPa）
	VibrationThresh = 0.3  // 振动阈值（mm/s）
)

// Scorer 来源判别评分器。
type Scorer struct {
	envs *store.EnvStore
	links *store.LinkStore
	jumps *store.JumpStore
}

// NewScorer 构造评分器。
func NewScorer(e *store.EnvStore, l *store.LinkStore, j *store.JumpStore) *Scorer {
	return &Scorer{envs: e, links: l, jumps: j}
}

// Score 对单个跳变段评分来源候选并持久化。
func (s *Scorer) Score(jumpID int64) ([]*model.SourceCandidate, error) {
	return s.ScoreCtx(context.Background(), jumpID)
}

// ScoreCtx 评分。底层错误必须 %w 包装，失败时由调用方回滚半写入候选。
func (s *Scorer) ScoreCtx(ctx context.Context, jumpID int64) ([]*model.SourceCandidate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: score jump %d: %v", model.ErrCanceled, jumpID, err)
	}
	sg, err := s.jumps.GetJump(jumpID)
	if err != nil {
		return nil, fmt.Errorf("score jump %d: %w", jumpID, err)
	}
	existing, err := s.jumps.ListCandidates(jumpID)
	if err != nil {
		return nil, fmt.Errorf("score jump %d: %w", jumpID, err)
	}
	if len(existing) > 0 {
		return existing, nil // 已评分，幂等
	}

	// 环境相干证据
	envScore, envDetail := s.environmentCoherence(sg.ClockID, sg.StartAt, sg.EndAt)
	// 链路相干证据
	linkScore, linkDetail, err := s.linkCoherence(sg.ClockID, sg.StartAt, sg.EndAt)
	if err != nil {
		return nil, fmt.Errorf("score jump %d: %w", jumpID, err)
	}
	// 设备候选：环境稳定 + 链路一致 -> 设备自身
	devScore := 0.0
	if envScore < 0.3 && linkScore > 0.5 {
		devScore = 1.0 - envScore
	}

	cands := []*model.SourceCandidate{
		{JumpID: jumpID, Source: model.SourceEnvironment, Score: envScore, Rationale: envDetail},
		{JumpID: jumpID, Source: model.SourceLink, Score: linkScore, Rationale: linkDetail},
		{JumpID: jumpID, Source: model.SourceDevice, Score: devScore, Rationale: "全部链路一致跳变且环境读数稳定，判定为设备自身状态切换。"},
	}
	if maxScore(cands) < 0.5 {
		cands = append(cands, &model.SourceCandidate{
			JumpID: jumpID, Source: model.SourceInsufficient, Score: 0.5 - maxScore(cands),
			Rationale: "各项证据不足，无法给出可靠来源结论。",
		})
	}
	var out []*model.SourceCandidate
	for _, c := range cands {
		saved, err := s.jumps.InsertCandidate(c)
		if err != nil {
			_ = s.jumps.DeleteCandidatesForJump(jumpID)
			return nil, fmt.Errorf("score jump %d: %w", jumpID, err)
		}
		out = append(out, saved)
	}
	return out, nil
}

// environmentCoherence 检查跳变时段内环境读数是否存在超阈值波动。
func (s *Scorer) environmentCoherence(clockID int64, from, to time.Time) (float64, string) {
	readings, err := s.envs.Range(clockID, from.Add(-time.Minute), to.Add(time.Minute))
	if err != nil || len(readings) < 2 {
		return 0, "跳变时段内无环境读数，环境相干性证据不足。"
	}
	tempDev := maxMinDelta(readings, func(r *model.EnvReading) float64 { return r.TempC })
	humDev := maxMinDelta(readings, func(r *model.EnvReading) float64 { return r.HumidityPct })
	pressDev := maxMinDelta(readings, func(r *model.EnvReading) float64 { return r.PressureHPa })
	vibDev := maxMinDelta(readings, func(r *model.EnvReading) float64 { return r.Vibration })

	hits := 0
	if tempDev > TempThreshC {
		hits++
	}
	if humDev > HumidityThresh {
		hits++
	}
	if pressDev > PressureThresh {
		hits++
	}
	if vibDev > VibrationThresh {
		hits++
	}
	score := float64(hits) / 4.0
	detail := fmt.Sprintf(
		"环境相干：温度波动 %.2f°C、湿度 %.2f%%、气压 %.2fhPa、振动 %.2fmm/s，命中 %d/4 项。",
		tempDev, humDev, pressDev, vibDev, hits)
	return score, detail
}

// linkCoherence 检查跳变时段内各比对链路的一致性：全部链路一致 -> 链路相关低；
// 仅部分链路跳变 -> 链路相关高。
func (s *Scorer) linkCoherence(clockID int64, from, to time.Time) (float64, string, error) {
	links, err := s.links.ListBySubject(clockID)
	if err != nil {
		return 0, "", err
	}
	if len(links) == 0 {
		return 0, "该时钟没有配置比对链路，无法评估链路相干性。", nil
	}
	healthy := 0
	suspect := 0
	for _, lk := range links {
		switch lk.Status {
		case model.LinkHealthy:
			healthy++
		case model.LinkSuspect, model.LinkIsolated:
			suspect++
		}
	}
	// 链路不一致比例
	ratio := float64(suspect) / float64(len(links))
	score := ratio
	detail := fmt.Sprintf(
		"链路相干：共 %d 条比对链路，其中 %d 条可疑/已隔离，不一致比例 %.2f。",
		len(links), suspect, ratio)
	return score, detail, nil
}

func maxMinDelta(xs []*model.EnvReading, f func(*model.EnvReading) float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	minV, maxV := math.Inf(1), math.Inf(-1)
	for _, x := range xs {
		v := f(x)
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	return maxV - minV
}

func maxScore(cs []*model.SourceCandidate) float64 {
	m := 0.0
	for _, c := range cs {
		if c.Score > m {
			m = c.Score
		}
	}
	return m
}
