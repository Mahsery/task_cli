package sqlite

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"task_cli/internal/domain"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "tk-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open test db: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
	return db, cleanup
}

func TestCreateAndGetTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo/test", "Test Task 1", "Description here", domain.StatusOpen, domain.PriorityHigh, []string{"bug"}, "Alice <alice@test.com>", "")
	if err != nil {
		t.Fatalf("unexpected error creating task: %v", err)
	}

	if task.ID != "T-1" {
		t.Errorf("expected ID T-1, got %s", task.ID)
	}
	if task.Title != "Test Task 1" {
		t.Errorf("expected title 'Test Task 1', got %s", task.Title)
	}

	// Fetch task
	fetched, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatalf("unexpected error fetching task: %v", err)
	}
	if fetched.ID != task.ID {
		t.Errorf("expected ID %s, got %s", task.ID, fetched.ID)
	}
}

func TestUpdateTaskAndNotes(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo/test", "Initial", "Desc", domain.StatusOpen, domain.PriorityNormal, nil, "Bob", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	newTitle := "Updated Title"
	newStatus := domain.StatusInProgress
	note := "Working on this now"
	updated, err := db.UpdateTask(task.ID, &newTitle, nil, &newStatus, nil, nil, nil, &note, "Bob")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if updated.Title != newTitle {
		t.Errorf("expected title %s, got %s", newTitle, updated.Title)
	}
	if updated.Status != domain.StatusInProgress {
		t.Errorf("expected status %s, got %s", domain.StatusInProgress, updated.Status)
	}
	if len(updated.Notes) != 1 || updated.Notes[0].Content != note {
		t.Errorf("expected 1 note with content %q, got %+v", note, updated.Notes)
	}
}

func TestAttachGitAndMarkDone(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo/test", "Git Task", "", domain.StatusOpen, domain.PriorityNormal, nil, "Dev", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	sha := "4a3f9e1"
	subject := "Fix issue"
	stat := "1 file changed, 5 insertions(+)"
	diff := "diff --git a/main.go b/main.go..."

	updated, err := db.AttachGit(task.ID, sha, subject, stat, diff, true)
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	if updated.Status != domain.StatusDone {
		t.Errorf("expected status done, got %s", updated.Status)
	}
	if updated.CommitSHA != sha {
		t.Errorf("expected sha %s, got %s", sha, updated.CommitSHA)
	}
	if updated.DiffStat != stat {
		t.Errorf("expected stat %s, got %s", stat, updated.DiffStat)
	}
	if updated.ClosedAt == nil {
		t.Error("expected closed_at to be populated")
	}
}

func TestConcurrentWrites(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const numWorkers = 30
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := db.CreateTask("local", "/repo/test", "Concurrent Task", "", domain.StatusOpen, domain.PriorityNormal, []string{"test"}, "Worker", "")
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent create failed: %v", err)
	}

	tasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo/test"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if len(tasks) != numWorkers {
		t.Fatalf("expected %d tasks, got %d", numWorkers, len(tasks))
	}
}
