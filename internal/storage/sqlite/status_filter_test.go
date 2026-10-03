package sqlite

import (
	"slices"
	"sort"
	"testing"
	"time"

	"task_cli/internal/domain"
)

// statusFixture creates a set of tasks covering every status plus two kinds of
// orphans and returns their IDs keyed by a descriptive name.
func statusFixture(t *testing.T, db *DB) map[string]string {
	t.Helper()
	const repo = "/repo/status"
	ids := make(map[string]string)
	mk := func(name string, st domain.Status, tags []string, assignee string) string {
		task, err := db.CreateTask("local", repo, name, "", st, domain.PriorityNormal, tags, "tester", assignee)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		ids[name] = task.ID
		return task.ID
	}
	mk("open", domain.StatusOpen, nil, "")
	mk("open-tagged-orphan", domain.StatusOpen, []string{"orphaned"}, "")
	mk("prog-active", domain.StatusInProgress, nil, "alice")
	stale := mk("prog-stale", domain.StatusInProgress, nil, "bob")
	mk("done", domain.StatusDone, nil, "")
	mk("closed", domain.StatusClosed, nil, "")

	old := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := db.conn.Exec("UPDATE tasks SET created_at = ?, updated_at = ? WHERE id = ?", old, old, stale); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	return ids
}

func listNames(t *testing.T, db *DB, ids map[string]string, f domain.TaskFilter) []string {
	t.Helper()
	f.Scope = "local"
	f.RepoPath = "/repo/status"
	tasks, err := db.ListTasks(f)
	if err != nil {
		t.Fatalf("ListTasks(%+v): %v", f, err)
	}
	byID := make(map[string]string, len(ids))
	for name, id := range ids {
		byID[id] = name
	}
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, byID[task.ID])
	}
	sort.Strings(names)
	return names
}

func assertNames(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%s: got %v, want %v", label, got, want)
	}
}

func TestListTasks_MultiStatusFilter(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ids := statusFixture(t, db)

	assertNames(t, "open,in_progress",
		listNames(t, db, ids, domain.TaskFilter{Statuses: []domain.Status{domain.StatusOpen, domain.StatusInProgress}}),
		"open", "open-tagged-orphan", "prog-active", "prog-stale")

	assertNames(t, "done,closed",
		listNames(t, db, ids, domain.TaskFilter{Statuses: []domain.Status{domain.StatusDone, domain.StatusClosed}}),
		"done", "closed")

	// Single-element Statuses behaves like the legacy single Status field.
	assertNames(t, "statuses=[done]",
		listNames(t, db, ids, domain.TaskFilter{Statuses: []domain.Status{domain.StatusDone}}),
		"done")
	done := domain.StatusDone
	assertNames(t, "status=done",
		listNames(t, db, ids, domain.TaskFilter{Status: &done}),
		"done")

	// Legacy Status and Statuses are merged (union).
	assertNames(t, "status=done + statuses=[closed]",
		listNames(t, db, ids, domain.TaskFilter{Status: &done, Statuses: []domain.Status{domain.StatusClosed}}),
		"done", "closed")

	// Duplicates are harmless.
	assertNames(t, "open,open",
		listNames(t, db, ids, domain.TaskFilter{Statuses: []domain.Status{domain.StatusOpen, domain.StatusOpen}}),
		"open", "open-tagged-orphan")
}

func TestListTasks_MultiStatusWithOrphan(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ids := statusFixture(t, db)

	// Lone orphan keeps OnlyOrphaned semantics.
	assertNames(t, "orphan",
		listNames(t, db, ids, domain.TaskFilter{Statuses: []domain.Status{domain.StatusOrphan}}),
		"open-tagged-orphan", "prog-stale")
	assertNames(t, "orphaned alias",
		listNames(t, db, ids, domain.TaskFilter{Statuses: []domain.Status{"orphaned"}}),
		"open-tagged-orphan", "prog-stale")

	// Mixed: union of concrete statuses and orphaned tasks.
	assertNames(t, "open,orphan",
		listNames(t, db, ids, domain.TaskFilter{Statuses: []domain.Status{domain.StatusOpen, domain.StatusOrphan}}),
		"open", "open-tagged-orphan", "prog-stale")
	assertNames(t, "done,orphan",
		listNames(t, db, ids, domain.TaskFilter{Statuses: []domain.Status{domain.StatusDone, domain.StatusOrphan}}),
		"done", "open-tagged-orphan", "prog-stale")

	// Limit is applied after the in-memory orphan union, not in SQL.
	got := listNames(t, db, ids, domain.TaskFilter{
		Statuses: []domain.Status{domain.StatusDone, domain.StatusOrphan},
		Sort:     "id",
		Limit:    2,
	})
	if len(got) != 2 {
		t.Fatalf("limit 2 with done,orphan: got %v", got)
	}
	for _, n := range got {
		if n != "done" && n != "open-tagged-orphan" && n != "prog-stale" {
			t.Fatalf("limit 2 with done,orphan returned non-matching task %q (%v)", n, got)
		}
	}
}
