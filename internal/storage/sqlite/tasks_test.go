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
	updated, err := db.UpdateTask(task.ID, &newTitle, nil, &newStatus, nil, nil, nil, nil, nil, &note, "Bob")
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
	_, err = db.UpdateTask(t1.ID, nil, nil, nil, nil, nil, nil, nil, nil, &note, "Dev")
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

func TestMonotonicSequencesAfterDelete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	t1, err := db.CreateTask("local", "/repo/test", "Task 1", "", domain.StatusOpen, domain.PriorityNormal, nil, "Dev", "")
	if err != nil {
		t.Fatalf("create t1 failed: %v", err)
	}
	if t1.ID != "T-1" {
		t.Fatalf("expected T-1, got %s", t1.ID)
	}

	t2, err := db.CreateTask("local", "/repo/test", "Task 2", "", domain.StatusOpen, domain.PriorityNormal, nil, "Dev", "")
	if err != nil {
		t.Fatalf("create t2 failed: %v", err)
	}
	if t2.ID != "T-2" {
		t.Fatalf("expected T-2, got %s", t2.ID)
	}

	// Delete T-2 (the latest sequence)
	if err := db.DeleteTask(t2.ID); err != nil {
		t.Fatalf("delete t2 failed: %v", err)
	}

	// Next task created MUST be T-3, NOT reissuing T-2
	t3, err := db.CreateTask("local", "/repo/test", "Task 3", "", domain.StatusOpen, domain.PriorityNormal, nil, "Dev", "")
	if err != nil {
		t.Fatalf("create t3 failed: %v", err)
	}
	if t3.ID != "T-3" {
		t.Fatalf("expected monotonic ID T-3 after deleting T-2, but got %s (ID was reused!)", t3.ID)
	}

	// Delete all remaining tasks
	if err := db.DeleteTask(t1.ID); err != nil {
		t.Fatalf("delete t1 failed: %v", err)
	}
	if err := db.DeleteTask(t3.ID); err != nil {
		t.Fatalf("delete t3 failed: %v", err)
	}

	// With empty table, next task must still be T-4
	t4, err := db.CreateTask("local", "/repo/test", "Task 4", "", domain.StatusOpen, domain.PriorityNormal, nil, "Dev", "")
	if err != nil {
		t.Fatalf("create t4 failed: %v", err)
	}
	if t4.ID != "T-4" {
		t.Fatalf("expected monotonic ID T-4 on empty table, but got %s", t4.ID)
	}
}

func TestAtomicCompetingClaims(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo/test", "Claim Test Task", "", domain.StatusOpen, domain.PriorityNormal, nil, "Creator", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if task.Status != domain.StatusOpen || task.Assignee != "" {
		t.Fatalf("expected open unassigned task, got status=%s assignee=%s", task.Status, task.Assignee)
	}

	// Worker 1 claims task
	claimed1, err := db.ClaimTask(task.ID, "worker-1", false)
	if err != nil {
		t.Fatalf("worker-1 claim failed: %v", err)
	}
	if claimed1.Assignee != "worker-1" || claimed1.Status != domain.StatusInProgress {
		t.Fatalf("expected worker-1 and in_progress, got assignee=%s status=%s", claimed1.Assignee, claimed1.Status)
	}
	if len(claimed1.Notes) != 1 {
		t.Fatalf("expected 1 claim attribution note, got %d", len(claimed1.Notes))
	}

	// Worker 2 attempts competing claim without force -> MUST FAIL
	_, err = db.ClaimTask(task.ID, "worker-2", false)
	if err == nil {
		t.Fatalf("competing claim without force should have failed, but succeeded")
	}

	// Worker 1 re-claims task (same assignee, idempotent) -> MUST SUCCEED
	_, err = db.ClaimTask(task.ID, "worker-1", false)
	if err != nil {
		t.Fatalf("worker-1 re-claiming own task should succeed, got: %v", err)
	}

	// Worker 2 forces claim with force=true -> MUST SUCCEED
	forced, err := db.ClaimTask(task.ID, "worker-2", true)
	if err != nil {
		t.Fatalf("worker-2 force claim failed: %v", err)
	}
	if forced.Assignee != "worker-2" {
		t.Fatalf("expected assignee worker-2 after forced claim, got %s", forced.Assignee)
	}
}

