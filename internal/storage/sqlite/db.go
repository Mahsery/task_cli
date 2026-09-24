package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func Open(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory %q: %w", dir, err)
	}

	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// Single writer connection pool for daemon guarantees 0 contention
	conn.SetMaxOpenConns(1)

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to migrate db: %w", err)
	}

	return db, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		scope TEXT NOT NULL,
		repo_path TEXT NOT NULL DEFAULT '',
		seq_num INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		priority TEXT NOT NULL DEFAULT 'normal',
		tags TEXT NOT NULL DEFAULT '[]',
		author TEXT NOT NULL DEFAULT '',
		assignee TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		closed_at DATETIME,
		commit_sha TEXT NOT NULL DEFAULT '',
		commit_msg TEXT NOT NULL DEFAULT '',
		diff_stat TEXT NOT NULL DEFAULT '',
		diff_content TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_scope_repo ON tasks(scope, repo_path, status);
	CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);

	CREATE TABLE IF NOT EXISTS task_notes (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		author TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_notes_task ON task_notes(task_id, created_at ASC);
	`

	_, err := d.conn.Exec(schema)
	return err
}
