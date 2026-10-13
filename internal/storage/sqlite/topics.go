package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"task_cli/internal/domain"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// topicTree is an in-memory snapshot of all topics with computed paths.
type topicTree struct {
	ordered  []*domain.Topic // depth-first tree order
	byID     map[int64]*domain.Topic
	byPath   map[string]*domain.Topic
	children map[int64][]*domain.Topic
}

const topicSelect = `SELECT id, slug, title, description, kind, parent_id, position, created_at, updated_at, archived_at FROM topics`

func loadTopicTree(q sqlx.Queryer) (*topicTree, error) {
	var topics []*domain.Topic
	if err := sqlx.Select(q, &topics, topicSelect); err != nil {
		return nil, fmt.Errorf("failed to query topics: %w", err)
	}
	children := make(map[int64][]*domain.Topic)
	t := &topicTree{
		byID:     make(map[int64]*domain.Topic, len(topics)),
		byPath:   make(map[string]*domain.Topic, len(topics)),
		children: children,
	}
	for _, tp := range topics {
		t.byID[tp.ID] = tp
	}
	for _, tp := range topics {
		var parent int64
		if tp.ParentID != nil {
			if _, ok := t.byID[*tp.ParentID]; ok {
				parent = *tp.ParentID
			}
		}
		children[parent] = append(children[parent], tp)
	}
	for _, list := range children {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Position != list[j].Position {
				return list[i].Position < list[j].Position
			}
			return list[i].Slug < list[j].Slug
		})
	}
	var walk func(parent int64, prefix string, depth int)
	walk = func(parent int64, prefix string, depth int) {
		for _, tp := range children[parent] {
			tp.Path = tp.Slug
			if prefix != "" {
				tp.Path = prefix + "/" + tp.Slug
			}
			tp.Depth = depth
			tp.ChildCount = len(children[tp.ID])
			t.ordered = append(t.ordered, tp)
			t.byPath[tp.Path] = tp
			walk(tp.ID, tp.Path, depth+1)
		}
	}
	walk(0, "", 0)

	var counts []struct {
		TopicID int64 `db:"topic_id"`
		Items   int   `db:"items"`
		Open    int   `db:"open"`
	}
	if err := sqlx.Select(q, &counts, `
		SELECT topic_id, COUNT(*) AS items,
		       COALESCE(SUM(CASE WHEN status IN ('open', 'in_progress') THEN 1 ELSE 0 END), 0) AS open
		FROM tasks
		WHERE topic_id IS NOT NULL AND archived_at IS NULL AND status != 'archived'
		GROUP BY topic_id
	`); err != nil {
		return nil, fmt.Errorf("failed to count topic items: %w", err)
	}
	for _, c := range counts {
		if tp, ok := t.byID[c.TopicID]; ok {
			tp.ItemCount = c.Items
			tp.OpenCount = c.Open
		}
	}

	// For groups, roll up ItemCount and OpenCount from all descendant non-group topics
	// using a single bottom-up post-order DFS (O(T) linear time).
	visited := make(map[int64]bool, len(topics))
	var rollup func(id int64) (items int, open int)
	rollup = func(id int64) (int, int) {
		if visited[id] {
			return 0, 0
		}
		visited[id] = true
		tp := t.byID[id]
		if tp == nil {
			return 0, 0
		}
		if tp.Kind != domain.TopicGroup {
			return tp.ItemCount, tp.OpenCount
		}
		groupItems, groupOpen := 0, 0
		for _, child := range t.children[id] {
			cItems, cOpen := rollup(child.ID)
			groupItems += cItems
			groupOpen += cOpen
		}
		tp.ItemCount = groupItems
		tp.OpenCount = groupOpen
		return groupItems, groupOpen
	}

	for _, tp := range t.children[0] {
		rollup(tp.ID)
	}

	return t, nil
}

