package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"task_cli/internal/domain"
)

func TestRepositoryMovesPreserveTasksAndSequence(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "original")
	db, err := OpenRepository(filepath.Join(repoPath, ".tasks", "tasks.db"), repoPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	create := func(title string, deps ...string) *domain.Task {
		t.Helper()
		task, createErr := db.CreateTask("local", repoPath, title, "preserved description", domain.StatusOpen, domain.PriorityHigh, []string{"bug"}, "tester", "agent", deps...)
		if createErr != nil {
			t.Fatal(createErr)
		}
		return task
	}
	first := create("First task")
	second := create("Dependent task", first.ID)
	deleted := create("Deleted task")
	if err := db.DeleteTask(deleted.ID); err != nil {
		t.Fatal(err)
	}
	// The sequence must survive even if the highest ID has no remaining record.
	if _, err := db.conn.Exec(`UPDATE task_sequences SET last_seq = 10 WHERE scope = 'local'`); err != nil {
		t.Fatal(err)
	}
	global, err := db.CreateTask("global", "", "Global task", "", domain.StatusOpen, domain.PriorityNormal, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"moved", "moved-again"} {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		nextPath := filepath.Join(root, name)
		if err := os.Rename(repoPath, nextPath); err != nil {
			t.Fatal(err)
		}
		repoPath = nextPath
		db, err = OpenRepository(filepath.Join(repoPath, ".tasks", "tasks.db"), repoPath)
		if err != nil {
			t.Fatal(err)
		}
		tasks, listErr := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: repoPath})
		if listErr != nil || len(tasks) != 2 {
			t.Fatalf("list after move: tasks=%v err=%v", tasks, listErr)
		}
		got, getErr := db.GetTask(second.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if got.RepoPath != repoPath || got.Title != second.Title || got.Description != second.Description || got.Assignee != second.Assignee || !got.CreatedAt.Equal(second.CreatedAt) || !got.UpdatedAt.Equal(second.UpdatedAt) || len(got.DependsOn) != 1 || got.DependsOn[0] != first.ID {
			t.Fatalf("task changed during move: %+v", got)
		}
		tombstones, listErr := db.ListTombstones("local", repoPath)
		if listErr != nil || len(tombstones) != 1 || tombstones[0].RepoPath != repoPath {
			t.Fatalf("deleted tasks after move: tasks=%v err=%v", tombstones, listErr)
		}
	}
	restored, err := db.RestoreTaskByID(deleted.ID)
	if err != nil || restored.RepoPath != repoPath {
		t.Fatalf("restore after move: task=%v err=%v", restored, err)
	}
	if next := create("After moves"); next.ID != "T-11" {
		t.Fatalf("expected T-11, got %s", next.ID)
	}
	gotGlobal, err := db.GetTask(global.ID)
	if err != nil || gotGlobal.RepoPath != "" {
		t.Fatalf("global task changed: task=%v err=%v", gotGlobal, err)
	}
}
