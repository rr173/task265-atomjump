// Package windowing 把频率样本切分为固定宽度窗口并判定窗口状态。
//
// 窗口状态机：raw -> stable / jump / gap / excluded。
// - 稳定：窗口内样本数达标且标准差低于阈值；
// - 跳变：均值相对上一窗口偏移超过跳变阈值（或窗口内标准差超阈值）；
// - 缺口：样本数不足；
// - 排除：被工程师显式标记不可信（通过排除 API）。
package windowing

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"task265-atomjump/internal/baseline"
	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

// Builder 频率窗口构建器。
type Builder struct {
	clocks  *store.ClockStore
	samples *store.SampleStore
	windows *store.WindowStore
	width   time.Duration
}

// NewBuilder 构造窗口构建器。
func NewBuilder(c *store.ClockStore, s *store.SampleStore, w *store.WindowStore, width time.Duration) *Builder {
	if width <= 0 {
		width = baseline.DefaultWindowWidth
	}
	return &Builder{clocks: c, samples: s, windows: w, width: width}
}

// Build 为时钟构建窗口（从数据库读样本）。
func (b *Builder) Build(clockID int64) ([]*model.FreqWindow, error) {
	return b.BuildCtx(context.Background(), clockID, nil)
}

// BuildCtx 按给定样本快照（或库内样本）切窗。samples 会被拷贝，避免并发 Ingest 改写遍历中的切片。
// ctx 取消时删除本轮已插入窗口并返回 ErrCanceled，不推进后续检测。
func (b *Builder) BuildCtx(ctx context.Context, clockID int64, samples []*model.Sample) ([]*model.FreqWindow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: build windows: %v", model.ErrCanceled, err)
	}
	if _, err := b.clocks.Get(clockID); err != nil {
		return nil, err
	}
	if samples == nil {
		var err error
		samples, err = b.samples.ListByClock(clockID, 10000)
		if err != nil {
			return nil, err
		}
	}
	copied := samples
	if len(copied) == 0 {
		return nil, nil
	}
	existing, err := b.windows.ListByClock(clockID)
	if err != nil {
		return nil, err
	}
	have := map[int64]bool{}
	var prevMean float64
	var prevValid bool
	for _, w := range existing {
		have[w.StartAt.Unix()] = true
		if w.Status == model.WindowStable {
			prevMean = w.MeanPPB
			prevValid = true
		}
	}

	type bucket struct {
		start, end time.Time
		ppbs       []float64
	}
	groups := map[int64]*bucket{}
	var keys []int64
	for _, sm := range copied {
		start, end := baseline.AlignWindow(sm.TakenAt, b.width)
		k := start.Unix()
		if _, ok := groups[k]; !ok {
			groups[k] = &bucket{start: start, end: end}
			keys = append(keys, k)
		}
		groups[k].ppbs = append(groups[k].ppbs, sm.OffsetPPB)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var out []*model.FreqWindow
	var inserted []int64
	var slot model.FreqWindow
	for _, k := range keys {
		if err := ctx.Err(); err != nil {
			_ = b.windows.DeleteIDs(inserted)
			return nil, fmt.Errorf("%w: build windows: %v", model.ErrCanceled, err)
		}
		if have[k] {
			continue
		}
		g := groups[k]
		mean, std := meanStd(g.ppbs)
		status := classify(len(g.ppbs), mean, std, prevMean, prevValid)
		win, err := b.windows.Insert(&model.FreqWindow{
			ClockID:   clockID,
			StartAt:   g.start,
			EndAt:     g.end,
			SampleN:   len(g.ppbs),
			MeanPPB:   mean,
			StdDevPPB: std,
			Status:    status,
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			_ = b.windows.DeleteIDs(inserted)
			return nil, err
		}
		inserted = append(inserted, win.ID)
		slot = *win
		out = append(out, &slot)
		if status == model.WindowStable {
			prevMean = mean
			prevValid = true
		}
	}
	return out, nil
}

// classify 判定窗口状态。
func classify(n int, mean, std, prevMean float64, prevValid bool) model.WindowStatus {
	if n < baseline.MinSamplesPerWindow {
		return model.WindowGap
	}
	if std > baseline.StdDevThresholdPPB {
		return model.WindowJump
	}
	if prevValid && math.Abs(mean-prevMean) > baseline.JumpThresholdPPB {
		return model.WindowJump
	}
	return model.WindowStable
}

// meanStd 计算均值与样本标准差。
func meanStd(xs []float64) (float64, float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	if len(xs) > 1 {
		return mean, math.Sqrt(ss / float64(len(xs)-1))
	}
	return mean, 0
}

// Exclude 把指定窗口标记为排除（不可信），拒绝封存时钟与已排除窗口。
func (b *Builder) Exclude(clockID, windowID int64) (*model.FreqWindow, error) {
	clk, err := b.clocks.Get(clockID)
	if err != nil {
		return nil, err
	}
	if clk.Status == model.ClockSealed {
		return nil, model.ErrClockSealed
	}
	win, err := b.windows.Get(windowID)
	if err != nil {
		return nil, err
	}
	if win.ClockID != clockID {
		return nil, model.ErrWindowNotFound
	}
	if win.Status == model.WindowExcluded {
		return win, nil
	}
	if err := b.windows.SetStatus(windowID, model.WindowExcluded); err != nil {
		return nil, err
	}
	return b.windows.Get(windowID)
}
