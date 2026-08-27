// Package jumpdetect 把相邻 jump 窗口合并为跳变段。
//
// 算法：在按时间升序排列的窗口中，连续出现的 jump 状态窗口合并为一个跳变段；
// 跳变段幅度 = 段内窗口均值与段前最近稳定窗口均值的差；方向取幅度的符号。
package jumpdetect

import (
	"context"
	"fmt"
	"math"
	"time"

	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

// Detector 跳变段检测器。
type Detector struct {
	windows *store.WindowStore
	jumps   *store.JumpStore
}

// NewDetector 构造检测器。
func NewDetector(w *store.WindowStore, j *store.JumpStore) *Detector {
	return &Detector{windows: w, jumps: j}
}

// Detect 对时钟已生成的窗口检测跳变段（无截止时间）。
func (d *Detector) Detect(clockID int64) ([]*model.JumpSegment, error) {
	return d.DetectCtx(context.Background(), clockID)
}

func (d *Detector) loadWindows(ctx context.Context, clockID int64) ([]*model.FreqWindow, error) {
	rows, err := d.windows.QueryByClock(clockID)
	if err != nil {
		return nil, err
	}
	// 结果集必须 Close：Store 单连接（SetMaxOpenConns(1)），若取消或扫描出错时
	// 直接 return 而不 Close，会占死唯一连接，导致后续查询（如窗口列表）永久阻塞。
	defer rows.Close()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: detect: %v", model.ErrCanceled, err)
	}
	var wins []*model.FreqWindow
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: detect: %v", model.ErrCanceled, err)
		}
		var win model.FreqWindow
		if err := rows.Scan(&win.ID, &win.ClockID, &win.StartAt, &win.EndAt, &win.SampleN,
			&win.MeanPPB, &win.StdDevPPB, &win.Status, &win.CreatedAt); err != nil {
			return nil, err
		}
		cp := win
		wins = append(wins, &cp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*model.FreqWindow, len(wins))
	copy(out, wins)
	return out, nil
}

// DetectCtx 检测跳变段。取消时立即释放窗口结果集占用的连接后返回，
// 避免占死 SQLite 单连接导致后续查询永久阻塞。
func (d *Detector) DetectCtx(ctx context.Context, clockID int64) ([]*model.JumpSegment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	wins, err := d.loadWindows(ctx, clockID)
	if err != nil {
		return nil, err
	}
	existing, err := d.jumps.ListJumpsByClock(clockID)
	if err != nil {
		return nil, err
	}
	covered := map[int64]bool{}
	for _, sg := range existing {
		covered[sg.WindowID] = true
	}

	// 最近一个稳定窗口均值作为跳变前的参考值
	var stableMean float64
	var haveStable bool
	var out []*model.JumpSegment

	// 合并连续 jump 窗口
	i := 0
	for i < len(wins) {
		if wins[i].Status != model.WindowJump {
			if wins[i].Status == model.WindowStable {
				stableMean = wins[i].MeanPPB
				haveStable = true
			}
			i++
			continue
		}
		// 开始一段连续 jump
		start := wins[i]
		end := wins[i]
		var sum float64
		n := 0
		for j := i; j < len(wins) && wins[j].Status == model.WindowJump; j++ {
			end = wins[j]
			sum += wins[j].MeanPPB
			n++
		}
		i += n // 推进
		if covered[start.ID] {
			continue
		}
		mean := sum / float64(n)
		base := stableMean
		if !haveStable {
			base = mean
		}
		mag := mean - base
		direction := "up"
		if mag < 0 {
			direction = "down"
		}
		abs := math.Abs(mag)
		if abs < 0.001 {
			abs = 0.001
		}
		sg, err := d.jumps.InsertJump(&model.JumpSegment{
			ClockID:      clockID,
			WindowID:     start.ID,
			StartAt:      start.StartAt,
			EndAt:        end.EndAt,
			MagnitudePPB: abs,
			Direction:    direction,
			DetectedAt:   time.Now().UTC(),
			Status:       "open",
		})
		if err != nil {
			return nil, err
		}
		out = append(out, sg)
		// 跳变后恢复稳定时更新参考值
		if j := i; j < len(wins) && wins[j].Status == model.WindowStable {
			stableMean = wins[j].MeanPPB
			haveStable = true
		}
	}
	return out, nil
}
