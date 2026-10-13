package sqlite

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"task_cli/internal/domain"

	"github.com/jmoiron/sqlx"
)

// execQueryer is satisfied by *sql.Tx and *sql.DB.
type execQueryer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// reindexLinksTx rebuilds the wiki-link index rows for one item from its
// description and notes. It must run inside the transaction that changed the text.
func reindexLinksTx(tx execQueryer, id string) error {
	var scope, description string
	if err := tx.QueryRow("SELECT scope, description FROM tasks WHERE id = ?", id).Scan(&scope, &description); err != nil {
		return fmt.Errorf("failed to load item %s for link indexing: %w", id, err)
	}
	var sb strings.Builder
	sb.WriteString(description)
	rows, err := tx.Query("SELECT content FROM task_notes WHERE task_id = ? ORDER BY created_at ASC", id)
	if err != nil {
		return fmt.Errorf("failed to load notes for link indexing: %w", err)
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			_ = rows.Close()
			return fmt.Errorf("failed to scan note for link indexing: %w", err)
		}
		sb.WriteByte('\n')
		sb.WriteString(c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("failed to iterate notes for link indexing: %w", err)
	}
	_ = rows.Close()

	if _, err := tx.Exec("DELETE FROM task_links WHERE from_id = ?", id); err != nil {
		return fmt.Errorf("failed to clear link index: %w", err)
	}
	now := time.Now().UTC()
	for _, target := range domain.LinkTargets(domain.ParseWikiLinks(sb.String(), scope)) {
		if target == id {
			continue // self-links are not indexed as backlinks
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO task_links (from_id, target, created_at) VALUES (?, ?, ?)", id, target, now); err != nil {
			return fmt.Errorf("failed to index link %s -> %s: %w", id, target, err)
		}
	}
	return nil
}

// reindexAllLinks rebuilds the whole link index (used by the v4 migration backfill).
func (d *DB) reindexAllLinks() error {
	var ids []string
	if err := d.conn.Select(&ids, "SELECT id FROM tasks"); err != nil {
		return err
	}
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if err := reindexLinksTx(tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// hydrateLinks fills Links and Backlinks for the given items.
func hydrateLinks(q sqlx.Queryer, taskMap map[string]*domain.Task, taskIDs []any) error {
	query, args, err := sqlx.In(`
		SELECT from_id, target FROM task_links WHERE from_id IN (?) OR target IN (?)
		ORDER BY from_id, target
	`, taskIDs, taskIDs)
	if err != nil {
		return fmt.Errorf("failed to build links query: %w", err)
	}
	var rows []struct {
		FromID string `db:"from_id"`
		Target string `db:"target"`
	}
	if err := sqlx.Select(q, &rows, query, args...); err != nil {
		return fmt.Errorf("failed to query links: %w", err)
	}
	for _, r := range rows {
		if t, ok := taskMap[r.FromID]; ok {
			t.Links = append(t.Links, r.Target)
		}
		if t, ok := taskMap[r.Target]; ok {
			t.Backlinks = append(t.Backlinks, r.FromID)
		}
	}
	for _, t := range taskMap {
		sort.Strings(t.Backlinks)
	}
	return nil
}

// Backlinks returns the IDs of items whose text links to target (an item ID or "@topic/path").
func (d *DB) Backlinks(target string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if strings.HasPrefix(target, "@") {
		target = "@" + domain.NormalizeTopicRef(target)
	}
	out := []string{}
	if err := d.conn.Select(&out, "SELECT from_id FROM task_links WHERE target = ? ORDER BY from_id", target); err != nil {
		return nil, fmt.Errorf("failed to query backlinks: %w", err)
	}
	return out, nil
}
