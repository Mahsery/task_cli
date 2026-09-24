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
	default:
		return StatusOpen, fmt.Errorf("invalid status %q (allowed: open, in_progress, done, closed)", s)
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

	// Git integration (Linux kernel style)
	CommitSHA   string     `json:"commit_sha,omitempty"`
	CommitMsg   string     `json:"commit_msg,omitempty"`
	DiffStat    string     `json:"diff_stat,omitempty"`
	DiffContent string     `json:"diff_content,omitempty"`

	Notes []TaskNote `json:"notes,omitempty"`
}

type TaskFilter struct {
	Scope        string
	RepoPath     string
	Status       *Status
	Priority     *Priority
	Tag          string
	Assignee     string
	Search       string
	Sort         string
	Limit        int
	OmitNotes    bool
	IncludeNotes bool
}
