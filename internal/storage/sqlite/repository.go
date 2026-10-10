package sqlite

import "fmt"

// OpenRepository binds a repository database to its current location.
func OpenRepository(dbPath, repoPath string) (*DB, error) {
	db, err := Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.relocateRepository(repoPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) relocateRepository(repoPath string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin repository relocation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Keep the high-water mark even when the latest tasks have been deleted.
	_, err = tx.Exec(`
		INSERT INTO task_sequences (scope, repo_path, last_seq)
		SELECT 'local', ?, MAX(seq) FROM (
			SELECT COALESCE(MAX(last_seq), 0) AS seq FROM task_sequences WHERE scope = 'local'
			UNION ALL SELECT COALESCE(MAX(seq_num), 0) FROM tasks WHERE scope = 'local'
			UNION ALL SELECT COALESCE(MAX(CAST(substr(id, 3) AS INTEGER)), 0)
			FROM task_tombstones WHERE scope = 'local'
		) WHERE true
		ON CONFLICT(scope, repo_path) DO UPDATE SET last_seq = MAX(last_seq, excluded.last_seq)
	`, repoPath)
	if err != nil {
		return fmt.Errorf("preserve repository sequence: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM task_sequences WHERE scope = 'local' AND repo_path != ?`, repoPath); err != nil {
		return fmt.Errorf("remove old repository sequences: %w", err)
	}
	if _, err := tx.Exec(`UPDATE tasks SET repo_path = ? WHERE scope = 'local' AND repo_path != ?`, repoPath, repoPath); err != nil {
		return fmt.Errorf("relocate repository tasks: %w", err)
	}
	if _, err := tx.Exec(`
		UPDATE task_tombstones SET repo_path = ?, task_json = json_set(task_json, '$.repo_path', ?)
		WHERE scope = 'local' AND repo_path != ?
	`, repoPath, repoPath, repoPath); err != nil {
		return fmt.Errorf("relocate deleted repository tasks: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit repository relocation: %w", err)
	}
	return nil
}
