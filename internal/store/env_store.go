package store

import (
	"database/sql"
	"fmt"
	"time"

	"task265-atomjump/internal/model"
)

// EnvStore 环境读数持久化。
type EnvStore struct{ db *sql.DB }

// Insert 写入一条环境读数。
func (e *EnvStore) Insert(r *model.EnvReading) error {
	_, err := e.db.Exec(
		`INSERT INTO env_readings(clock_id, taken_at, temp_c, humidity_pct, pressure_hpa, vibration, created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		r.ClockID, r.TakenAt, r.TempC, r.HumidityPct, r.PressureHPa, r.Vibration, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("insert env reading: %w", err)
	}
	return nil
}

// ListByClock 按时钟列出环境读数（时间升序）。
func (e *EnvStore) ListByClock(clockID int64, limit int) ([]*model.EnvReading, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := e.db.Query(
		`SELECT id, clock_id, taken_at, temp_c, humidity_pct, pressure_hpa, vibration, created_at
		 FROM env_readings WHERE clock_id=? ORDER BY taken_at ASC LIMIT ?`, clockID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.EnvReading
	for rows.Next() {
		var r model.EnvReading
		if err := rows.Scan(&r.ID, &r.ClockID, &r.TakenAt, &r.TempC, &r.HumidityPct,
			&r.PressureHPa, &r.Vibration, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// Range 查询 [from,to] 时间窗内的环境读数。
func (e *EnvStore) Range(clockID int64, from, to time.Time) ([]*model.EnvReading, error) {
	rows, err := e.db.Query(
		`SELECT id, clock_id, taken_at, temp_c, humidity_pct, pressure_hpa, vibration, created_at
		 FROM env_readings WHERE clock_id=? AND taken_at>=? AND taken_at<=? ORDER BY taken_at ASC`,
		clockID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.EnvReading
	for rows.Next() {
		var r model.EnvReading
		if err := rows.Scan(&r.ID, &r.ClockID, &r.TakenAt, &r.TempC, &r.HumidityPct,
			&r.PressureHPa, &r.Vibration, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}
