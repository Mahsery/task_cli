package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupMeta struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Location string    `json:"location,omitempty"`
}

// Path returns the filesystem path of the database.
func (d *DB) Path() string {
	return d.dbPath
}

// IntegrityCheck runs SQLite PRAGMA integrity_check.
func (d *DB) IntegrityCheck() error {
	var status string
	err := d.conn.QueryRow("PRAGMA integrity_check;").Scan(&status)
	if err != nil {
		return fmt.Errorf("integrity check query failed: %w", err)
	}
	if status != "ok" {
		return fmt.Errorf("sqlite integrity check failed: %s", status)
	}
	return nil
}

// QuickCheck runs SQLite PRAGMA quick_check.
func (d *DB) QuickCheck() error {
	var status string
	err := d.conn.QueryRow("PRAGMA quick_check;").Scan(&status)
	if err != nil {
		return fmt.Errorf("quick check query failed: %w", err)
	}
	if status != "ok" {
		return fmt.Errorf("sqlite quick check failed: %s", status)
	}
	return nil
}

// Backup creates a point-in-time snapshot of the database using VACUUM INTO.
func (d *DB) Backup(targetPath string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.backupLocked(targetPath)
}

func (d *DB) backupLocked(targetPath string) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	tmpTarget := fmt.Sprintf("%s.tmp-%d", targetPath, time.Now().UnixNano())
	_ = os.Remove(tmpTarget)

	// VACUUM INTO accepts an expression, so the target path is bound as a parameter.
	if _, err := d.conn.Exec("VACUUM INTO ?", tmpTarget); err != nil {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("failed to vacuum into %q: %w", targetPath, err)
	}

	if err := os.Rename(tmpTarget, targetPath); err != nil {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("failed to move snapshot into place: %w", err)
	}
	return nil
}

// CreateRollingBackup creates a named or timestamped backup in the default backups directory and prunes older snapshots.
func (d *DB) CreateRollingBackup(name string, maxSnapshots int) (string, error) {
	backupDir := filepath.Join(filepath.Dir(d.dbPath), "backups")
	return d.CreateRollingBackupInDir(backupDir, name, maxSnapshots)
}

// CreateRollingBackupInDir creates a named or timestamped snapshot in the specified directory and prunes older snapshots.
func (d *DB) CreateRollingBackupInDir(backupDir, name string, maxSnapshots int) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if maxSnapshots <= 0 {
		maxSnapshots = 5
	}

	cleaned := filepath.Base(filepath.Clean(strings.TrimSpace(name)))
	if cleaned == "" || cleaned == "." {
		cleaned = fmt.Sprintf("backup-%s-%03d.db", time.Now().Format("20060102-150405"), time.Now().Nanosecond()/1e6)
	}
	if !strings.HasSuffix(cleaned, ".db") {
		cleaned += ".db"
	}
	targetPath := filepath.Join(backupDir, cleaned)

	if err := d.backupLocked(targetPath); err != nil {
		return "", err
	}

	// Prune older snapshots if total > maxSnapshots
	entries, err := os.ReadDir(backupDir)
	if err == nil {
		var dbs []os.FileInfo
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".db") {
				if info, err := e.Info(); err == nil {
					dbs = append(dbs, info)
				}
			}
		}
		if len(dbs) > maxSnapshots {
			sort.Slice(dbs, func(i, j int) bool {
				return dbs[i].ModTime().After(dbs[j].ModTime())
			})
			for _, old := range dbs[maxSnapshots:] {
				_ = os.Remove(filepath.Join(backupDir, old.Name()))
			}
		}
	}

	return targetPath, nil
}

// ListBackups returns all available snapshots in the database's default local backups directory.
func (d *DB) ListBackups() ([]BackupMeta, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	backupDir := filepath.Join(filepath.Dir(d.dbPath), "backups")
	return ListBackupsInDir(backupDir, "local")
}

// ListBackupsInDir scans the specified directory for .db snapshot files and returns their metadata.
func ListBackupsInDir(backupDir, location string) ([]BackupMeta, error) {
	entries, err := os.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return []BackupMeta{}, nil
	}
	if err != nil {
		return nil, err
	}

	var backups []BackupMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") || strings.Contains(entry.Name(), ".tmp-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, BackupMeta{
			Name:     entry.Name(),
			Path:     filepath.Join(backupDir, entry.Name()),
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Location: location,
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ModTime.After(backups[j].ModTime)
	})

	return backups, nil
}

// Resurrect safely restores an active in-memory database to disk when its underlying file was unlinked/deleted.
// It snapshots the live state via VACUUM INTO to a temporary file, closes the old connection to release the deleted inode,
// atomically renames the temporary file into targetPath, and reopens a fresh connection pointing to the newly created file.
func (d *DB) Resurrect(targetPath string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory for resurrect: %w", err)
	}

	tmpPath := fmt.Sprintf("%s.tmp-resurrect-%d", targetPath, time.Now().UnixNano())
	if err := d.backupLocked(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to backup live db to %s: %w", tmpPath, err)
	}

	// Close old connection pointing to unlinked descriptor
	_ = d.conn.Close()

	_ = os.Remove(targetPath + "-wal")
	_ = os.Remove(targetPath + "-shm")
	_ = os.Remove(targetPath)

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename resurrect file to %s: %w", targetPath, err)
	}

	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-16000)&_pragma=mmap_size(67108864)", targetPath)
	newConn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to reopen db after resurrect: %w", err)
	}
	newConn.SetMaxOpenConns(1)

	d.conn = newConn
	d.dbPath = targetPath
	return nil
}
