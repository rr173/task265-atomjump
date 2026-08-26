package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"task265-atomjump/internal/model"
)

// SnapshotStore 诊断快照持久化（发布后不可变，新发布替代旧快照）。
type SnapshotStore struct{ db *sql.DB }

// Insert 创建草稿快照。
func (s *SnapshotStore) Insert(snap *model.Snapshot) (*model.Snapshot, error) {
	iso, _ := json.Marshal(snap.IsolatedLinks)
	jumps, _ := json.Marshal(snap.JumpIDs)
	ev, _ := json.Marshal(snap.Evidence)
	if snap.Evidence == nil {
		ev = []byte("{}")
	}
	res, err := s.db.Exec(
		`INSERT INTO snapshots(clock_id, status, reference_id, isolated_links, summary, jump_ids, evidence_json, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		snap.ClockID, model.SnapshotDraft, snap.ReferenceID, string(iso), snap.Summary, string(jumps), string(ev), time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("insert snapshot: %w", err)
	}
	id, _ := res.LastInsertId()
	snap.ID = id
	snap.Status = model.SnapshotDraft
	return snap, nil
}

// Get 按 ID 查询快照。
func (s *SnapshotStore) Get(id int64) (*model.Snapshot, error) {
	row := s.db.QueryRow(
		`SELECT id, clock_id, status, reference_id, isolated_links, summary, jump_ids, evidence_json, created_at, published_at, superseded_at
		 FROM snapshots WHERE id=?`, id)
	snap, err := scanSnapshot(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrSnapshotNotFound
	}
	return snap, err
}

// ListByClock 列出时钟全部快照（按创建时间倒序）。
func (s *SnapshotStore) ListByClock(clockID int64) ([]*model.Snapshot, error) {
	rows, err := s.db.Query(
		`SELECT id, clock_id, status, reference_id, isolated_links, summary, jump_ids, evidence_json, created_at, published_at, superseded_at
		 FROM snapshots WHERE clock_id=? ORDER BY created_at DESC`, clockID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSnapshots(rows)
}

// List 列出全部快照。
func (s *SnapshotStore) List() ([]*model.Snapshot, error) {
	rows, err := s.db.Query(
		`SELECT id, clock_id, status, reference_id, isolated_links, summary, jump_ids, evidence_json, created_at, published_at, superseded_at
		 FROM snapshots ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSnapshots(rows)
}

// Publish 原子发布快照：draft -> published，同时把该时钟其他 published 快照置为 superseded。
func (s *SnapshotStore) Publish(id int64) (*model.Snapshot, error) {
	snap, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if snap.Status == model.SnapshotPublished {
		return nil, model.ErrSnapshotAlreadyPub
	}
	if snap.Status != model.SnapshotDraft {
		return nil, model.ErrSnapshotImmutable
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	// 旧发布快照 -> superseded
	if _, err := tx.Exec(
		`UPDATE snapshots SET status='superseded', superseded_at=? WHERE clock_id=? AND status='published'`,
		now, snap.ClockID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`UPDATE snapshots SET status='published', published_at=? WHERE id=?`, now, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// PublishAndSeal 在同一事务里：冻结证据、发布快照、按状态机封存时钟。
// 任一步失败则回滚，避免 published 快照与 live 时钟分叉。
func (s *SnapshotStore) PublishAndSeal(id int64, evidence *model.FrozenEvidence) (*model.Snapshot, error) {
	snap, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if snap.Status == model.SnapshotPublished {
		return nil, model.ErrSnapshotAlreadyPub
	}
	if snap.Status != model.SnapshotDraft {
		return nil, model.ErrSnapshotImmutable
	}
	if evidence == nil {
		return nil, fmt.Errorf("publish snapshot %d: missing frozen evidence", id)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin publish: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	ev, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE snapshots SET status='superseded', superseded_at=? WHERE clock_id=? AND status='published'`,
		now, snap.ClockID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`UPDATE snapshots SET status='published', published_at=?, evidence_json=? WHERE id=?`,
		now, string(ev), id); err != nil {
		return nil, err
	}
	var curStatus string
	if err := tx.QueryRow(`SELECT status FROM clocks WHERE id=?`, snap.ClockID).Scan(&curStatus); err != nil {
		return nil, err
	}
	from := model.ClockStatus(curStatus)
	if from != model.ClockSealed {
		if !from.CanTransition(model.ClockSealed) {
			return nil, model.ErrClockStatus(from, model.ClockSealed)
		}
		if _, err := tx.Exec(
			`UPDATE clocks SET status=?, updated_at=? WHERE id=? AND status=?`,
			model.ClockSealed, now, snap.ClockID, from); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit publish: %w", err)
	}
	return s.Get(id)
}

// SetClockSealedOnPublish 保留兼容路径：单独封存，不写入冻结证据。
func (s *SnapshotStore) SetClockSealedOnPublish(snapID int64) error {
	_, err := s.db.Exec(
		`UPDATE clocks SET status='sealed', updated_at=? WHERE id=(
			SELECT clock_id FROM snapshots WHERE id=?)`, time.Now().UTC(), snapID)
	return err
}

func scanSnapshot(r rowScanner) (*model.Snapshot, error) {
	var snap model.Snapshot
	var iso, jumps, evidence string
	var pubAt, supAt sql.NullTime
	err := r.Scan(&snap.ID, &snap.ClockID, &snap.Status, &snap.ReferenceID, &iso,
		&snap.Summary, &jumps, &evidence, &snap.CreatedAt, &pubAt, &supAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(iso), &snap.IsolatedLinks)
	_ = json.Unmarshal([]byte(jumps), &snap.JumpIDs)
	if evidence != "" && evidence != "{}" {
		var ev model.FrozenEvidence
		if err := json.Unmarshal([]byte(evidence), &ev); err == nil {
			snap.Evidence = &ev
		}
	}
	if pubAt.Valid {
		t := pubAt.Time
		snap.PublishedAt = &t
	}
	if supAt.Valid {
		t := supAt.Time
		snap.SupersededAt = &t
	}
	return &snap, nil
}

func scanSnapshots(rows *sql.Rows) ([]*model.Snapshot, error) {
	var out []*model.Snapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}
