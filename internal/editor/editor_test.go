package editor

import (
	"testing"

	"task_cli/internal/domain"
)

func TestParseNewTaskContent(t *testing.T) {
	input := `# Title: Fix race condition in connection handler
# Priority: high (low | normal | high | urgent)
# Tags: concurrency, bug, network
# Assignee: mahsery

Detailed explanation:
The socket connection reader must be protected under concurrent writes.
`

	parsed, err := ParseNewTaskContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Title != "Fix race condition in connection handler" {
		t.Errorf("unexpected title: %q", parsed.Title)
	}
	if parsed.Priority != domain.PriorityHigh {
		t.Errorf("expected high priority, got %q", parsed.Priority)
	}
	if len(parsed.Tags) != 3 || parsed.Tags[0] != "concurrency" || parsed.Tags[1] != "bug" || parsed.Tags[2] != "network" {
		t.Errorf("unexpected tags: %v", parsed.Tags)
	}
	if parsed.Assignee != "mahsery" {
		t.Errorf("unexpected assignee: %q", parsed.Assignee)
	}
	if parsed.Description != "Detailed explanation:\nThe socket connection reader must be protected under concurrent writes." {
		t.Errorf("unexpected description: %q", parsed.Description)
	}
}

func TestParseNewTaskEmptyTitle(t *testing.T) {
	input := `# Title: 
# Priority: normal

Just description without title.
`
	_, err := ParseNewTaskContent(input)
	if err == nil {
		t.Fatal("expected error on empty title, got nil")
	}
}

func TestParseEditTaskContent(t *testing.T) {
	orig := &domain.Task{
		ID:          "T-10",
		Title:       "Original Title",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityNormal,
		Tags:        []string{"core"},
		Assignee:    "dev",
		Description: "Old description",
	}

	input := `# Task: T-10
# Title: Modified Title
# Status: done (open | in_progress | done | closed)
# Priority: urgent
# Tags: core, released
# Assignee: mahsery

Updated description with more details.

# --- Add new notes/comments below (optional) ---
Finished the deployment and verified on prod.
`

	parsed, err := ParseEditTaskContent(input, orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Title != "Modified Title" {
		t.Errorf("unexpected title: %q", parsed.Title)
	}
	if parsed.Status != domain.StatusDone {
		t.Errorf("expected status done, got %q", parsed.Status)
	}
	if parsed.Priority != domain.PriorityUrgent {
		t.Errorf("expected urgent priority, got %q", parsed.Priority)
	}
	if len(parsed.Tags) != 2 || parsed.Tags[1] != "released" {
		t.Errorf("unexpected tags: %v", parsed.Tags)
	}
	if parsed.Description != "Updated description with more details." {
		t.Errorf("unexpected description: %q", parsed.Description)
	}
	if parsed.NewNote != "Finished the deployment and verified on prod." {
		t.Errorf("unexpected note: %q", parsed.NewNote)
	}
}
