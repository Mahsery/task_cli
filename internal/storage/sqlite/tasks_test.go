package sqlite

import (
	"fmt"
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

func setupBenchDB(b *testing.B) (*DB, func()) {
	b.Helper()
	tmpDir, err := os.MkdirTemp("", "tk-bench-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "bench.db")
	db, err := Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		b.Fatalf("failed to open bench db: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
	return db, cleanup
}

func BenchmarkCreateTask(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.CreateTask("local", "/repo/test", "Benchmark Task", "Benchmarking description", domain.StatusOpen, domain.PriorityNormal, []string{"bench"}, "Benchmarker", "")
		if err != nil {
			b.Fatalf("create failed at %d: %v", i, err)
		}
	}
}

func BenchmarkListTasks(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	// Seed 500 tasks
	for i := 0; i < 500; i++ {
		_, err := db.CreateTask("local", "/repo/test", fmt.Sprintf("Task %d", i), "Desc", domain.StatusOpen, domain.PriorityNormal, []string{"bench"}, "Benchmarker", "")
		if err != nil {
			b.Fatalf("seed failed: %v", err)
		}
	}

	filter := domain.TaskFilter{Scope: "local", RepoPath: "/repo/test"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks, err := db.ListTasks(filter)
		if err != nil {
			b.Fatalf("list failed: %v", err)
		}
		if len(tasks) != 500 {
			b.Fatalf("expected 500 tasks, got %d", len(tasks))
		}
	}
}

func BenchmarkGetTask(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo/test", "Task 1", "Desc", domain.StatusOpen, domain.PriorityNormal, []string{"bench"}, "Benchmarker", "")
	if err != nil {
		b.Fatalf("seed failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetTask(task.ID)
		if err != nil {
			b.Fatalf("get failed: %v", err)
		}
	}
}

func TestGlobalTaskSequence(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	t1, err := db.CreateTask("global", "/path/one", "Global Task 1", "", domain.StatusOpen, domain.PriorityNormal, nil, "User1", "")
	if err != nil {
		t.Fatalf("create t1 failed: %v", err)
	}
	if t1.ID != "G-1" {
		t.Fatalf("expected ID G-1, got %s", t1.ID)
	}

	t2, err := db.CreateTask("global", "/path/two", "Global Task 2", "", domain.StatusOpen, domain.PriorityNormal, nil, "User2", "")
	if err != nil {
		t.Fatalf("create t2 failed: %v", err)
	}
	if t2.ID != "G-2" {
		t.Fatalf("expected ID G-2, got %s", t2.ID)
	}

	t3, err := db.CreateTask("global", "", "Global Task 3", "", domain.StatusOpen, domain.PriorityNormal, nil, "User3", "")
	if err != nil {
		t.Fatalf("create t3 failed: %v", err)
	}
	if t3.ID != "G-3" {
		t.Fatalf("expected ID G-3, got %s", t3.ID)
	}
}

func TestDeleteTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo/test", "Task to delete", "", domain.StatusOpen, domain.PriorityNormal, nil, "User", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	_, err = db.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get before delete failed: %v", err)
	}

	err = db.DeleteTask(task.ID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = db.GetTask(task.ID)
	if err == nil {
		t.Fatalf("expected error getting deleted task, got nil")
	}

	// Deleting again should return error
	err = db.DeleteTask(task.ID)
	if err == nil {
		t.Fatalf("expected error deleting non-existent task, got nil")
	}
}

func TestListTasksFiltersAndNotes(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Task 1: user1, bug tag
	t1, err := db.CreateTask("local", "/repo/test", "Task One", "Desc 1", domain.StatusOpen, domain.PriorityHigh, []string{"bug", "frontend"}, "Dev", "user1")
	if err != nil {
		t.Fatalf("create t1 failed: %v", err)
	}
	note := "First note on Task One"
	_, err = db.UpdateTask(t1.ID, nil, nil, nil, nil, nil, nil, &note, "Dev")
	if err != nil {
		t.Fatalf("update t1 failed: %v", err)
	}

	// Task 2: user2, bugfix tag (should not match "bug" tag filter!)
	_, err = db.CreateTask("local", "/repo/test", "Task Two", "Desc 2", domain.StatusInProgress, domain.PriorityNormal, []string{"bugfix"}, "Dev", "user2")
	if err != nil {
		t.Fatalf("create t2 failed: %v", err)
	}

	// Filter by Assignee "user1"
	tasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo/test", Assignee: "user1"})
	if err != nil {
		t.Fatalf("filter by assignee failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != t1.ID {
		t.Fatalf("expected 1 task with ID %s, got %d tasks", t1.ID, len(tasks))
	}
	// Verify notes are populated by ListTasks!
	if len(tasks[0].Notes) != 1 || tasks[0].Notes[0].Content != note {
		t.Fatalf("expected note %q in ListTasks result, got %+v", note, tasks[0].Notes)
	}

	// Filter by Tag "bug" - exact match, should NOT match "bugfix"
	bugTasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo/test", Tag: "bug"})
	if err != nil {
		t.Fatalf("filter by tag failed: %v", err)
	}
	if len(bugTasks) != 1 || bugTasks[0].ID != t1.ID {
		t.Fatalf("expected exactly 1 task matching tag 'bug', got %d", len(bugTasks))
	}

	// Filter by Status "in_progress"
	progStatus := domain.StatusInProgress
	progTasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo/test", Status: &progStatus})
	if err != nil {
		t.Fatalf("filter by status failed: %v", err)
	}
	if len(progTasks) != 1 || progTasks[0].Title != "Task Two" {
		t.Fatalf("expected 1 in_progress task, got %d", len(progTasks))
	}
}

func TestSearchTagsAndDSL(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := db.CreateTask("local", "/repo/test", "Compiler Refactor", "Description here", domain.StatusOpen, domain.PriorityHigh, []string{"compiler", "odin"}, "Dev", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	_, err = db.CreateTask("local", "/repo/test", "UI Fixes", "Button styling", domain.StatusOpen, domain.PriorityNormal, []string{"frontend", "odin"}, "Dev", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// 1. Search for a word that ONLY appears in tags: "odin"
	odinTasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo/test", Search: "odin"})
	if err != nil {
		t.Fatalf("search odin failed: %v", err)
	}
	if len(odinTasks) != 2 {
		t.Fatalf("expected 2 tasks with tag 'odin', got %d", len(odinTasks))
	}

	// 2. Boolean AND search: "compiler AND odin"
	andTasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo/test", Search: "compiler AND odin"})
	if err != nil {
		t.Fatalf("search compiler AND odin failed: %v", err)
	}
	if len(andTasks) != 1 || andTasks[0].Title != "Compiler Refactor" {
		t.Fatalf("expected 1 task matching 'compiler AND odin', got %d", len(andTasks))
	}

	// 3. Boolean NOT search: "odin NOT compiler"
	notTasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo/test", Search: "odin NOT compiler"})
	if err != nil {
		t.Fatalf("search odin NOT compiler failed: %v", err)
	}
	if len(notTasks) != 1 || notTasks[0].Title != "UI Fixes" {
		t.Fatalf("expected 1 task matching 'odin NOT compiler', got %d", len(notTasks))
	}
}



