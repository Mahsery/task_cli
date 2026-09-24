package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"task_cli/internal/domain"
)

func BenchmarkRestoreTask(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	task := &domain.Task{
		ID:          "T-100",
		Scope:       "local",
		RepoPath:    "/repo/test",
		Title:       "Restored Task",
		Description: "Detailed description of task",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityNormal,
		Tags:        []string{"tag1", "tag2"},
		Author:      "Benchmarker",
		Assignee:    "Worker",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Notes: []domain.TaskNote{
			{ID: "n-1", Author: "Benchmarker", Content: "Note 1", CreatedAt: time.Now()},
			{ID: "n-2", Author: "Benchmarker", Content: "Note 2", CreatedAt: time.Now()},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task.ID = fmt.Sprintf("T-%d", i+1)
		task.Notes = []domain.TaskNote{
			{ID: fmt.Sprintf("n-%d-1", i+1), Author: "Benchmarker", Content: "Note 1", CreatedAt: time.Now()},
			{ID: fmt.Sprintf("n-%d-2", i+1), Author: "Benchmarker", Content: "Note 2", CreatedAt: time.Now()},
		}
		if err := db.RestoreTask(task); err != nil {
			b.Fatalf("restore failed at %d: %v", i, err)
		}
	}
}

func BenchmarkBackupDB(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	// Seed 100 tasks with notes
	for i := 0; i < 100; i++ {
		t, err := db.CreateTask("local", "/repo/test", fmt.Sprintf("Task %d", i), "Desc", domain.StatusOpen, domain.PriorityNormal, []string{"bench"}, "Benchmarker", "")
		if err != nil {
			b.Fatalf("seed failed: %v", err)
		}
		note := "Progress note"
		_, _ = db.UpdateTask(t.ID, nil, nil, nil, nil, nil, nil, nil, nil, &note, "Benchmarker")
	}

	tmpDir := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		targetPath := filepath.Join(tmpDir, fmt.Sprintf("backup-%d.db", i))
		if err := db.Backup(targetPath); err != nil {
			b.Fatalf("backup failed: %v", err)
		}
		_ = os.Remove(targetPath)
	}
}
