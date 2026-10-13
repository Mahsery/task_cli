package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type DB struct {
	conn      *sqlx.DB
	dbPath    string
	mu        sync.Mutex
	topicTree *topicTree
}

func (d *DB) invalidateTopicCacheLocked() {
	d.topicTree = nil
}

func (d *DB) getTopicTreeLocked(q sqlx.Queryer) (*topicTree, error) {
	if d.topicTree != nil {
		return d.topicTree, nil
	}
	if q == nil {
		q = d.conn
	}
	tree, err := loadTopicTree(q)
	if err != nil {
		return nil, err
	}
	d.topicTree = tree
	return tree, nil
}

func Open(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create db directory %q: %w", dir, err)
	}

	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-16000)&_pragma=mmap_size(67108864)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// Single writer connection pool for daemon guarantees 0 contention
	conn.SetMaxOpenConns(1)

	db := &DB{conn: sqlx.NewDb(conn, "sqlite"), dbPath: dbPath}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to migrate db: %w", err)
	}

	return db, nil
}

func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.invalidateTopicCacheLocked()
	// Flush any pending WAL frames and truncate WAL to zero out disk footprint
	_, _ = d.conn.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return d.conn.Close()
}

// Checkpoint runs a WAL checkpoint (PASSIVE, RESTART, or TRUNCATE) to keep WAL lean.
func (d *DB) Checkpoint(mode string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch mode {
	case "PASSIVE", "RESTART", "TRUNCATE":
	default:
		mode = "PASSIVE"
	}
	_, err := d.conn.Exec(fmt.Sprintf("PRAGMA wal_checkpoint(%s);", mode))
	return err
}

const CurrentSchemaVersion = 4

