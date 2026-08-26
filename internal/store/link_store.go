package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"task265-atomjump/internal/model"
)

// LinkStore 比对链路持久化（subject+reference 唯一，防自环）。
type LinkStore struct{ db *sql.DB }

// Upsert 新建或更新一条比对链路。subject==reference 时报自环错误。
func (l *LinkStore) Upsert(subjectID, referenceID int64, offsetPPB float64, latencyMS int64) (*model.CompareLink, error) {
	if subjectID == referenceID {
		return nil, model.ErrReferenceSelfLoop
	}
	now := time.Now().UTC()
	if _, err := l.db.Exec(
		`INSERT INTO compare_links(subject_id, reference_id, offset_ppb, status, latency_ms, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(subject_id, reference_id) DO UPDATE SET
		   offset_ppb=excluded.offset_ppb, latency_ms=excluded.latency_ms, updated_at=excluded.updated_at`,
		subjectID, referenceID, offsetPPB, model.LinkHealthy, latencyMS, now, now); err != nil {
		return nil, fmt.Errorf("upsert link: %w", err)
	}
	return l.GetByPair(subjectID, referenceID)
}

// Get 按 ID 查询链路。
func (l *LinkStore) Get(id int64) (*model.CompareLink, error) {
	row := l.db.QueryRow(
		`SELECT id, subject_id, reference_id, offset_ppb, status, latency_ms, created_at, updated_at
		 FROM compare_links WHERE id=?`, id)
	link, err := scanLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrLinkNotFound
	}
	return link, err
}

// GetByPair 按 (subject, reference) 查询链路。
func (l *LinkStore) GetByPair(subjectID, referenceID int64) (*model.CompareLink, error) {
	row := l.db.QueryRow(
		`SELECT id, subject_id, reference_id, offset_ppb, status, latency_ms, created_at, updated_at
		 FROM compare_links WHERE subject_id=? AND reference_id=?`, subjectID, referenceID)
	link, err := scanLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrLinkNotFound
	}
	return link, err
}

// ListBySubject 列出被测钟的全部比对链路。
func (l *LinkStore) ListBySubject(subjectID int64) ([]*model.CompareLink, error) {
	rows, err := l.db.Query(
		`SELECT id, subject_id, reference_id, offset_ppb, status, latency_ms, created_at, updated_at
		 FROM compare_links WHERE subject_id=? ORDER BY id`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinks(rows)
}

// List 列出全部链路。
func (l *LinkStore) List() ([]*model.CompareLink, error) {
	rows, err := l.db.Query(
		`SELECT id, subject_id, reference_id, offset_ppb, status, latency_ms, created_at, updated_at
		 FROM compare_links ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinks(rows)
}

// SetStatus 更新链路状态（healthy/suspect/isolated）。
func (l *LinkStore) SetStatus(id int64, status model.LinkStatus) (*model.CompareLink, error) {
	if !status.Valid() {
		return nil, fmt.Errorf("invalid link status: %s", status)
	}
	if _, err := l.db.Exec(
		`UPDATE compare_links SET status=?, updated_at=? WHERE id=?`, status, time.Now().UTC(), id); err != nil {
		return nil, err
	}
	return l.Get(id)
}

// HealthyCount 统计健康链路数。
func (l *LinkStore) HealthyCount(subjectID int64) (int, error) {
	var n int
	err := l.db.QueryRow(
		`SELECT COUNT(*) FROM compare_links WHERE subject_id=? AND status='healthy'`, subjectID).Scan(&n)
	return n, err
}

func scanLink(r rowScanner) (*model.CompareLink, error) {
	var lk model.CompareLink
	err := r.Scan(&lk.ID, &lk.SubjectID, &lk.ReferenceID, &lk.OffsetPPB,
		&lk.Status, &lk.LatencyMS, &lk.CreatedAt, &lk.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &lk, nil
}

func scanLinks(rows *sql.Rows) ([]*model.CompareLink, error) {
	var out []*model.CompareLink
	for rows.Next() {
		lk, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lk)
	}
	return out, rows.Err()
}
