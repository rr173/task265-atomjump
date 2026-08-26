package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"task265-atomjump/internal/model"
)

// WindowStore 频率窗口持久化。
type WindowStore struct{ db *sql.DB }

// Insert 写入一个频率窗口。
func (w *WindowStore) Insert(win *model.FreqWindow) (*model.FreqWindow, error) {
	if !win.Status.Valid() {
		return nil, fmt.Errorf("invalid window status: %s", win.Status)
	}
	res, err := w.db.Exec(
		`INSERT INTO freq_windows(clock_id, start_at, end_at, sample_n, mean_ppb, stddev_ppb, status, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		win.ClockID, win.StartAt, win.EndAt, win.SampleN, win.MeanPPB, win.StdDevPPB, win.Status, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("insert window: %w", err)
	}
	id, _ := res.LastInsertId()
	win.ID = id
	return win, nil
}

// Get 按 ID 查询窗口。
func (w *WindowStore) Get(id int64) (*model.FreqWindow, error) {
	row := w.db.QueryRow(
		`SELECT id, clock_id, start_at, end_at, sample_n, mean_ppb, stddev_ppb, status, created_at
		 FROM freq_windows WHERE id=?`, id)
	win, err := scanWindow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrWindowNotFound
	}
	return win, err
}

// ListByClock 列出时钟全部窗口（时间升序）。
func (w *WindowStore) ListByClock(clockID int64) ([]*model.FreqWindow, error) {
	rows, err := w.db.Query(
		`SELECT id, clock_id, start_at, end_at, sample_n, mean_ppb, stddev_ppb, status, created_at
		 FROM freq_windows WHERE clock_id=? ORDER BY start_at ASC`, clockID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.FreqWindow
	for rows.Next() {
		win, err := scanWindow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, win)
	}
	return out, rows.Err()
}

// QueryByClock 返回未关闭的窗口结果集，调用方必须 Close。
func (w *WindowStore) QueryByClock(clockID int64) (*sql.Rows, error) {
	return w.db.Query(
		`SELECT id, clock_id, start_at, end_at, sample_n, mean_ppb, stddev_ppb, status, created_at
		 FROM freq_windows WHERE clock_id=? ORDER BY start_at ASC`, clockID)
}

// DeleteIDs 回滚一次分析写入的窗口。
func (w *WindowStore) DeleteIDs(ids []int64) error {
	for _, id := range ids {
		if _, err := w.db.Exec(`DELETE FROM freq_windows WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

// CountByClock 统计时钟窗口数。
func (w *WindowStore) CountByClock(clockID int64) (int, error) {
	var n int
	err := w.db.QueryRow(`SELECT COUNT(*) FROM freq_windows WHERE clock_id=?`, clockID).Scan(&n)
	return n, err
}

// SetStatus 更新窗口状态（raw/stable/jump/gap/excluded）。
func (w *WindowStore) SetStatus(id int64, status model.WindowStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid window status: %s", status)
	}
	if _, err := w.db.Exec(`UPDATE freq_windows SET status=? WHERE id=?`, status, id); err != nil {
		return err
	}
	return nil
}

func scanWindow(r rowScanner) (*model.FreqWindow, error) {
	var win model.FreqWindow
	err := r.Scan(&win.ID, &win.ClockID, &win.StartAt, &win.EndAt, &win.SampleN,
		&win.MeanPPB, &win.StdDevPPB, &win.Status, &win.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &win, nil
}
