package cli

import (
	"strings"
	"testing"
	"time"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"

	"github.com/mattn/go-runewidth"
)

func TestWatchDashboard_InitialStateAndLatest(t *testing.T) {
	now := time.Now()
	t1 := &domain.Task{
		ID:        "T-1",
		Title:     "First task",
		Status:    domain.StatusOpen,
		Priority:  domain.PriorityNormal,
		CreatedAt: now.Add(-10 * time.Minute),
		UpdatedAt: now.Add(-10 * time.Minute),
	}
	t2 := &domain.Task{
		ID:        "T-2",
		Title:     "Second task",
		Status:    domain.StatusInProgress,
		Priority:  domain.PriorityUrgent,
		Assignee:  "alice",
		CreatedAt: now.Add(-5 * time.Minute),
		UpdatedAt: now.Add(-2 * time.Minute),
		Notes: []domain.TaskNote{
			{
				ID:        "N-1",
				Author:    "alice",
				Content:   "Working on urgent fix",
				CreatedAt: now.Add(-2 * time.Minute),
			},
		},
	}

	dashboard := NewWatchDashboard("local", "/repo", []*domain.Task{t1, t2})
	snap := dashboard.Snapshot()

	if len(snap.ActiveTasks) != 1 || snap.ActiveTasks[0].ID != "T-2" {
		t.Fatalf("expected 1 active task T-2, got %v", snap.ActiveTasks)
	}
	if len(snap.BacklogTasks) != 1 || snap.BacklogTasks[0].ID != "T-1" {
		t.Fatalf("expected 1 backlog task T-1, got %v", snap.BacklogTasks)
	}
	if snap.LatestTask == nil || snap.LatestTask.ID != "T-2" {
		t.Fatalf("expected latest task T-2, got %v", snap.LatestTask)
	}
}

func TestWatchDashboard_RecordEvent_UpdatesLatest(t *testing.T) {
	now := time.Now()
	t1 := &domain.Task{
		ID:        "T-1",
		Title:     "First task",
		Status:    domain.StatusOpen,
		Priority:  domain.PriorityNormal,
		CreatedAt: now.Add(-10 * time.Minute),
	}
	dashboard := NewWatchDashboard("local", "/repo", []*domain.Task{t1})

	// Create an updated event for T-1
	updatedTime := now.Add(-10 * time.Second)
	updatedT1 := &domain.Task{
		ID:        "T-1",
		Title:     "First task (in progress)",
		Status:    domain.StatusInProgress,
		Priority:  domain.PriorityHigh,
		Assignee:  "bob",
		CreatedAt: t1.CreatedAt,
		UpdatedAt: updatedTime,
		Notes: []domain.TaskNote{
			{
				ID:        "N-2",
				Author:    "bob",
				Content:   "Started working on this task now",
				CreatedAt: updatedTime,
			},
		},
	}

	dashboard.RecordEvent(protocol.Event{
		Type:      protocol.EventTaskUpdated,
		Timestamp: updatedTime,
		Task:      updatedT1,
		Message:   "~ T-1 updated (status: in_progress)",
	})

	snap := dashboard.Snapshot()
	if len(snap.ActiveTasks) != 1 || snap.ActiveTasks[0].ID != "T-1" {
		t.Fatalf("expected T-1 in active tasks, got %v", snap.ActiveTasks)
	}
	if len(snap.BacklogTasks) != 0 {
		t.Fatalf("expected 0 backlog tasks, got %d", len(snap.BacklogTasks))
	}
	if snap.LatestTask == nil || snap.LatestTask.ID != "T-1" {
		t.Fatalf("expected latest task to be T-1, got %v", snap.LatestTask)
	}
	if len(snap.RecentEvents) != 1 || snap.RecentEvents[0].Message != "~ T-1 updated (status: in_progress)" {
		t.Fatalf("expected event recorded in history, got %v", snap.RecentEvents)
	}
}

func TestWatchDashboard_RecordEvent_DeleteTask(t *testing.T) {
	now := time.Now()
	t1 := &domain.Task{
		ID:        "T-1",
		Title:     "First task",
		Status:    domain.StatusOpen,
		CreatedAt: now.Add(-10 * time.Minute),
	}
	t2 := &domain.Task{
		ID:        "T-2",
		Title:     "Second task",
		Status:    domain.StatusInProgress,
		CreatedAt: now.Add(-5 * time.Minute),
	}
	dashboard := NewWatchDashboard("local", "/repo", []*domain.Task{t1, t2})

	// Delete T-2 which was the latest task
	dashboard.RecordEvent(protocol.Event{
		Type:      protocol.EventTaskDeleted,
		Timestamp: now,
		Task:      &domain.Task{ID: "T-2"},
		Message:   "- T-2 deleted",
	})

	snap := dashboard.Snapshot()
	if len(snap.ActiveTasks) != 0 {
		t.Fatalf("expected 0 active tasks after delete, got %d", len(snap.ActiveTasks))
	}
	if snap.LatestTask == nil || snap.LatestTask.ID != "T-1" {
		t.Fatalf("expected latest task to fallback to T-1, got %v", snap.LatestTask)
	}
}

