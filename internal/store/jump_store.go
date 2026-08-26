package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"task265-atomjump/internal/model"
)

// JumpStore 跳变段与来源候选持久化。
type JumpStore struct{ db *sql.DB }

// InsertJump 写入跳变段。
func (j *JumpStore) InsertJump(sg *model.JumpSegment) (*model.JumpSegment, error) {
	res, err := j.db.Exec(
		`INSERT INTO jump_segments(clock_id, window_id, start_at, end_at, magnitude_ppb, direction, detected_at, status)
		 VALUES(?,?,?,?,?,?,?,?)`,
		sg.ClockID, sg.WindowID, sg.StartAt, sg.EndAt, sg.MagnitudePPB, sg.Direction, time.Now().UTC(), "open")
	if err != nil {
		return nil, fmt.Errorf("insert jump: %w", err)
	}
	id, _ := res.LastInsertId()
	sg.ID = id
	return sg, nil
}

// GetJump 按 ID 查询跳变段。
func (j *JumpStore) GetJump(id int64) (*model.JumpSegment, error) {
	row := j.db.QueryRow(
		`SELECT id, clock_id, window_id, start_at, end_at, magnitude_ppb, direction, detected_at, status
		 FROM jump_segments WHERE id=?`, id)
	sg, err := scanJump(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrJumpNotFound
	}
	return sg, err
}

// ListJumpsByClock 列出时钟全部跳变段。
func (j *JumpStore) ListJumpsByClock(clockID int64) ([]*model.JumpSegment, error) {
	rows, err := j.db.Query(
		`SELECT id, clock_id, window_id, start_at, end_at, magnitude_ppb, direction, detected_at, status
		 FROM jump_segments WHERE clock_id=? ORDER BY start_at ASC`, clockID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.JumpSegment
	for rows.Next() {
		sg, err := scanJump(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

// MarkJumpConfirmed 标记跳变段已确认。
func (j *JumpStore) MarkJumpConfirmed(id int64) error {
	_, err := j.db.Exec(`UPDATE jump_segments SET status='confirmed' WHERE id=?`, id)
	return err
}

// DeleteCandidatesForJump 评分失败时清掉半写入候选，避免错误传播后留下脏证据。
func (j *JumpStore) DeleteCandidatesForJump(jumpID int64) error {
	_, err := j.db.Exec(`DELETE FROM source_candidates WHERE jump_id=?`, jumpID)
	return err
}

// InsertCandidate 写入来源候选。
func (j *JumpStore) InsertCandidate(c *model.SourceCandidate) (*model.SourceCandidate, error) {
	if !c.Source.Valid() {
		return nil, fmt.Errorf("invalid source: %s", c.Source)
	}
	res, err := j.db.Exec(
		`INSERT INTO source_candidates(jump_id, source, score, rationale, created_at)
		 VALUES(?,?,?,?,?)`,
		c.JumpID, c.Source, c.Score, c.Rationale, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("insert candidate: %w", err)
	}
	id, _ := res.LastInsertId()
	c.ID = id
	return c, nil
}

// ListCandidates 列出跳变段全部来源候选（按分数降序）。
func (j *JumpStore) ListCandidates(jumpID int64) ([]*model.SourceCandidate, error) {
	rows, err := j.db.Query(
		`SELECT id, jump_id, source, score, rationale, created_at
		 FROM source_candidates WHERE jump_id=? ORDER BY score DESC`, jumpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.SourceCandidate
	for rows.Next() {
		var c model.SourceCandidate
		if err := rows.Scan(&c.ID, &c.JumpID, &c.Source, &c.Score, &c.Rationale, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

func scanJump(r rowScanner) (*model.JumpSegment, error) {
	var sg model.JumpSegment
	err := r.Scan(&sg.ID, &sg.ClockID, &sg.WindowID, &sg.StartAt, &sg.EndAt,
		&sg.MagnitudePPB, &sg.Direction, &sg.DetectedAt, &sg.Status)
	if err != nil {
		return nil, err
	}
	return &sg, nil
}
