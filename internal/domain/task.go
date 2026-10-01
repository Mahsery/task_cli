package domain

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	ScopeLocal  = "local"
	ScopeGlobal = "global"
)

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusClosed     Status = "closed"
	StatusArchived   Status = "archived"
	StatusOrphan     Status = "orphan"
)

func ParseStatus(s string) (Status, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "open", "o":
		return StatusOpen, nil
	case "in_progress", "in-progress", "prog", "p", "active":
		return StatusInProgress, nil
	case "done", "d", "completed", "resolved":
		return StatusDone, nil
	case "closed", "c":
		return StatusClosed, nil
	case "archived", "archive", "arc":
		return StatusArchived, nil
	case "orphan", "orphaned":
		return StatusOrphan, nil
	default:
		return StatusOpen, fmt.Errorf("invalid status %q (allowed: open, in_progress, done, closed, archived, orphan)", s)
	}
}

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func ParsePriority(s string) (Priority, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low", "l":
		return PriorityLow, nil
	case "normal", "norm", "n", "":
		return PriorityNormal, nil
	case "high", "h":
		return PriorityHigh, nil
	case "urgent", "u", "critical":
		return PriorityUrgent, nil
	default:
		return PriorityNormal, fmt.Errorf("invalid priority %q (allowed: low, normal, high, urgent)", s)
	}
}

type TaskNote struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	ID          string     `json:"id"`          // e.g. "T-1" (local) or "G-1" (global)
	Scope       string     `json:"scope"`       // "local" or "global"
	RepoPath    string     `json:"repo_path"`   // git root path if local
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	Priority    Priority   `json:"priority"`
	Tags        []string   `json:"tags"`
	Author      string     `json:"author"`
	Assignee    string     `json:"assignee,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`

	// Git integration (Linux kernel style)
	CommitSHA   string     `json:"commit_sha,omitempty"`
	CommitMsg   string     `json:"commit_msg,omitempty"`
	DiffStat    string     `json:"diff_stat,omitempty"`
	DiffContent string     `json:"diff_content,omitempty"`

	// Dependencies & DAG
	DependsOn []string `json:"depends_on,omitempty"` // Prerequisite task IDs that must be completed before this task
	BlockedBy []string `json:"blocked_by,omitempty"` // Unfinished prerequisite task IDs that currently block this task
	Blocks    []string `json:"blocks,omitempty"`     // Downstream task IDs that depend on this task
	IsBlocked bool     `json:"is_blocked,omitempty"` // True if any prerequisite task is not done/closed

	Notes []TaskNote `json:"notes,omitempty"`
}

func (t *Task) IsArchived() bool {
	return t != nil && (t.ArchivedAt != nil || t.Status == StatusArchived)
}

func (t *Task) LastActivityTime() time.Time {
	if t == nil {
		return time.Time{}
	}
	latest := t.UpdatedAt
	if latest.IsZero() {
		latest = t.CreatedAt
	}
	for _, n := range t.Notes {
		if n.CreatedAt.After(latest) {
			latest = n.CreatedAt
		}
	}
	if t.ClosedAt != nil && t.ClosedAt.After(latest) {
		latest = *t.ClosedAt
	}
	return latest
}

func (t *Task) InactiveDuration() time.Duration {
	act := t.LastActivityTime()
	if act.IsZero() {
		return 0
	}
	return time.Since(act)
}

func (t *Task) IsOrphaned() bool {
	if t == nil || t.IsArchived() {
		return false
	}
	if t.Status == StatusDone || t.Status == StatusClosed {
		return false
	}
	if strings.EqualFold(string(t.Status), "orphan") || strings.EqualFold(string(t.Status), "orphaned") {
		return true
	}
	for _, tag := range t.Tags {
		if strings.EqualFold(tag, "orphaned") || strings.EqualFold(tag, "orphan") {
			return true
		}
	}
	// Open tasks without an assignee are normal backlog items and can NEVER be orphaned.
	if t.Status == StatusOpen && strings.TrimSpace(t.Assignee) == "" {
		return false
	}
	// Assigned or in_progress tasks whose activity has timed out (> 1 hour) are orphaned.
	if t.Status == StatusInProgress || strings.TrimSpace(t.Assignee) != "" {
		act := t.LastActivityTime()
		if !act.IsZero() && time.Since(act) > time.Hour {
			return true
		}
	}
	return false
}

type TaskDependency struct {
	TaskID      string    `json:"task_id"`
	DependsOnID string    `json:"depends_on_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type TaskFilter struct {
	Scope           string
	RepoPath        string
	Status          *Status
	Priority        *Priority
	Tag             string
	Assignee        string
	Search          string
	Sort            string
	Limit           int
	OmitNotes       bool
	IncludeNotes    bool
	OnlyReady       bool   // Return tasks with status open and 0 unfinished blockers
	OnlyBlocked     bool   // Return tasks that have unfinished blockers
	OnlyOrphaned    bool   // Return tasks that are orphaned (tagged, status, or inactive)
	IncludeArchived bool   // Return both active and archived tasks
	OnlyArchived    bool   // Return only archived tasks
	DependsOn       string // Filter by prerequisite task ID
	Blocks          string // Filter by downstream task ID
}

type StaleTaskCandidate struct {
	TaskID           string        `json:"task_id"`
	Title            string        `json:"title"`
	Assignee         string        `json:"assignee"`
	Status           Status        `json:"status"`
	Priority         Priority      `json:"priority"`
	InactiveDuration time.Duration `json:"inactive_duration"`
	LastActivity     time.Time     `json:"last_activity"`
}