// resolve finds a topic by numeric ID, exact path, or unique slug.
func (t *topicTree) resolve(ref string) (*domain.Topic, error) {
	norm := domain.NormalizeTopicRef(ref)
	if norm == "" {
		return nil, fmt.Errorf("topic reference cannot be empty")
	}
	if tp, ok := t.byPath[norm]; ok {
		return tp, nil
	}
	if id, err := strconv.ParseInt(norm, 10, 64); err == nil {
		if tp, ok := t.byID[id]; ok {
			return tp, nil
		}
	}
	var matches []*domain.Topic
	for _, tp := range t.ordered {
		if tp.Slug == norm || strings.HasSuffix(tp.Path, "/"+norm) {
			matches = append(matches, tp)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("topic %q not found", ref)
	case 1:
		return matches[0], nil
	default:
		paths := make([]string, len(matches))
		for i, m := range matches {
			paths[i] = m.Path
		}
		return nil, fmt.Errorf("topic %q is ambiguous: %s", ref, strings.Join(paths, ", "))
	}
}

// descendants returns the IDs of the topic and all topics below it.
func (t *topicTree) descendants(id int64) []int64 {
	out := []int64{id}
	for i := 0; i < len(out); i++ {
		for _, child := range t.children[out[i]] {
			out = append(out, child.ID)
		}
	}
	return out
}

func (t *topicTree) pathOf(id int64) string {
	if tp, ok := t.byID[id]; ok {
		return tp.Path
	}
	return ""
}

// ListTopics returns all topics in depth-first tree order with computed paths and counts.
func (d *DB) ListTopics() ([]*domain.Topic, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	tree, err := d.getTopicTreeLocked(d.conn)
	if err != nil {
		return nil, err
	}
	if tree.ordered == nil {
		return []*domain.Topic{}, nil
	}
	return tree.ordered, nil
}

// GetTopic resolves a topic by path, unique slug or numeric ID.
func (d *DB) GetTopic(ref string) (*domain.Topic, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	tree, err := d.getTopicTreeLocked(d.conn)
	if err != nil {
		return nil, err
	}
	return tree.resolve(ref)
}

// CreateTopic creates a topic. slug defaults to NormalizeSlug(title); parentRef "" creates a root topic.
func (d *DB) CreateTopic(title, slug, description string, kind domain.TopicKind, parentRef string) (*domain.Topic, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("topic title cannot be empty")
	}
	if slug = strings.TrimSpace(slug); slug == "" {
		slug = domain.NormalizeSlug(title)
	}
	if err := domain.ValidateSlug(slug); err != nil {
		return nil, err
	}
	if kind == "" {
		kind = domain.TopicTasks
	}
	if _, err := domain.ParseTopicKind(string(kind)); err != nil {
		return nil, err
	}

	tree, err := d.getTopicTreeLocked(d.conn)
	if err != nil {
		return nil, err
	}
	var parentID *int64
	path := slug
	if strings.TrimSpace(parentRef) != "" {
		parent, err := tree.resolve(parentRef)
		if err != nil {
			return nil, err
		}
		parentID = &parent.ID
		path = parent.Path + "/" + slug
	}
	if _, exists := tree.byPath[path]; exists {
		return nil, fmt.Errorf("topic %q already exists", path)
	}

	now := time.Now().UTC()
	res, err := d.conn.Exec(`INSERT INTO topics (slug, title, description, kind, parent_id, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM topics WHERE COALESCE(parent_id, 0) = COALESCE(?, 0)), ?, ?)`,
		slug, title, strings.TrimSpace(description), string(kind), parentID, parentID, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create topic: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to read new topic id: %w", err)
	}
	return d.topicByIDLocked(id)
}

func (d *DB) topicByIDLocked(id int64) (*domain.Topic, error) {
	d.invalidateTopicCacheLocked()
	tree, err := d.getTopicTreeLocked(d.conn)
	if err != nil {
		return nil, err
	}
	tp, ok := tree.byID[id]
	if !ok {
		return nil, fmt.Errorf("topic %d not found", id)
	}
	return tp, nil
}

