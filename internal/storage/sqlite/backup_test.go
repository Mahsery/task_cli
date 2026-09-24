package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"task_cli/internal/domain"
)

func TestBackupAndIntegrity(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Create tasks and notes
	t1, err := db.CreateTask("local", "/repo", "Task For Backup 1", "Desc 1", domain.StatusOpen, domain.PriorityHigh, []string{"backup"}, "Alice", "")
	if err != nil {
		t.Fatalf("create t1 failed: %v", err)
	}

	note := "Important progress note before backup"
	_, err = db.UpdateTask(t1.ID, nil, nil, nil, nil, nil, nil, nil, nil, &note, "Alice")
	if err != nil {
		t.Fatalf("update t1 failed: %v", err)
	}

	// 2. Run integrity check on active db
	if err := db.IntegrityCheck(); err != nil {
		t.Fatalf("IntegrityCheck failed: %v", err)
	}
	if err := db.QuickCheck(); err != nil {
		t.Fatalf("QuickCheck failed: %v", err)
	}

	// 3. Create backup snapshot
	backupDir := t.TempDir()
	backupFile := filepath.Join(backupDir, "manual-snapshot.db")
	if err := db.Backup(backupFile); err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// 4. Verify backup file exists and is readable
	info, err := os.Stat(backupFile)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected valid non-empty backup file, got stat: %v", err)
	}

	// 5. Mutate/delete task in active DB
	if err := db.DeleteTask(t1.ID); err != nil {
		t.Fatalf("delete t1 failed: %v", err)
	}
	if _, err := db.GetTask(t1.ID); err == nil {
		t.Fatal("expected t1 to be deleted in active DB")
	}

	// 6. Open backup DB directly and verify t1 is intact
	backupDB, err := Open(backupFile)
	if err != nil {
		t.Fatalf("failed to open backup database: %v", err)
	}
	defer backupDB.Close()

	if err := backupDB.IntegrityCheck(); err != nil {
		t.Fatalf("backup database failed integrity check: %v", err)
	}

	restoredTask, err := backupDB.GetTask(t1.ID)
	if err != nil {
		t.Fatalf("failed to retrieve task from backup: %v", err)
	}
	if restoredTask.Title != "Task For Backup 1" {
		t.Errorf("expected title %q, got %q", "Task For Backup 1", restoredTask.Title)
	}
	if len(restoredTask.Notes) != 1 || restoredTask.Notes[0].Content != note {
		t.Errorf("expected note %q in backup, got %+v", note, restoredTask.Notes)
	}
}

func TestRollingBackupPruning(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.CreateTask("local", "/repo", "Rolling Task", "", domain.StatusOpen, domain.PriorityNormal, nil, "Dev", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Create 5 backups with maxSnapshots = 3
	for i := 1; i <= 5; i++ {
		name := fmt.Sprintf("test-snap-%d.db", i)
		_, err := db.CreateRollingBackup(name, 3)
		if err != nil {
			t.Fatalf("CreateRollingBackup %d failed: %v", i, err)
		}
		// Brief sleep so ModTime differs slightly for predictable ordering
		time.Sleep(10 * time.Millisecond)
	}

	backups, err := db.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != 3 {
		t.Fatalf("expected exactly 3 backups after pruning, got %d", len(backups))
	}

	// Oldest backups (1 and 2) must have been pruned; newest (5, 4, 3) retained
	names := make(map[string]bool)
	for _, b := range backups {
		names[b.Name] = true
	}

	if !names["test-snap-5.db"] || !names["test-snap-4.db"] || !names["test-snap-3.db"] {
		t.Errorf("expected test-snap-5, 4, 3 to be retained, got: %+v", names)
	}
	if names["test-snap-1.db"] || names["test-snap-2.db"] {
		t.Errorf("expected test-snap-1 and 2 to be pruned, got: %+v", names)
	}
}