func TestConcurrentTagAdditions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo/test", "Tag Test Task", "", domain.StatusOpen, domain.PriorityNormal, []string{"base"}, "Dev", "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	const numWorkers = 20
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tag := fmt.Sprintf("tag-%02d", idx)
			_, err := db.UpdateTask(task.ID, nil, nil, nil, nil, nil, []string{tag}, nil, nil, nil, "Dev")
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent tag addition failed: %v", err)
	}

	finalTask, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}

	// Must have 1 base tag + 20 concurrent tags = 21 tags
	if len(finalTask.Tags) != numWorkers+1 {
		t.Fatalf("expected %d tags, but got %d: %v", numWorkers+1, len(finalTask.Tags), finalTask.Tags)
	}

	// Test atomic removal
	removeTag := "tag-00"
	removed, err := db.UpdateTask(task.ID, nil, nil, nil, nil, nil, nil, []string{removeTag}, nil, nil, "Dev")
	if err != nil {
		t.Fatalf("remove tag failed: %v", err)
	}
	if len(removed.Tags) != numWorkers {
		t.Fatalf("expected %d tags after removing %s, got %d", numWorkers, removeTag, len(removed.Tags))
	}
	for _, tg := range removed.Tags {
		if tg == removeTag {
			t.Fatalf("tag %s was not removed", removeTag)
		}
	}
}

func TestListTasksDefaultOrdering(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create tasks in disordered sequence:
	// T-1: done, normal
	// T-2: open, low
	// T-3: in_progress, high
	// T-4: in_progress, urgent
	// T-5: open, high
	// T-6: in_progress, high (created after T-3)
	// T-7: closed, urgent
	_, _ = db.CreateTask("local", "/repo", "T1", "", domain.StatusDone, domain.PriorityNormal, nil, "author", "")
	_, _ = db.CreateTask("local", "/repo", "T2", "", domain.StatusOpen, domain.PriorityLow, nil, "author", "")
	_, _ = db.CreateTask("local", "/repo", "T3", "", domain.StatusInProgress, domain.PriorityHigh, nil, "author", "")
	_, _ = db.CreateTask("local", "/repo", "T4", "", domain.StatusInProgress, domain.PriorityUrgent, nil, "author", "")
	_, _ = db.CreateTask("local", "/repo", "T5", "", domain.StatusOpen, domain.PriorityHigh, nil, "author", "")
	_, _ = db.CreateTask("local", "/repo", "T6", "", domain.StatusInProgress, domain.PriorityHigh, nil, "author", "")
	_, _ = db.CreateTask("local", "/repo", "T7", "", domain.StatusClosed, domain.PriorityUrgent, nil, "author", "")

	// 1. Default ordering: status (in_progress, open, done, closed) -> priority (urgent, high, normal, low) -> id (seq_num ASC)
	tasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo"})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	expectedOrder := []string{"T-4", "T-3", "T-6", "T-5", "T-2", "T-1", "T-7"}
	// T-4: in_progress, urgent
	// T-3: in_progress, high (id 3)
	// T-6: in_progress, high (id 6)
	// T-5: open, high
	// T-2: open, low
	// T-1: done, normal
	// T-7: closed, urgent

	if len(tasks) != len(expectedOrder) {
		t.Fatalf("expected %d tasks, got %d", len(expectedOrder), len(tasks))
	}

	for i, expectedID := range expectedOrder {
		if tasks[i].ID != expectedID {
			t.Errorf("at index %d: expected task %s (%s, %s), got %s (%s, %s)",
				i, expectedID, expectedOrder, tasks[i].Status, tasks[i].ID, tasks[i].Status, tasks[i].Priority)
		}
	}

	// 2. Custom sort: "recent" (seq_num DESC)
	tasksRecent, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo", Sort: "recent"})
	if err != nil {
		t.Fatalf("ListTasks recent failed: %v", err)
	}
	if tasksRecent[0].ID != "T-7" || tasksRecent[len(tasksRecent)-1].ID != "T-1" {
		t.Errorf("expected recent sort from T-7 to T-1, got first=%s, last=%s", tasksRecent[0].ID, tasksRecent[len(tasksRecent)-1].ID)
	}

	// 3. Custom sort: "id" (seq_num ASC)
	tasksID, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/repo", Sort: "id"})
	if err != nil {
		t.Fatalf("ListTasks id failed: %v", err)
	}
	if tasksID[0].ID != "T-1" || tasksID[len(tasksID)-1].ID != "T-7" {
		t.Errorf("expected id sort from T-1 to T-7, got first=%s, last=%s", tasksID[0].ID, tasksID[len(tasksID)-1].ID)
	}
}