// UpdateTopic changes a topic's title, slug, description or kind. A kind change is refused
// if the topic contains items the new kind does not accept.
func (d *DB) UpdateTopic(ref string, title, slug, description *string, kind *domain.TopicKind) (*domain.Topic, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tree, err := d.getTopicTreeLocked(d.conn)
	if err != nil {
		return nil, err
	}
	tp, err := tree.resolve(ref)
	if err != nil {
		return nil, err
	}

	updates := []string{"updated_at = ?"}
	args := []any{time.Now().UTC()}
	if title != nil {
		t := strings.TrimSpace(*title)
		if t == "" {
			return nil, fmt.Errorf("topic title cannot be empty")
		}
		updates = append(updates, "title = ?")
		args = append(args, t)
	}
	if slug != nil && *slug != tp.Slug {
		s := strings.TrimSpace(*slug)
		if err := domain.ValidateSlug(s); err != nil {
			return nil, err
		}
		newPath := s
		if tp.ParentID != nil {
			newPath = tree.pathOf(*tp.ParentID) + "/" + s
		}
		if _, exists := tree.byPath[newPath]; exists {
			return nil, fmt.Errorf("topic %q already exists", newPath)
		}
		updates = append(updates, "slug = ?")
		args = append(args, s)
	}
	if description != nil {
		updates = append(updates, "description = ?")
		args = append(args, strings.TrimSpace(*description))
	}
	if kind != nil && *kind != tp.Kind {
		if _, err := domain.ParseTopicKind(string(*kind)); err != nil {
			return nil, err
		}
		var incompatible int
		if err := d.conn.QueryRow(`SELECT COUNT(*) FROM tasks WHERE topic_id = ?`, tp.ID).Scan(&incompatible); err != nil {
			return nil, fmt.Errorf("failed to inspect topic items: %w", err)
		}
		if incompatible > 0 {
			var bad int
			if err := d.conn.QueryRow(`SELECT COUNT(*) FROM tasks WHERE topic_id = ? AND kind != ?`, tp.ID, string(itemKindFor(*kind))).Scan(&bad); err != nil {
				return nil, fmt.Errorf("failed to inspect topic items: %w", err)
			}
			if *kind == domain.TopicGroup || bad > 0 {
				return nil, fmt.Errorf("cannot change topic %q to %s: it contains %d item(s) a %s topic cannot hold (move them first)", tp.Path, *kind, incompatible, *kind)
			}
		}
		updates = append(updates, "kind = ?")
		args = append(args, string(*kind))
	}
	args = append(args, tp.ID)
	query := fmt.Sprintf("UPDATE topics SET %s WHERE id = ?", strings.Join(updates, ", "))
	if _, err := d.conn.Exec(query, args...); err != nil {
		return nil, fmt.Errorf("failed to update topic: %w", err)
	}
	return d.topicByIDLocked(tp.ID)
}

func itemKindFor(k domain.TopicKind) domain.ItemKind {
	if k == domain.TopicBoard {
		return domain.KindPost
	}
	return domain.KindTask
}

// MoveTopic re-parents a topic. parentRef "" moves it to the root. Moving a topic
// into itself or one of its descendants is refused.
func (d *DB) MoveTopic(ref, parentRef string) (*domain.Topic, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tree, err := d.getTopicTreeLocked(d.conn)
	if err != nil {
		return nil, err
	}
	tp, err := tree.resolve(ref)
	if err != nil {
		return nil, err
	}
	var parentID *int64
	newPath := tp.Slug
	if strings.TrimSpace(parentRef) != "" {
		parent, err := tree.resolve(parentRef)
		if err != nil {
			return nil, err
		}
		for _, id := range tree.descendants(tp.ID) {
			if id == parent.ID {
				return nil, fmt.Errorf("cannot move topic %q into itself or its own sub-topic %q", tp.Path, parent.Path)
			}
		}
		parentID = &parent.ID
		newPath = parent.Path + "/" + tp.Slug
	}
	if existing, exists := tree.byPath[newPath]; exists && existing.ID != tp.ID {
		return nil, fmt.Errorf("topic %q already exists", newPath)
	}
	if _, err := d.conn.Exec(`UPDATE topics SET parent_id = ?, updated_at = ? WHERE id = ?`, parentID, time.Now().UTC(), tp.ID); err != nil {
		return nil, fmt.Errorf("failed to move topic: %w", err)
	}
	return d.topicByIDLocked(tp.ID)
}

