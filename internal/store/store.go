// Package store 提供基于 SQLite 的持久化实现。
//
// 所有建表迁移在 Open 时自动执行；关闭后重新打开同一数据库即可恢复全部状态
// （时钟、样本、环境读数、比对链路、频率窗口、跳变段、来源候选与诊断快照）。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store 聚合全部存储访问器。
type Store struct {
	db *sql.DB

	Clocks   *ClockStore
	Samples  *SampleStore
	Envs     *EnvStore
	Links    *LinkStore
	Windows  *WindowStore
	Jumps    *JumpStore
	Snapshots *SnapshotStore
}

// Open 打开（或创建）SQLite 数据库并执行建表迁移。
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("empty db path")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir db dir: %w", err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 单写者，串行化连接避免锁竞争
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	s := &Store{db: db}
	s.Clocks = &ClockStore{db: db}
	s.Samples = &SampleStore{db: db}
	s.Envs = &EnvStore{db: db}
	s.Links = &LinkStore{db: db}
	s.Windows = &WindowStore{db: db}
	s.Jumps = &JumpStore{db: db}
	s.Snapshots = &SnapshotStore{db: db}
	return s, nil
}

// Close 关闭数据库。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB 暴露底层句柄（供事务使用）。
func (s *Store) DB() *sql.DB { return s.db }

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS clocks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			clock_type TEXT NOT NULL,
			nominal_hz REAL NOT NULL,
			status TEXT NOT NULL DEFAULT 'collecting',
			reference_id INTEGER NOT NULL DEFAULT 0,
			is_reference INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_clocks_name ON clocks(name)`,
		`CREATE TABLE IF NOT EXISTS samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			clock_id INTEGER NOT NULL REFERENCES clocks(id),
			seq INTEGER NOT NULL,
			taken_at DATETIME NOT NULL,
			freq_hz REAL NOT NULL,
			offset_ppb REAL NOT NULL,
			baseline_ref TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			UNIQUE(clock_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_samples_clock_time ON samples(clock_id, taken_at)`,
		`CREATE TABLE IF NOT EXISTS env_readings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			clock_id INTEGER NOT NULL REFERENCES clocks(id),
			taken_at DATETIME NOT NULL,
			temp_c REAL NOT NULL,
			humidity_pct REAL NOT NULL,
			pressure_hpa REAL NOT NULL,
			vibration REAL NOT NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_env_clock_time ON env_readings(clock_id, taken_at)`,
		`CREATE TABLE IF NOT EXISTS compare_links (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject_id INTEGER NOT NULL REFERENCES clocks(id),
			reference_id INTEGER NOT NULL REFERENCES clocks(id),
			offset_ppb REAL NOT NULL,
			status TEXT NOT NULL DEFAULT 'healthy',
			latency_ms INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(subject_id, reference_id)
		)`,
		`CREATE TABLE IF NOT EXISTS freq_windows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			clock_id INTEGER NOT NULL REFERENCES clocks(id),
			start_at DATETIME NOT NULL,
			end_at DATETIME NOT NULL,
			sample_n INTEGER NOT NULL DEFAULT 0,
			mean_ppb REAL NOT NULL DEFAULT 0,
			stddev_ppb REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'raw',
			created_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_windows_clock_time ON freq_windows(clock_id, start_at)`,
		`CREATE TABLE IF NOT EXISTS jump_segments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			clock_id INTEGER NOT NULL REFERENCES clocks(id),
			window_id INTEGER NOT NULL REFERENCES freq_windows(id),
			start_at DATETIME NOT NULL,
			end_at DATETIME NOT NULL,
			magnitude_ppb REAL NOT NULL,
			direction TEXT NOT NULL,
			detected_at DATETIME NOT NULL,
			status TEXT NOT NULL DEFAULT 'open'
		)`,
		`CREATE TABLE IF NOT EXISTS source_candidates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			jump_id INTEGER NOT NULL REFERENCES jump_segments(id),
			source TEXT NOT NULL,
			score REAL NOT NULL,
			rationale TEXT NOT NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_candidates_jump ON source_candidates(jump_id)`,
		`CREATE TABLE IF NOT EXISTS snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			clock_id INTEGER NOT NULL REFERENCES clocks(id),
			status TEXT NOT NULL DEFAULT 'draft',
			reference_id INTEGER NOT NULL DEFAULT 0,
			isolated_links TEXT NOT NULL DEFAULT '[]',
			summary TEXT NOT NULL DEFAULT '',
			jump_ids TEXT NOT NULL DEFAULT '[]',
			evidence_json TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL,
			published_at DATETIME,
			superseded_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS self_checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			check_at DATETIME NOT NULL,
			kind TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			passed INTEGER NOT NULL DEFAULT 1
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("exec migrate stmt: %w", err)
		}
	}
	return nil
}