func (d *DB) migrate() error {
	var userVersion int
	if err := d.conn.QueryRow("PRAGMA user_version;").Scan(&userVersion); err != nil {
		return fmt.Errorf("failed to read PRAGMA user_version: %w", err)
	}

	var tableCount int
	if err := d.conn.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tasks';").Scan(&tableCount); err != nil {
		return fmt.Errorf("failed to query sqlite_master table count: %w", err)
	}

	if userVersion == 0 && tableCount > 0 {
		userVersion = 1
		_, _ = d.conn.Exec("PRAGMA user_version = 1;")
	}

	if userVersion < CurrentSchemaVersion && tableCount > 0 {
		backupDir := filepath.Join(filepath.Dir(d.dbPath), "backups")
		_ = os.MkdirAll(backupDir, 0o755)
		snapshotPath := filepath.Join(backupDir, fmt.Sprintf("pre-upgrade-v%d-%s.db", userVersion, time.Now().Format("20060102-150405")))

		if err := d.IntegrityCheck(); err != nil {
			return fmt.Errorf("cannot migrate corrupted database: %w", err)
		}

		if err := d.backupLocked(snapshotPath); err != nil {
			return fmt.Errorf("failed to create pre-upgrade safety snapshot: %w", err)
		}
	}

	// Schema v3 migration: ensure archived_at column and index exist for pre-existing databases before schema exec
	if userVersion < 3 && tableCount > 0 {
		hasArchivedAt, err := d.hasColumn("tasks", "archived_at")
		if err != nil {
			return fmt.Errorf("migration to v3 failed (inspect tasks): %w", err)
		}
		if !hasArchivedAt {
			if _, err := d.conn.Exec("ALTER TABLE tasks ADD COLUMN archived_at DATETIME;"); err != nil {
				return fmt.Errorf("migration to v3 failed (add archived_at): %w", err)
			}
		}
		if _, err := d.conn.Exec("CREATE INDEX IF NOT EXISTS idx_tasks_archived ON tasks(archived_at);"); err != nil {
			return fmt.Errorf("migration to v3 failed (create index idx_tasks_archived): %w", err)
		}
	}

	// Schema v4 migration: item kind (task/post) and topic membership columns.
	if userVersion < 4 && tableCount > 0 {
		for _, col := range []struct{ name, ddl string }{
			{"kind", "ALTER TABLE tasks ADD COLUMN kind TEXT NOT NULL DEFAULT 'task';"},
			{"topic_id", "ALTER TABLE tasks ADD COLUMN topic_id INTEGER REFERENCES topics(id) ON DELETE SET NULL;"},
			{"orphaned_at", "ALTER TABLE tasks ADD COLUMN orphaned_at DATETIME;"},
		} {
			has, err := d.hasColumn("tasks", col.name)
			if err != nil {
				return fmt.Errorf("migration to v4 failed (inspect tasks): %w", err)
			}
			if !has {
				if _, err := d.conn.Exec(col.ddl); err != nil {
					return fmt.Errorf("migration to v4 failed (add %s): %w", col.name, err)
				}
			}
		}
	}

	schema := `
	CREATE TABLE IF NOT EXISTS topics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		slug TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT 'tasks',
		parent_id INTEGER REFERENCES topics(id) ON DELETE RESTRICT,
		position INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		archived_at DATETIME
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_topics_parent_slug ON topics(COALESCE(parent_id, 0), slug);

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
		archived_at DATETIME,
		commit_sha TEXT NOT NULL DEFAULT '',
		commit_msg TEXT NOT NULL DEFAULT '',
		diff_stat TEXT NOT NULL DEFAULT '',
		diff_content TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT 'task',
		topic_id INTEGER REFERENCES topics(id) ON DELETE SET NULL,
		orphaned_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_scope_repo ON tasks(scope, repo_path, status);
	CREATE INDEX IF NOT EXISTS idx_tasks_scope_repo_seq ON tasks(scope, repo_path, seq_num DESC);
	CREATE INDEX IF NOT EXISTS idx_tasks_global_seq ON tasks(scope, seq_num DESC);
	CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_tasks_archived ON tasks(archived_at);
	CREATE INDEX IF NOT EXISTS idx_tasks_topic ON tasks(topic_id);

	CREATE TABLE IF NOT EXISTS task_notes (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		author TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_notes_task ON task_notes(task_id, created_at ASC);

	CREATE TABLE IF NOT EXISTS task_sequences (
		scope TEXT NOT NULL,
		repo_path TEXT NOT NULL DEFAULT '',
		last_seq INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (scope, repo_path)
	);

	INSERT OR IGNORE INTO task_sequences (scope, repo_path, last_seq)
	SELECT scope, repo_path, COALESCE(MAX(seq_num), 0)
	FROM tasks
	WHERE kind = 'task'
	GROUP BY scope, repo_path;

	CREATE TABLE IF NOT EXISTS post_sequences (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		last_seq INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS task_tombstones (
		id TEXT PRIMARY KEY,
		scope TEXT NOT NULL,
		repo_path TEXT NOT NULL DEFAULT '',
		task_json TEXT NOT NULL,
		deleted_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_tombstones_scope_repo ON task_tombstones(scope, repo_path, deleted_at DESC);

	CREATE TABLE IF NOT EXISTS task_dependencies (
		task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		depends_on_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		created_at DATETIME NOT NULL,
		PRIMARY KEY (task_id, depends_on_id)
	);

	CREATE INDEX IF NOT EXISTS idx_deps_task ON task_dependencies(task_id);
	CREATE INDEX IF NOT EXISTS idx_deps_parent ON task_dependencies(depends_on_id);

	CREATE TABLE IF NOT EXISTS task_links (
		from_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		target TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		PRIMARY KEY (from_id, target)
	);

	CREATE INDEX IF NOT EXISTS idx_links_target ON task_links(target);
	`

	if _, err := d.conn.Exec(schema); err != nil {
		return err
	}

	// v4 backfill: convert the legacy "orphaned" tag into orphaned_at, then build
	// the wiki-link index for all pre-existing items.
	if userVersion < 4 && tableCount > 0 {
		if _, err := d.conn.Exec(`
			UPDATE tasks SET
				orphaned_at = COALESCE(orphaned_at, updated_at),
				tags = (SELECT COALESCE(json_group_array(value), '[]') FROM json_each(tasks.tags) WHERE lower(value) NOT IN ('orphaned', 'orphan'))
			WHERE json_valid(tags) AND EXISTS (SELECT 1 FROM json_each(tasks.tags) WHERE lower(value) IN ('orphaned', 'orphan'))
		`); err != nil {
			return fmt.Errorf("migration to v4 failed (orphan marker): %w", err)
		}
		if err := d.reindexAllLinks(); err != nil {
			return fmt.Errorf("migration to v4 failed (index links): %w", err)
		}
	}

	if _, err := d.conn.Exec(fmt.Sprintf("PRAGMA user_version = %d;", CurrentSchemaVersion)); err != nil {
		return err
	}

	return d.QuickCheck()
}

// hasColumn reports whether table has a column with the given name.
func (d *DB) hasColumn(table, column string) (bool, error) {
	rows, err := d.conn.Query(fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