// DeleteTopic removes a topic. A topic that still holds items or sub-topics is only
// deleted when moveTo is given: items move there (or become unfiled when moveTo is
// "unfiled") and sub-topics are re-parented to it (or to the root).
func (d *DB) DeleteTopic(ref, moveTo string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tree, err := d.getTopicTreeLocked(d.conn)
	if err != nil {
		return err
	}
	tp, err := tree.resolve(ref)
	if err != nil {
		return err
	}
	var itemCount int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM tasks WHERE topic_id = ?`, tp.ID).Scan(&itemCount); err != nil {
		return fmt.Errorf("failed to inspect topic items: %w", err)
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if itemCount > 0 || tp.ChildCount > 0 {
		moveTo = strings.TrimSpace(moveTo)
		if moveTo == "" {
			return fmt.Errorf("topic %q contains %d item(s) and %d sub-topic(s); pass a destination (--move-to <topic> or --move-to unfiled)", tp.Path, itemCount, tp.ChildCount)
		}
		var destID *int64
		if domain.NormalizeTopicRef(moveTo) != domain.UnfiledTopic {
			dest, err := tree.resolve(moveTo)
			if err != nil {
				return err
			}
			for _, id := range tree.descendants(tp.ID) {
				if id == dest.ID {
					return fmt.Errorf("destination %q is inside the topic being deleted", dest.Path)
				}
			}
			if itemCount > 0 {
				var bad int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE topic_id = ? AND kind != ?`, tp.ID, string(itemKindFor(dest.Kind))).Scan(&bad); err != nil {
					return fmt.Errorf("failed to inspect topic items: %w", err)
				}
				if dest.Kind == domain.TopicGroup || bad > 0 {
					return fmt.Errorf("destination %q (%s) cannot hold the items of %q", dest.Path, dest.Kind, tp.Path)
				}
			}
			destID = &dest.ID
		}
		if _, err := tx.Exec(`UPDATE tasks SET topic_id = ?, updated_at = ? WHERE topic_id = ?`, destID, time.Now().UTC(), tp.ID); err != nil {
			return fmt.Errorf("failed to move topic items: %w", err)
		}

		if tp.ChildCount > 0 {
			var childSlugs []string
			rows, err := tx.Query(`SELECT slug FROM topics WHERE parent_id = ?`, tp.ID)
			if err != nil {
				return fmt.Errorf("failed to inspect sub-topics: %w", err)
			}
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					_ = rows.Close()
					return fmt.Errorf("failed to scan sub-topic slug: %w", err)
				}
				childSlugs = append(childSlugs, s)
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return fmt.Errorf("failed reading sub-topic slugs: %w", err)
			}

			for _, slug := range childSlugs {
				var collision int
				err := tx.QueryRow(
					`SELECT COUNT(*) FROM topics WHERE COALESCE(parent_id, 0) = COALESCE(?, 0) AND slug = ? AND id != ?`,
					destID, slug, tp.ID,
				).Scan(&collision)
				if err != nil {
					return fmt.Errorf("failed to check slug collision: %w", err)
				}
				if collision > 0 {
					destName := "root"
					if destID != nil {
						destName = moveTo
					}
					return fmt.Errorf("cannot delete topic %q: sub-topic slug %q already exists in destination %q", tp.Path, slug, destName)
				}
			}

			if _, err := tx.Exec(`UPDATE topics SET parent_id = ? WHERE parent_id = ?`, destID, tp.ID); err != nil {
				return fmt.Errorf("failed to re-parent sub-topics: %w", err)
			}
		}
	}
	if _, err := tx.Exec(`DELETE FROM topics WHERE id = ?`, tp.ID); err != nil {
		return fmt.Errorf("failed to delete topic: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit delete topic: %w", err)
	}
	d.invalidateTopicCacheLocked()
	return nil
}