func TestRenderLatestCard_AlignmentAndBorders(t *testing.T) {
	now := time.Now()
	task := &domain.Task{
		ID:       "T-39",
		Title:    "UI rendering: draw ordered shapes and images in the Vulkan UI pass",
		Status:   domain.StatusInProgress,
		Priority: domain.PriorityUrgent,
		Assignee: "sol-doc",
		Notes: []domain.TaskNote{
			{
				Author:  "sol-document-20260924-b",
				Content: "Follow-up 9de5ff7 adds no-recorder finalizer branch model with an active priority slot/ticket and real two-packet UI Packet_Flights native fixture. Coordinated D16 mass test passed 8/8 GPU tests and 2/2 native UI tests.",
			},
		},
	}

	widths := []int{60, 80, 100}
	for _, w := range widths {
		expectedW := w
		if expectedW > 92 {
			expectedW = 92
		}

		// Unicode Card
		uCard := renderLatestCard(task, now.Add(-4*time.Minute), w, false, now)
		lines := strings.Split(strings.TrimRight(uCard, "\n"), "\n")
		if len(lines) < 5 {
			t.Fatalf("expected at least 5 lines in card, got %d", len(lines))
		}
		for i, line := range lines {
			lineWidth := runewidth.StringWidth(line)
			if lineWidth != expectedW {
				t.Errorf("width %d, line %d: expected visual width %d, got %d for line: %q",
					w, i, expectedW, lineWidth, line)
			}
		}

		// ASCII Card
		aCard := renderLatestCard(task, now.Add(-4*time.Minute), w, true, now)
		aLines := strings.Split(strings.TrimRight(aCard, "\n"), "\n")
		for i, line := range aLines {
			lineWidth := runewidth.StringWidth(line)
			if lineWidth != expectedW {
				t.Errorf("ascii width %d, line %d: expected visual width %d, got %d for line: %q",
					w, i, expectedW, lineWidth, line)
			}
		}
	}
}

func TestWrapCardText(t *testing.T) {
	text := "Follow-up 9de5ff7 adds no-recorder finalizer branch model with an active priority slot/ticket and real two-packet UI Packet_Flights native fixture."
	lines := wrapCardText(text, 40, 4)

	if len(lines) == 0 {
		t.Fatal("expected wrapped lines, got none")
	}
	if len(lines) > 4 {
		t.Fatalf("expected <= 4 lines, got %d", len(lines))
	}
	for i, l := range lines {
		w := runewidth.StringWidth(l)
		if w > 40 {
			t.Errorf("line %d visual width %d exceeds max 40: %q", i, w, l)
		}
	}

	// Long unbroken word
	unbroken := "SuperLongWordThatCannotFitIntoTenCharsAtAllSoItMustBeBroken"
	unbrokenLines := wrapCardText(unbroken, 15, 5)
	if len(unbrokenLines) < 2 {
		t.Fatalf("expected unbroken word to be split into multiple lines, got %d", len(unbrokenLines))
	}
	for i, l := range unbrokenLines {
		if runewidth.StringWidth(l) > 15 {
			t.Errorf("line %d width %d exceeds 15: %q", i, runewidth.StringWidth(l), l)
		}
	}
}

func TestTruncateWithEllipsis(t *testing.T) {
	if res := truncateWithEllipsis("Short", 10); res != "Short" {
		t.Fatalf("expected 'Short', got %q", res)
	}
	if res := truncateWithEllipsis("Very long string that needs truncation", 12); res != "Very long .." {
		t.Fatalf("expected 'Very long ..', got %q", res)
	}
	if res := truncateWithEllipsis("Multi\nLine\rString", 20); res != "Multi Line String" {
		t.Fatalf("expected whitespace cleaned, got %q", res)
	}
}

func TestRenderDashboardView_Clamping(t *testing.T) {
	now := time.Now()
	t1 := &domain.Task{
		ID:        "T-10",
		Title:     "Very long task title that would definitely wrap onto a second line if not clamped properly",
		Status:    domain.StatusInProgress,
		Priority:  domain.PriorityUrgent,
		Assignee:  "very-long-assignee-name-exceeding-normal-bounds",
		CreatedAt: now.Add(-30 * time.Minute),
	}
	t2 := &domain.Task{
		ID:        "T-11",
		Title:     "Another very long backlog task with multiple tags",
		Status:    domain.StatusOpen,
		Priority:  domain.PriorityLow,
		Tags:      []string{"tag-one", "tag-two", "tag-three", "tag-four", "tag-five"},
		CreatedAt: now.Add(-2 * time.Hour),
	}

	state := &WatchState{
		Scope:        "local",
		RepoPath:     "/home/user/project",
		ActiveTasks:  []*domain.Task{t1},
		BacklogTasks: []*domain.Task{t2},
		TotalCount:   2,
		LatestTask:   t1,
		LatestTime:   now.Add(-5 * time.Minute),
		RecentEvents: []WatchEventItem{
			{
				Timestamp: now.Add(-1 * time.Minute),
				Message:   "~ T-10 updated (status: in_progress)",
			},
		},
	}

	widths := []int{60, 80, 110}
	for _, w := range widths {
		view := renderDashboardView(state, w, 24, false, now)
		lines := strings.Split(view, "\n")
		for i, line := range lines {
			lw := runewidth.StringWidth(line)
			if lw > w {
				t.Errorf("width %d, line %d exceeds terminal width: len=%d vs max=%d, line=%q",
					w, i, lw, w, line)
			}
		}
	}
}
