package domain

import (
	"testing"
	"time"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected Status
		wantErr  bool
	}{
		{"open", StatusOpen, false},
		{"o", StatusOpen, false},
		{"OPEN", StatusOpen, false},
		{"in_progress", StatusInProgress, false},
		{"in-progress", StatusInProgress, false},
		{"prog", StatusInProgress, false},
		{"active", StatusInProgress, false},
		{"done", StatusDone, false},
		{"d", StatusDone, false},
		{"completed", StatusDone, false},
		{"closed", StatusClosed, false},
		{"c", StatusClosed, false},
		{"archived", StatusArchived, false},
		{"archive", StatusArchived, false},
		{"arc", StatusArchived, false},
		{"ARCHIVE", StatusArchived, false},
		{"orphan", StatusOrphan, false},
		{"orphaned", StatusOrphan, false},
		{"ORPHAN", StatusOrphan, false},
		{"invalid_status", StatusOpen, true},
		{"", StatusOpen, true},
	}

	for _, tt := range tests {
		got, err := ParseStatus(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseStatus(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.expected {
			t.Errorf("ParseStatus(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		input    string
		expected Priority
		wantErr  bool
	}{
		{"low", PriorityLow, false},
		{"l", PriorityLow, false},
		{"normal", PriorityNormal, false},
		{"norm", PriorityNormal, false},
		{"", PriorityNormal, false},
		{"high", PriorityHigh, false},
		{"h", PriorityHigh, false},
		{"urgent", PriorityUrgent, false},
		{"u", PriorityUrgent, false},
		{"critical", PriorityUrgent, false},
		{"extreme", PriorityNormal, true},
	}

	for _, tt := range tests {
		got, err := ParsePriority(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParsePriority(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.expected {
			t.Errorf("ParsePriority(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestIsArchived(t *testing.T) {
	var nilTask *Task
	if nilTask.IsArchived() {
		t.Error("expected nil task to return false for IsArchived")
	}

	task := &Task{Status: StatusOpen}
	if task.IsArchived() {
		t.Error("expected open task without archived_at to not be archived")
	}

	task.Status = StatusArchived
	if !task.IsArchived() {
		t.Error("expected task with StatusArchived to be archived")
	}

	now := time.Now()
	task.Status = StatusDone
	task.ArchivedAt = &now
	if !task.IsArchived() {
		t.Error("expected task with ArchivedAt set to be archived")
	}
}

func TestIsOrphaned(t *testing.T) {
	var nilTask *Task
	if nilTask.IsOrphaned() {
		t.Error("expected nil task to return false for IsOrphaned")
	}

	// Tagged 'orphaned'
	tagged := &Task{Tags: []string{"orphaned"}}
	if !tagged.IsOrphaned() {
		t.Error("expected task tagged orphaned to return true")
	}

	// Tagged 'orphan'
	taggedOrphan := &Task{Tags: []string{"orphan"}}
	if !taggedOrphan.IsOrphaned() {
		t.Error("expected task tagged orphan to return true")
	}

	// Status 'orphan'
	statusOrphan := &Task{Status: StatusOrphan}
	if !statusOrphan.IsOrphaned() {
		t.Error("expected task with status orphan to return true")
	}

	// Open, unassigned, older than 1h -> CANNOT be orphaned
	oldOpen := &Task{
		Status:    StatusOpen,
		Assignee:  "",
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	if oldOpen.IsOrphaned() {
		t.Error("expected open unassigned task to NEVER be orphaned")
	}

	// In progress, assigned, inactive for 2h -> IS orphaned
	inactiveInProg := &Task{
		Status:    StatusInProgress,
		Assignee:  "alice",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}
	if !inactiveInProg.IsOrphaned() {
		t.Error("expected assigned in_progress task inactive for 2h to be orphaned")
	}

	// In progress, assigned, recent activity (10m ago) -> ACTIVE (NOT orphaned)
	activeInProg := &Task{
		Status:    StatusInProgress,
		Assignee:  "alice",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	if activeInProg.IsOrphaned() {
		t.Error("expected active in_progress task with recent updates to NOT be orphaned")
	}

	// In progress, inactive for 2h BUT archived -> NOT orphaned
	archivedInProg := &Task{
		Status:     StatusArchived,
		Assignee:   "alice",
		CreatedAt:  time.Now().Add(-2 * time.Hour),
		ArchivedAt: func() *time.Time { t := time.Now(); return &t }(),
	}
	if archivedInProg.IsOrphaned() {
		t.Error("expected archived task to never be considered orphaned")
	}
}

