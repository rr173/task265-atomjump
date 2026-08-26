package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"task265-atomjump/internal/model"
)

// SampleStore 频率样本持久化（clock_id+seq 幂等）。
type SampleStore struct{ db *sql.DB }

// Insert 写入一条样本；若 (clock_id, seq) 已存在则返回 ErrSampleDuplicate。
func (s *SampleStore) Insert(sm *model.Sample) error {
	_, err := s.db.Exec(
		`INSERT INTO samples(clock_id, seq, taken_at, freq_hz, offset_ppb, baseline_ref, created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		sm.ClockID, sm.Seq, sm.TakenAt, sm.FreqHz, sm.OffsetPPB, sm.BaselineRef, time.Now().UTC())
	if err != nil {
		if isUniqueViolation(err) {
			return model.ErrSampleDuplicate
		}
		return fmt.Errorf("insert sample: %w", err)
	}
	return nil
}

// InsertBatch 在同一事务中写入多条样本。ctx 取消时 Rollback，避免半批脏行占用连接。
func (s *SampleStore) InsertBatch(ctx context.Context, samples []*model.Sample) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin sample batch: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("insert batch canceled: %v", err)
	}
	for _, sm := range samples {
		_, err := tx.Exec(
			`INSERT INTO samples(clock_id, seq, taken_at, freq_hz, offset_ppb, baseline_ref, created_at)
			 VALUES(?,?,?,?,?,?,?)`,
			sm.ClockID, sm.Seq, sm.TakenAt, sm.FreqHz, sm.OffsetPPB, sm.BaselineRef, time.Now().UTC())
		if err != nil {
			if isUniqueViolation(err) {
				return model.ErrSampleDuplicate
			}
			return fmt.Errorf("insert sample batch: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sample batch: %w", err)
	}
	return nil
}

// ListByClock 按时钟列出样本（按时间升序）。
func (s *SampleStore) ListByClock(clockID int64, limit int) ([]*model.Sample, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.db.Query(
		`SELECT id, clock_id, seq, taken_at, freq_hz, offset_ppb, baseline_ref, created_at
		 FROM samples WHERE clock_id=? ORDER BY taken_at ASC LIMIT ?`, clockID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Sample
	for rows.Next() {
		var sm model.Sample
		if err := rows.Scan(&sm.ID, &sm.ClockID, &sm.Seq, &sm.TakenAt, &sm.FreqHz,
			&sm.OffsetPPB, &sm.BaselineRef, &sm.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &sm)
	}
	return out, rows.Err()
}

// CountByClock 统计时钟样本数。
func (s *SampleStore) CountByClock(clockID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM samples WHERE clock_id=?`, clockID).Scan(&n)
	return n, err
}

// MaxSeq 返回时钟当前最大样本序号（0 表示无样本）。
func (s *SampleStore) MaxSeq(clockID int64) (int64, error) {
	var n sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT MAX(seq) FROM samples WHERE clock_id=?`, clockID).Scan(&n); err != nil {
		return 0, err
	}
	return n.Int64, nil
}

// isUniqueViolation 识别 SQLite 唯一约束冲突。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var msg string
	if sqlErr, ok := err.(interface{ Error() string }); ok {
		msg = sqlErr.Error()
	}
	return errors.Is(err, sql.ErrNoRows) == false && containsAny(msg, "UNIQUE constraint failed")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
