package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupMeta struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}
	_ = os.Remove(targetPath)

	escaped := strings.ReplaceAll(targetPath, "'", "''")
	query := fmt.Sprintf("VACUUM INTO '%s';", escaped)
	if _, err := d.conn.Exec(query); err != nil {
		return fmt.Errorf("failed to vacuum into %q: %w", targetPath, err)
	}
	return nil
}

// CreateRollingBackup creates a named or timestamped backup and prunes snapshots exceeding maxSnapshots.
func (d *DB) CreateRollingBackup(name string, maxSnapshots int) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if maxSnapshots <= 0 {
		maxSnapshots = 5
	}

	backupDir := filepath.Join(filepath.Dir(d.dbPath), "backups")
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("backup-%s.db", time.Now().Format("20060102-150405"))
	}
	if !strings.HasSuffix(name, ".db") {
		name += ".db"
	}
	targetPath := filepath.Join(backupDir, name)

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

// ListBackups returns all available snapshots in the database's backups directory.
func (d *DB) ListBackups() ([]BackupMeta, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	backupDir := filepath.Join(filepath.Dir(d.dbPath), "backups")
	entries, err := os.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return []BackupMeta{}, nil
	}
	if err != nil {
		return nil, err
	}

	var backups []BackupMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, BackupMeta{
			Name:    entry.Name(),
			Path:    filepath.Join(backupDir, entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ModTime.After(backups[j].ModTime)
	})

	return backups, nil
}
