package editor

import (
	"os"
	"strings"
	"testing"
	"time"

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

// An orphaned task's edit template starts with a keyless warning banner. Those
// comment lines must not end the header: otherwise every header line below them
// is saved into the description (seen in the wild on T-209/T-239/T-241).
func TestEditOrphanBannerRoundTripKeepsDescriptionClean(t *testing.T) {
	stale := time.Now().Add(-30 * 24 * time.Hour)
	orig := &domain.Task{
		ID:          "T-209",
		Title:       "Navigation",
		Status:      domain.StatusInProgress,
		Priority:    domain.PriorityHigh,
		Assignee:    "Argon",
		Description: "Real body.",
		OrphanedAt:  &stale,
	}
	if !orig.IsOrphaned() {
		t.Fatal("test setup: expected task to be orphaned")
	}
	path, err := CreateEditTaskFile(orig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path) }()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ORPHANED") {
		t.Fatal("test setup: template should contain the orphan banner")
	}

	parsed, err := ParseEditTaskContent(string(data), orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Description != "Real body." {
		t.Errorf("description polluted by header template: %q", parsed.Description)
	}
	if parsed.Status != domain.StatusInProgress || parsed.Priority != domain.PriorityHigh || parsed.Assignee != "Argon" {
		t.Errorf("header fields lost: %+v", parsed)
	}
	if parsed.NewNote != "" {
		t.Errorf("unexpected note: %q", parsed.NewNote)
	}
}

func TestParseNewTaskSkipsKeylessHeaderComments(t *testing.T) {
	input := "# Title: X\n#\n# just a comment\n# Priority: low\n\nBody\n"
	parsed, err := ParseNewTaskContent(input)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Description != "Body" || parsed.Priority != domain.PriorityLow {
		t.Errorf("got desc=%q prio=%q", parsed.Description, parsed.Priority)
	}
}

func editRoundTrip(t *testing.T, orig *domain.Task, mutate func(string) string) *ParsedEditTask {
	t.Helper()
	path, err := CreateEditTaskFile(orig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path) }()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseEditTaskContent(mutate(string(data)), orig)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// Opening and closing the editor without touching anything must produce no
// update at all (no write, no event, nothing to clobber).
func TestEditChangesEmptyWhenUntouched(t *testing.T) {
	for _, orig := range []*domain.Task{
		{ID: "T-1", Title: "A", Status: domain.StatusOpen, Priority: domain.PriorityNormal, Description: "body\n\n"},
		{ID: "T-2", Title: "B", Status: domain.StatusInProgress, Priority: domain.PriorityHigh, Assignee: "x", TopicPath: "game/nav", Tags: []string{"a"}},
	} {
		ch := editRoundTrip(t, orig, func(s string) string { return s }).Changes(orig)
		if !ch.Empty() {
			t.Errorf("%s: expected no changes, got %+v", orig.ID, ch)
		}
	}
}

// Changing one field must send only that field, so a concurrent update to any
// other field (e.g. an agent claiming the task while $EDITOR is open) survives.
func TestEditChangesOnlyTouchedFields(t *testing.T) {
	orig := &domain.Task{ID: "T-3", Title: "C", Status: domain.StatusOpen, Priority: domain.PriorityNormal, Description: "body"}
	ch := editRoundTrip(t, orig, func(s string) string {
		return strings.Replace(s, "# Priority: normal", "# Priority: urgent", 1)
	}).Changes(orig)
	if ch.Priority == nil || *ch.Priority != domain.PriorityUrgent {
		t.Fatalf("expected priority change, got %+v", ch)
	}
	if ch.Title != nil || ch.Description != nil || ch.Status != nil || ch.Assignee != nil || ch.Tags != nil || ch.AddNote != nil || ch.Topic != "" {
		t.Errorf("untouched fields leaked into update: %+v", ch)
	}
}

func TestEditChangesTopicMove(t *testing.T) {
	orig := &domain.Task{ID: "T-4", Title: "D", Status: domain.StatusOpen, Priority: domain.PriorityNormal}
	ch := editRoundTrip(t, orig, func(s string) string {
		return strings.Replace(s, "# Topic: unfiled", "# Topic: game/nav", 1)
	}).Changes(orig)
	if ch.Topic != "game/nav" {
		t.Errorf("expected topic move to game/nav, got %q", ch.Topic)
	}
}

func TestEditReviewerAndReviewNoteRoundTrip(t *testing.T) {
	orig := &domain.Task{
		ID:       "T-5",
		Title:    "Verify Reviewer Template",
		Status:   domain.StatusInProgress,
		Priority: domain.PriorityHigh,
		Assignee: "bob",
	}
	ch := editRoundTrip(t, orig, func(s string) string {
		s = strings.Replace(s, "# Reviewer: ", "# Reviewer: alice", 1)
		s += "\nReviewed architecture and error handling.\n"
		return s
	}).Changes(orig)
	if ch.Reviewer == nil || *ch.Reviewer != "alice" {
		t.Fatalf("expected Reviewer change to 'alice', got %+v", ch.Reviewer)
	}
	if ch.AddReviewNote == nil || *ch.AddReviewNote != "Reviewed architecture and error handling." {
		t.Fatalf("expected AddReviewNote to be populated, got %+v", ch.AddReviewNote)
	}
	if ch.AddNote != nil {
		t.Fatalf("expected regular AddNote to be nil when only reviewer note was added, got %q", *ch.AddNote)
	}
}