func TestEmptyTitleValidation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Create with empty or whitespace title should fail
	_, err := db.CreateTask("local", "/repo", "", "Desc", domain.StatusOpen, domain.PriorityNormal, nil, "Author", "")
	if err == nil {
		t.Fatal("expected CreateTask with empty title to fail, but it succeeded")
	}

	_, err = db.CreateTask("local", "/repo", "   ", "Desc", domain.StatusOpen, domain.PriorityNormal, nil, "Author", "")
	if err == nil {
		t.Fatal("expected CreateTask with whitespace title to fail, but it succeeded")
	}

	// 2. Create valid task, then try to update with blank title
	task, err := db.CreateTask("local", "/repo", "Valid Title", "Desc", domain.StatusOpen, domain.PriorityNormal, nil, "Author", "")
	if err != nil {
		t.Fatalf("unexpected error creating task: %v", err)
	}

	blank := ""
	_, err = db.UpdateTask(task.ID, &blank, nil, nil, nil, nil, nil, nil, nil, nil, "Tester")
	if err == nil {
		t.Fatal("expected UpdateTask with blank title to fail, but it succeeded")
	}

	spaces := "   "
	_, err = db.UpdateTask(task.ID, &spaces, nil, nil, nil, nil, nil, nil, nil, nil, "Tester")
	if err == nil {
		t.Fatal("expected UpdateTask with whitespace title to fail, but it succeeded")
	}
}

func TestRestoreTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo/test", "Original Task", "Original Desc", domain.StatusInProgress, domain.PriorityHigh, []string{"bug", "ui"}, "Tester", "Worker")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	noteContent := "Working on fix"
	taskWithNote, err := db.UpdateTask(task.ID, nil, nil, nil, nil, nil, nil, nil, nil, &noteContent, "Worker")
	if err != nil {
		t.Fatalf("add note failed: %v", err)
	}
	task = taskWithNote

	// Delete task
	if err := db.DeleteTask(task.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Verify task is gone
	_, err = db.GetTask(task.ID)
	if err == nil {
		t.Fatal("expected task to be deleted")
	}

	// Restore task
	if err := db.RestoreTask(task); err != nil {
		t.Fatalf("restore task failed: %v", err)
	}

	// Verify restored task
	restored, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get restored task failed: %v", err)
	}
	if restored.ID != task.ID || restored.Title != task.Title || restored.Status != task.Status {
		t.Fatalf("restored task mismatch: got %+v, want %+v", restored, task)
	}
	if len(restored.Notes) != 1 || restored.Notes[0].Content != "Working on fix" {
		t.Fatalf("restored notes mismatch: got %+v", restored.Notes)
	}
}




