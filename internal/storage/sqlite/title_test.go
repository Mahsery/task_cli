package sqlite

import (
	"strings"
	"testing"

	"task_cli/internal/domain"
)

func TestCreateTaskRejectsOverlongTitle(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	long := strings.Repeat("x", domain.MaxTitleLength+1)
	if _, err := db.CreateTask("local", "/repo", long, "", domain.StatusOpen, domain.PriorityNormal, nil, "a", ""); err == nil {
		t.Fatal("expected error for over-length title")
	}
	if _, err := db.CreateTask("local", "/repo", "multi\nline", "", domain.StatusOpen, domain.PriorityNormal, nil, "a", ""); err == nil {
		t.Fatal("expected error for multi-line title")
	}
	// A rejected create must not burn a sequence number.
	task, err := db.CreateTask("local", "/repo", strings.Repeat("y", domain.MaxTitleLength), "", domain.StatusOpen, domain.PriorityNormal, nil, "a", "")
	if err != nil {
		t.Fatalf("title at limit should be accepted: %v", err)
	}
	if task.ID != "T-1" {
		t.Errorf("expected T-1 after rejected creates, got %s", task.ID)
	}
}

func TestUpdateTaskTitleLengthAndGrandfathering(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task, err := db.CreateTask("local", "/repo", "Short", "", domain.StatusOpen, domain.PriorityNormal, nil, "a", "")
	if err != nil {
		t.Fatal(err)
	}

	long := strings.Repeat("z", domain.MaxTitleLength+10)
	if _, err := db.UpdateTask(task.ID, &long, nil, nil, nil, nil, nil, nil, nil, nil, "a"); err == nil {
		t.Fatal("expected error when updating to over-length title")
	}

	// Simulate a legacy title that predates the limit.
	if _, err := db.conn.Exec("UPDATE tasks SET title = ? WHERE id = ?", long, task.ID); err != nil {
		t.Fatal(err)
	}
	// Editors re-submit the unchanged title alongside other edits; that must still work.
	desc := "new description"
	updated, err := db.UpdateTask(task.ID, &long, &desc, nil, nil, nil, nil, nil, nil, nil, "a")
	if err != nil {
		t.Fatalf("unchanged legacy title should not block unrelated edits: %v", err)
	}
	if updated.Description != desc || updated.Title != long {
		t.Errorf("unexpected result: title=%q desc=%q", updated.Title, updated.Description)
	}

	// Changing the legacy title to another over-length value is rejected.
	longer := long + "!"
	if _, err := db.UpdateTask(task.ID, &longer, nil, nil, nil, nil, nil, nil, nil, nil, "a"); err == nil {
		t.Fatal("expected error when changing to a different over-length title")
	}
}
