package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"task265-atomjump/internal/model"
)

// ClockStore 时钟持久化。
type ClockStore struct{ db *sql.DB }

// Create 新建时钟（同名唯一；默认状态 collecting）。
func (c *ClockStore) Create(name, clockType string, nominalHz float64, isReference bool, referenceID int64) (*model.Clock, error) {
	if err := model.ValidateClockType(clockType); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	res, err := c.db.Exec(
		`INSERT INTO clocks(name, clock_type, nominal_hz, status, reference_id, is_reference, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		name, clockType, nominalHz, model.ClockCollecting, referenceID, boolToInt(isReference), now, now)
	if err != nil {
		return nil, fmt.Errorf("insert clock: %w", err)
	}
	id, _ := res.LastInsertId()
	return c.Get(id)
}

// Get 按 ID 查询时钟。
func (c *ClockStore) Get(id int64) (*model.Clock, error) {
	row := c.db.QueryRow(
		`SELECT id, name, clock_type, nominal_hz, status, reference_id, is_reference, created_at, updated_at
		 FROM clocks WHERE id=?`, id)
	clk, err := scanClock(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrClockNotFound
	}
	return clk, err
}

// GetByName 按名称查询时钟。
func (c *ClockStore) GetByName(name string) (*model.Clock, error) {
	row := c.db.QueryRow(
		`SELECT id, name, clock_type, nominal_hz, status, reference_id, is_reference, created_at, updated_at
		 FROM clocks WHERE name=?`, name)
	clk, err := scanClock(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrClockNotFound
	}
	return clk, err
}

// List 列出全部时钟。
func (c *ClockStore) List() ([]*model.Clock, error) {
	rows, err := c.db.Query(
		`SELECT id, name, clock_type, nominal_hz, status, reference_id, is_reference, created_at, updated_at
		 FROM clocks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Clock
	for rows.Next() {
		clk, err := scanClock(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, clk)
	}
	return out, rows.Err()
}

// SetStatus 原子化更新时钟状态（含合法性校验；封存后拒绝修改）。
func (c *ClockStore) SetStatus(id int64, next model.ClockStatus) (*model.Clock, error) {
	cur, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if cur.Status == model.ClockSealed {
		return nil, model.ErrClockSealed
	}
	if !cur.Status.CanTransition(next) {
		return nil, model.ErrClockStatus(cur.Status, next)
	}
	now := time.Now().UTC()
	if _, err := c.db.Exec(
		`UPDATE clocks SET status=?, updated_at=? WHERE id=? AND status=?`,
		next, now, id, cur.Status); err != nil {
		return nil, err
	}
	return c.Get(id)
}

// UpdateReference 更新时钟关联的参考钟（仅允许未封存时钟）。
func (c *ClockStore) UpdateReference(id, refID int64) (*model.Clock, error) {
	cur, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if cur.Status == model.ClockSealed {
		return nil, model.ErrClockSealed
	}
	if id == refID {
		return nil, model.ErrReferenceSelfLoop
	}
	now := time.Now().UTC()
	if _, err := c.db.Exec(
		`UPDATE clocks SET reference_id=?, updated_at=? WHERE id=?`, refID, now, id); err != nil {
		return nil, err
	}
	return c.Get(id)
}

// MarkIsReference 将时钟标记为主基准参考钟。
func (c *ClockStore) MarkIsReference(id int64) (*model.Clock, error) {
	now := time.Now().UTC()
	if _, err := c.db.Exec(
		`UPDATE clocks SET is_reference=1, updated_at=? WHERE id=?`, now, id); err != nil {
		return nil, err
	}
	return c.Get(id)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanClock(r rowScanner) (*model.Clock, error) {
	var clk model.Clock
	var refID int64
	var isRef int
	err := r.Scan(&clk.ID, &clk.Name, &clk.ClockType, &clk.NominalHz,
		&clk.Status, &refID, &isRef, &clk.CreatedAt, &clk.UpdatedAt)
	if err != nil {
		return nil, err
	}
	clk.ReferenceID = refID
	clk.IsReference = isRef == 1
	return &clk, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
