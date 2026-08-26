package service

import (
	"sync"

	"task265-atomjump/internal/model"
)

// liveIndex 是样本主数据的内存投影。Analyze 必须先拷贝快照再窗口化，
// 否则并发 Ingest 会改写正在遍历的底层数组。
type liveIndex struct {
	mu      sync.Mutex
	samples map[int64][]*model.Sample
}

func newLiveIndex() *liveIndex {
	return &liveIndex{samples: map[int64][]*model.Sample{}}
}

func (l *liveIndex) Append(sm *model.Sample) {
	if sm == nil {
		return
	}
	l.samples[sm.ClockID] = append(l.samples[sm.ClockID], sm)
}

func (l *liveIndex) Snapshot(clockID int64) []*model.Sample {
	return l.samples[clockID]
}

func (l *liveIndex) Replace(clockID int64, samples []*model.Sample) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*model.Sample, len(samples))
	for i, sm := range samples {
		if sm == nil {
			continue
		}
		cp := *sm
		out[i] = &cp
	}
	l.samples[clockID] = out
}
