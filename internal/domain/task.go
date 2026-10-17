package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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

// MaxTitleLength is the maximum number of characters (runes) allowed in a task title.
// Longer context belongs in the description.
const MaxTitleLength = 120

// NormalizeTitle trims surrounding whitespace and validates a task title.
// It rejects empty titles, titles containing line breaks or control characters,
// and titles longer than MaxTitleLength characters.
func NormalizeTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("task title cannot be empty")
	}
	for _, r := range title {
		if r == '\n' || r == '\r' {
			return "", fmt.Errorf("task title must be a single line (put details in the description)")
		}
		if unicode.IsControl(r) && r != '\t' {
			return "", fmt.Errorf("task title contains invalid control character %U", r)
		}
	}
	title = strings.ReplaceAll(title, "\t", " ")
	if n := utf8.RuneCountInString(title); n > MaxTitleLength {
		return "", fmt.Errorf("task title is too long: %d characters (max %d); move details into the description", n, MaxTitleLength)
	}
	return title, nil
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
	ID        string    `json:"id" db:"id"`
	Author    string    `json:"author" db:"author"`
	Content   string    `json:"content" db:"content"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type TaskCommit struct {
	ID          int64     `json:"id,omitempty" db:"id"`
	TaskID      string    `json:"task_id,omitempty" db:"task_id"`
	SHA         string    `json:"sha" db:"sha"`
	Subject     string    `json:"subject" db:"subject"`
	Author      string    `json:"author,omitempty" db:"author"`
	CommittedAt string    `json:"committed_at,omitempty" db:"committed_at"`
	DiffStat    string    `json:"diff_stat,omitempty" db:"diff_stat"`
	DiffContent string    `json:"diff_content,omitempty" db:"diff_content"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type Task struct {
	ID          string     `json:"id"`        // e.g. "T-1" (local) or "G-1" (global)
	Scope       string     `json:"scope"`     // "local" or "global"
	RepoPath    string     `json:"repo_path"` // git root path if local
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	Priority    Priority   `json:"priority"`
	Tags        []string   `json:"tags"`
	Author      string     `json:"author"`
	Assignee    string     `json:"assignee,omitempty"`
	Reviewer    string     `json:"reviewer,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
	OrphanedAt  *time.Time `json:"orphaned_at,omitempty"` // set when the task was explicitly orphaned/reaped; cleared on claim

	// Git & GitHub integration
	Branch      string       `json:"branch,omitempty"`
	ExternalRef string       `json:"external_ref,omitempty"`
	PRURL       string       `json:"pr_url,omitempty"`
	CommitSHA   string       `json:"commit_sha,omitempty"`
	CommitMsg   string       `json:"commit_msg,omitempty"`
	DiffStat    string       `json:"diff_stat,omitempty"`
	DiffContent string       `json:"diff_content,omitempty"`
	Commits     []TaskCommit `json:"commits,omitempty"`

	// Dependencies & DAG
	DependsOn []string `json:"depends_on,omitempty"` // Prerequisite task IDs that must be completed before this task
	BlockedBy []string `json:"blocked_by,omitempty"` // Unfinished prerequisite task IDs that currently block this task
	Blocks    []string `json:"blocks,omitempty"`     // Downstream task IDs that depend on this task
	IsBlocked bool     `json:"is_blocked,omitempty"` // True if any prerequisite task is not done/closed

	Notes         []TaskNote `json:"notes,omitempty"`
	ReviewerNotes []TaskNote `json:"reviewer_notes,omitempty"`

	// Organisation: item kind and topic membership (TopicID 0 = unfiled)
	Kind      ItemKind `json:"kind,omitempty"`
	TopicID   int64    `json:"topic_id,omitempty"`
	TopicPath string   `json:"topic,omitempty"` // computed slash path of the topic, e.g. "game/hero-system"

	// Wiki links: outgoing [[...]] targets parsed from description and notes, and incoming backlinks
	Links     []string `json:"links,omitempty"`
	Backlinks []string `json:"backlinks,omitempty"`
}

// IsPost reports whether the item is a board post rather than a task.
func (t *Task) IsPost() bool {
	return t != nil && t.Kind == KindPost
}

// ReviewedByPrefix is the tag prefix added to Task.Tags when a task has been reviewed.
const ReviewedByPrefix = "reviewed-by:"

// ReviewedBy returns the reviewer identifier from a "reviewed-by:<tag>" tag on the task, or "".
func (t *Task) ReviewedBy() string {
	if t == nil {
		return ""
	}
	for _, tag := range t.Tags {
		trimmed := strings.TrimSpace(tag)
		if strings.HasPrefix(strings.ToLower(trimmed), ReviewedByPrefix) {
			val := strings.TrimSpace(trimmed[len(ReviewedByPrefix):])
			if val != "" {
				return val
			}
		}
	}
	return ""
}

// WithReviewedByTag returns tags with any previous "reviewed-by:*" tag replaced by "reviewed-by:<reviewer>".
// If reviewer is empty, any "reviewed-by:*" tag is removed.
func WithReviewedByTag(tags []string, reviewer string) []string {
	reviewer = strings.TrimSpace(reviewer)
	out := make([]string, 0, len(tags)+1)
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if strings.HasPrefix(strings.ToLower(trimmed), ReviewedByPrefix) {
			continue
		}
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if reviewer != "" {
		out = append(out, ReviewedByPrefix+reviewer)
	}
	return out
}

// IsReviewed reports whether the task has completed review (stamped with a "reviewed-by:<tag>" tag,
// or has an assigned reviewer with reviewer notes). Reviewer notes are optional.
// Board posts do not require a review.
func (t *Task) IsReviewed() bool {
	if t == nil || t.IsPost() {
		return true
	}
	if t.ReviewedBy() != "" {
		return true
	}
	return strings.TrimSpace(t.Reviewer) != "" && len(t.ReviewerNotes) > 0
}

// NeedsReview reports whether a task is active/open and still requires a review before it can be closed.
func (t *Task) NeedsReview() bool {
	if t == nil || t.IsPost() || t.IsArchived() {
		return false
	}
	if t.Status == StatusDone || t.Status == StatusClosed {
		return false
	}
	return !t.IsReviewed()
}

// LinkText returns all text that may contain wiki links: the description followed by each note and reviewer note.
func (t *Task) LinkText() string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(t.Description)
	for _, n := range t.Notes {
		b.WriteString("\n")
		b.WriteString(n.Content)
	}
	for _, rn := range t.ReviewerNotes {
		b.WriteString("\n")
		b.WriteString(rn.Content)
	}
	return b.String()
}

// StatusLabel renders the status for display; done posts read as "resolved".
func (t *Task) StatusLabel() string {
	if t == nil {
		return ""
	}
	if t.IsPost() && t.Status == StatusDone {
		return "resolved"
	}
	return string(t.Status)
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
	for _, rn := range t.ReviewerNotes {
		if rn.CreatedAt.After(latest) {
			latest = rn.CreatedAt
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
	if t == nil || t.IsArchived() || t.IsPost() {
		return false
	}
	if t.Status == StatusDone || t.Status == StatusClosed {
		return false
	}
	if strings.EqualFold(string(t.Status), "orphan") || strings.EqualFold(string(t.Status), "orphaned") {
		return true
	}
	if t.OrphanedAt != nil {
		return true
	}
	// Legacy marker from before schema v4 (tags are being phased out).
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
	Status          *Status  // Single-status filter (kept for compatibility; merged with Statuses)
	Statuses        []Status // Multi-status filter: a task matches if its status is any of these (OR)
	Priority        *Priority
	Tag             string
	Assignee        string
	Reviewer        string
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
	Topic           string // Topic path/slug, or "unfiled" for items without a topic; empty = all topics
	TopicRecursive  bool   // Include items in descendant topics of Topic
	Kind            ItemKind
	LinksTo         string // Only items whose text links to this target (ID or @topic)
	Branch          string // Filter by bound git branch
	ExternalRef     string // Filter by external issue reference (e.g. "gh#42")
}

// EffectiveStatuses returns the de-duplicated union of Status and Statuses,
// normalizing the "orphaned" alias to StatusOrphan. Order of first appearance is kept.
func (f *TaskFilter) EffectiveStatuses() []Status {
	var out []Status
	seen := make(map[Status]bool)
	add := func(s Status) {
		if strings.EqualFold(string(s), "orphaned") {
			s = StatusOrphan
		}
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	if f.Status != nil {
		add(*f.Status)
	}
	for _, s := range f.Statuses {
		add(s)
	}
	return out
}

// ParseStatusList parses a comma-separated list of statuses (e.g. "open,in_progress").
// Each element is parsed with ParseStatus; empty elements are ignored and duplicates removed.
// An input containing no statuses at all is an error.
func ParseStatusList(s string) ([]Status, error) {
	var out []Status
	seen := make(map[Status]bool)
	for _, part := range strings.Split(s, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		st, err := ParseStatus(part)
		if err != nil {
			return nil, err
		}
		if !seen[st] {
			seen[st] = true
			out = append(out, st)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("invalid status %q: no status given", s)
	}
	return out, nil
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
