package domain

import (
	"fmt"
	"strings"
	"time"
)

// ItemKind distinguishes tasks (claimable, dependency-chained work) from posts
// (board entries such as bug reports that have no lifecycle beyond open/resolved/closed).
type ItemKind string

const (
	KindTask ItemKind = "task"
	KindPost ItemKind = "post"
)

// ParseItemKind parses "task"/"post" (and plurals); empty input means task.
func ParseItemKind(s string) (ItemKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "task", "tasks", "t":
		return KindTask, nil
	case "post", "posts", "p":
		return KindPost, nil
	default:
		return KindTask, fmt.Errorf("invalid item kind %q (allowed: task, post)", s)
	}
}

// TopicKind determines what a topic may contain.
type TopicKind string

const (
	TopicBoard TopicKind = "board" // holds posts
	TopicTasks TopicKind = "tasks" // holds tasks with dependency chains
	TopicGroup TopicKind = "group" // holds only sub-topics
)

// ParseTopicKind parses a topic kind; empty input means a tasks topic.
func ParseTopicKind(s string) (TopicKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "tasks", "task", "t":
		return TopicTasks, nil
	case "board", "posts", "post", "b":
		return TopicBoard, nil
	case "group", "folder", "g":
		return TopicGroup, nil
	default:
		return TopicTasks, fmt.Errorf("invalid topic kind %q (allowed: tasks, board, group)", s)
	}
}

// Accepts reports whether an item of the given kind may live in a topic of this kind.
func (k TopicKind) Accepts(item ItemKind) bool {
	switch k {
	case TopicTasks:
		return item == KindTask || item == ""
	case TopicBoard:
		return item == KindPost
	default:
		return false
	}
}

// UnfiledTopic is the reserved reference for items that belong to no topic.
const UnfiledTopic = "unfiled"

// Topic is a node in the per-database topic tree.
type Topic struct {
	ID          int64      `json:"id" db:"id"`
	Slug        string     `json:"slug" db:"slug"`
	Title       string     `json:"title" db:"title"`
	Description string     `json:"description,omitempty" db:"description"`
	Kind        TopicKind  `json:"kind" db:"kind"`
	ParentID    *int64     `json:"parent_id,omitempty" db:"parent_id"`
	Position    int        `json:"position" db:"position"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty" db:"archived_at"`

	// Computed fields
	Path       string `json:"path" db:"-"`        // slash-joined slugs from the root, e.g. "game/hero-system"
	Depth      int    `json:"depth" db:"-"`       // 0 for root topics
	ItemCount  int    `json:"item_count" db:"-"`  // non-archived items directly in this topic
	OpenCount  int    `json:"open_count" db:"-"`  // open/in-progress items directly in this topic
	ChildCount int    `json:"child_count" db:"-"` // direct sub-topics
}

// NormalizeSlug derives a URL-style slug from free text: lower-case ASCII
// letters/digits separated by single dashes ("Hero System!" -> "hero-system").
func NormalizeSlug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ValidateSlug rejects empty slugs, slugs that are not already normalized, and reserved names.
func ValidateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("topic slug cannot be empty")
	}
	if NormalizeSlug(slug) != slug {
		return fmt.Errorf("invalid topic slug %q (use lower-case letters, digits, '-' or '_')", slug)
	}
	if slug == UnfiledTopic || slug == "all" {
		return fmt.Errorf("topic slug %q is reserved", slug)
	}
	return nil
}

// NormalizeTopicRef cleans a user-supplied topic reference: strips a leading '@',
// surrounding slashes and whitespace, and lower-cases it ("@Game/Hero-System/" -> "game/hero-system").
func NormalizeTopicRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "@")
	ref = strings.Trim(ref, "/")
	return strings.ToLower(ref)
}
