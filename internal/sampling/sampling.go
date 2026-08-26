// Package sampling 负责频率样本与环境读数的接入。
//
// 幂等约束：同一时钟的样本序号 (clock_id, seq) 全局唯一，重复上报被拒绝；
// 单位校验：原始频率读数必须在标称频率容差内，否则拒绝。
package sampling

import (
	"context"
	"fmt"
	"time"

	"task265-atomjump/internal/baseline"
	"task265-atomjump/internal/model"
	"task265-atomjump/internal/store"
)

// Receiver 采样接收器：编排样本换算、幂等写入与环境读数写入。
type Receiver struct {
	clocks  *store.ClockStore
	samples *store.SampleStore
	envs    *store.EnvStore
}

// NewReceiver 构造采样接收器。
func NewReceiver(c *store.ClockStore, s *store.SampleStore, e *store.EnvStore) *Receiver {
	return &Receiver{clocks: c, samples: s, envs: e}
}

// IngestInput 频率样本上报输入。
type IngestInput struct {
	ClockID  int64
	Seq      int64
	TakenAt  time.Time
	FreqHz   float64
	Baseline string
}

// Ingest 接收一条频率样本（无截止时间）。
func (r *Receiver) Ingest(in IngestInput) (*model.Sample, error) {
	return r.IngestCtx(context.Background(), in)
}

func (r *Receiver) prepare(in IngestInput) (*model.Sample, error) {
	clk, err := r.clocks.Get(in.ClockID)
	if err != nil {
		return nil, err
	}
	if clk.Status == model.ClockSealed {
		return nil, model.ErrClockSealed
	}
	ref, err := baseline.ParseBaseline(in.Baseline)
	if err != nil {
		return nil, err
	}
	ppb, err := baseline.Normalize(in.FreqHz, clk.NominalHz)
	if err != nil {
		return nil, err
	}
	return &model.Sample{
		ClockID:     in.ClockID,
		Seq:         in.Seq,
		TakenAt:     in.TakenAt.UTC(),
		FreqHz:      in.FreqHz,
		OffsetPPB:   ppb,
		BaselineRef: ref,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

// IngestCtx 接收一条频率样本：校验未封存、换算 ppb、尊重 ctx 取消。
func (r *Receiver) IngestCtx(ctx context.Context, in IngestInput) (*model.Sample, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: ingest: %v", model.ErrCanceled, err)
	}
	sm, err := r.prepare(in)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: ingest: %v", model.ErrCanceled, err)
	}
	if err := r.samples.Insert(sm); err != nil {
		return nil, err
	}
	return sm, nil
}

// IngestMany 批量写入。取消时事务回滚，后续单条上报仍可使用同一连接。
func (r *Receiver) IngestMany(ctx context.Context, ins []IngestInput) ([]*model.Sample, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	prepared := make([]*model.Sample, 0, len(ins))
	for _, in := range ins {
		sm, err := r.prepare(in)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, sm)
	}
	if err := r.samples.InsertBatch(ctx, prepared); err != nil {
		return nil, err
	}
	return prepared, nil
}

// IngestEnv 接收一条环境读数（时钟必须存在）。
func (r *Receiver) IngestEnv(in *model.EnvReading) (*model.EnvReading, error) {
	if _, err := r.clocks.Get(in.ClockID); err != nil {
		return nil, err
	}
	in.TakenAt = in.TakenAt.UTC()
	in.CreatedAt = time.Now().UTC()
	if err := r.envs.Insert(in); err != nil {
		return nil, err
	}
	return in, nil
}

// NextSeq 计算时钟下一个建议样本序号（基于当前最大序号+1，供客户端参考）。
func (r *Receiver) NextSeq(clockID int64) (int64, error) {
	max, err := r.samples.MaxSeq(clockID)
	if err != nil {
		return 0, err
	}
	return max + 1, nil
}