// MoveItems moves tasks/posts into a topic ("unfiled" or "" clears the topic). Each move
// is validated against the destination kind and recorded as a note on the item.
func (d *DB) MoveItems(ids []string, topicRef, actor string) ([]*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tree, err := d.getTopicTreeLocked(d.conn)
	if err != nil {
		return nil, err
	}
	var dest *domain.Topic
	if norm := domain.NormalizeTopicRef(topicRef); norm != "" && norm != domain.UnfiledTopic {
		dest, err = tree.resolve(topicRef)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	for _, id := range ids {
		var kind string
		var topicID sql.NullInt64
		if err := tx.QueryRow(`SELECT kind, topic_id FROM tasks WHERE id = ?`, id).Scan(&kind, &topicID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("item %s not found", id)
			}
			return nil, fmt.Errorf("failed to query item %s: %w", id, err)
		}
		from := "unfiled"
		if topicID.Valid {
			if p := tree.pathOf(topicID.Int64); p != "" {
				from = "@" + p
			}
		}
		var destID *int64
		to := "unfiled"
		if dest != nil {
			if !dest.Kind.Accepts(domain.ItemKind(kind)) {
				return nil, fmt.Errorf("cannot move %s %s into %s topic %q", kind, id, dest.Kind, dest.Path)
			}
			if topicID.Valid && topicID.Int64 == dest.ID {
				continue
			}
			destID = &dest.ID
			to = "@" + dest.Path
		} else if !topicID.Valid {
			continue
		}
		if _, err := tx.Exec(`UPDATE tasks SET topic_id = ?, updated_at = ? WHERE id = ?`, destID, now, id); err != nil {
			return nil, fmt.Errorf("failed to move %s: %w", id, err)
		}
		note := fmt.Sprintf("Moved from %s to %s", from, to)
		if _, err := tx.Exec(`INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`,
			uuid.New().String(), id, actor, note, now); err != nil {
			return nil, fmt.Errorf("failed to record move note: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit move: %w", err)
	}
	d.invalidateTopicCacheLocked()

	out := make([]*domain.Task, 0, len(ids))
	for _, id := range ids {
		t, err := d.getTaskLocked(d.conn, id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// resolveTopicForItem returns the topic ID an item of the given kind should be created in.
// An empty ref means unfiled.
func resolveTopicForItem(q sqlx.Queryer, ref string, kind domain.ItemKind) (*int64, error) {
	norm := domain.NormalizeTopicRef(ref)
	if norm == "" || norm == domain.UnfiledTopic {
		return nil, nil
	}
	tree, err := loadTopicTree(q)
	if err != nil {
		return nil, err
	}
	tp, err := tree.resolve(ref)
	if err != nil {
		return nil, err
	}
	if !tp.Kind.Accepts(kind) {
		return nil, fmt.Errorf("%s topic %q cannot hold %ss", tp.Kind, tp.Path, kind)
	}
	return &tp.ID, nil
}

// PromotePost turns a post into a new task in topicRef. The task copies the post's
// title, description and priority and links back with [[P-n]]; the post is resolved
// with a note linking to the new task. Both directions show up as wiki links/backlinks.
func (d *DB) PromotePost(postID, topicRef, author string) (task, post *domain.Task, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	src, err := d.getTaskLocked(d.conn, postID)
	if err != nil {
		return nil, nil, err
	}
	if !src.IsPost() {
		return nil, nil, fmt.Errorf("%s is not a post", postID)
	}
	if strings.TrimSpace(author) == "" {
		author = "system"
	}

	desc := strings.TrimRight(src.Description, "\n")
	if desc != "" {
		desc += "\n\n"
	}
	desc += fmt.Sprintf("Promoted from [[%s]]", src.ID)

	task, err = d.createItemLocked(CreateItemParams{
		Scope: src.Scope, RepoPath: src.RepoPath, Kind: domain.KindTask, Topic: topicRef,
		Title: src.Title, Description: desc, Status: domain.StatusOpen, Priority: src.Priority,
		Author: author,
	})
	if err != nil {
		return nil, nil, err
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE tasks SET status = ?, closed_at = ?, updated_at = ? WHERE id = ?`,
		string(domain.StatusDone), now, now, src.ID); err != nil {
		return nil, nil, fmt.Errorf("failed to resolve post: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`,
		uuid.New().String(), src.ID, author, fmt.Sprintf("Promoted to [[%s]]", task.ID), now); err != nil {
		return nil, nil, fmt.Errorf("failed to record promotion note: %w", err)
	}
	if err := reindexLinksTx(tx, src.ID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit promotion: %w", err)
	}
	d.invalidateTopicCacheLocked()

	if task, err = d.getTaskLocked(d.conn, task.ID); err != nil {
		return nil, nil, err
	}
	if post, err = d.getTaskLocked(d.conn, src.ID); err != nil {
		return nil, nil, err
	}
	return task, post, nil
}
