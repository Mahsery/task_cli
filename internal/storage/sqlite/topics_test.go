package sqlite

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"task_cli/internal/domain"

	_ "modernc.org/sqlite"
)

func mustTopic(t *testing.T, db *DB, title string, kind domain.TopicKind, parent string) *domain.Topic {
	t.Helper()
	tp, err := db.CreateTopic(title, "", "", kind, parent)
	if err != nil {
		t.Fatalf("CreateTopic(%q): %v", title, err)
	}
	return tp
}

func mustItem(t *testing.T, db *DB, p CreateItemParams) *domain.Task {
	t.Helper()
	if p.Scope == "" {
		p.Scope = domain.ScopeLocal
		p.RepoPath = "/repo"
	}
	it, err := db.CreateItem(p)
	if err != nil {
		t.Fatalf("CreateItem(%q): %v", p.Title, err)
	}
	return it
}

func TestTopicTreePathsAndResolution(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	game := mustTopic(t, db, "Game", domain.TopicGroup, "")
	hero := mustTopic(t, db, "Hero System", domain.TopicTasks, "game")
	mustTopic(t, db, "Bug Reports", domain.TopicBoard, "")
	mustTopic(t, db, "UI", domain.TopicTasks, "game")

	if hero.Path != "game/hero-system" || hero.Depth != 1 {
		t.Fatalf("hero path/depth = %q/%d", hero.Path, hero.Depth)
	}

	topics, err := db.ListTopics()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, tp := range topics {
		paths = append(paths, tp.Path)
	}
	// Depth-first order, siblings by creation position.
	want := []string{"game", "game/hero-system", "game/ui", "bug-reports"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("topic order = %v, want %v", paths, want)
	}

	for _, ref := range []string{"game/hero-system", "@Game/Hero-System/", "hero-system", "@hero-system"} {
		got, err := db.GetTopic(ref)
		if err != nil || got.ID != hero.ID {
			t.Errorf("GetTopic(%q) = %v, %v", ref, got, err)
		}
	}
	if _, err := db.CreateTopic("Hero System", "", "", domain.TopicTasks, "game"); err == nil {
		t.Error("duplicate sibling slug must be rejected")
	}
	// Same slug under another parent is allowed but then the bare slug is ambiguous.
	mustTopic(t, db, "Hero System", domain.TopicTasks, "bug-reports")
	if _, err := db.GetTopic("hero-system"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguity error, got %v", err)
	}
	if _, err := db.GetTopic("game/hero-system"); err != nil {
		t.Errorf("full path must still resolve: %v", err)
	}
	if _, err := db.CreateTopic("Unfiled", "", "", domain.TopicTasks, ""); err == nil {
		t.Error("reserved slug 'unfiled' must be rejected")
	}
	_ = game
}

func TestMoveTopicRejectsCycles(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mustTopic(t, db, "A", domain.TopicGroup, "")
	mustTopic(t, db, "B", domain.TopicGroup, "a")
	mustTopic(t, db, "C", domain.TopicTasks, "a/b")

	if _, err := db.MoveTopic("a", "a/b/c"); err == nil {
		t.Error("moving a topic under its own descendant must fail")
	}
	if _, err := db.MoveTopic("a", "a"); err == nil {
		t.Error("moving a topic under itself must fail")
	}
	moved, err := db.MoveTopic("a/b/c", "")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Path != "c" {
		t.Errorf("moved path = %q, want c", moved.Path)
	}
}

func TestItemKindsIDsAndTopicRules(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mustTopic(t, db, "Hero", domain.TopicTasks, "")
	mustTopic(t, db, "Bugs", domain.TopicBoard, "")
	mustTopic(t, db, "Game", domain.TopicGroup, "")

	t1 := mustItem(t, db, CreateItemParams{Title: "task one", Topic: "hero"})
	p1 := mustItem(t, db, CreateItemParams{Title: "post one", Kind: domain.KindPost, Topic: "bugs"})
	t2 := mustItem(t, db, CreateItemParams{Title: "task two"})
	p2 := mustItem(t, db, CreateItemParams{Title: "post two", Kind: domain.KindPost})

	if t1.ID != "T-1" || t2.ID != "T-2" || p1.ID != "P-1" || p2.ID != "P-2" {
		t.Fatalf("IDs = %s %s %s %s; tasks and posts must have independent sequences", t1.ID, t2.ID, p1.ID, p2.ID)
	}
	if t1.TopicPath != "hero" || p1.TopicPath != "bugs" || t2.TopicID != 0 {
		t.Errorf("topic membership wrong: %q %q %d", t1.TopicPath, p1.TopicPath, t2.TopicID)
	}
	if !p1.IsPost() || t1.IsPost() {
		t.Error("kind not persisted")
	}

	if _, err := db.CreateItem(CreateItemParams{Scope: "local", Title: "x", Topic: "bugs"}); err == nil {
		t.Error("task in board must be rejected")
	}
	if _, err := db.CreateItem(CreateItemParams{Scope: "local", Title: "x", Kind: domain.KindPost, Topic: "hero"}); err == nil {
		t.Error("post in tasks topic must be rejected")
	}
	if _, err := db.CreateItem(CreateItemParams{Scope: "local", Title: "x", Topic: "game"}); err == nil {
		t.Error("item in group must be rejected")
	}

	if _, err := db.MoveItems([]string{"T-2"}, "bugs", "me"); err == nil {
		t.Error("moving a task into a board must fail")
	}
	moved, err := db.MoveItems([]string{"T-2"}, "hero", "me")
	if err != nil {
		t.Fatal(err)
	}
	if moved[0].TopicPath != "hero" {
		t.Errorf("move did not apply: %q", moved[0].TopicPath)
	}
	last := moved[0].Notes[len(moved[0].Notes)-1].Content
	if last != "Moved from unfiled to @hero" {
		t.Errorf("move note = %q", last)
	}
	moved, err = db.MoveItems([]string{"T-2"}, "unfiled", "me")
	if err != nil || moved[0].TopicID != 0 {
		t.Fatalf("move to unfiled failed: %v %+v", err, moved)
	}

	// Posts never take part in dependencies or claims.
	if err := db.AddDependency("T-1", "P-1"); err == nil {
		t.Error("task depending on a post must fail")
	}
	if err := db.AddDependency("P-1", "T-1"); err == nil {
		t.Error("post depending on a task must fail")
	}
	if _, err := db.CreateItem(CreateItemParams{Scope: "local", Title: "x", DependsOn: []string{"P-1"}}); err == nil {
		t.Error("creating a task with a post prerequisite must fail")
	}
	if _, err := db.ClaimTask("P-1", "me", false); err == nil {
		t.Error("claiming a post must fail")
	}

	// Filters
	posts, err := db.ListTasks(domain.TaskFilter{Kind: domain.KindPost})
	if err != nil || len(posts) != 2 {
		t.Errorf("kind filter: %v len=%d", err, len(posts))
	}
	unfiled, err := db.ListTasks(domain.TaskFilter{Topic: "unfiled"})
	if err != nil || len(unfiled) != 2 {
		t.Errorf("unfiled filter: %v len=%d", err, len(unfiled))
	}
}

func TestCrossTopicDependencyGraph(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mustTopic(t, db, "Engine", domain.TopicTasks, "")
	mustTopic(t, db, "Hero", domain.TopicTasks, "")
	mustTopic(t, db, "UI", domain.TopicTasks, "")

	core := mustItem(t, db, CreateItemParams{Title: "core", Topic: "engine"})
	var dependents []string
	for i, topic := range []string{"hero", "hero", "ui", "ui", "engine"} {
		it := mustItem(t, db, CreateItemParams{Title: "dep " + string(rune('a'+i)), Topic: topic, DependsOn: []string{core.ID}})
		dependents = append(dependents, it.ID)
	}
	got, err := db.GetTask(core.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 5 {
		t.Fatalf("core should block 5 tasks across 3 topics, got %v", got.Blocks)
	}
	// Cycle across topics is rejected.
	if err := db.AddDependency(core.ID, dependents[2]); err == nil {
		t.Error("cross-topic cycle must be rejected")
	}
	// Topic filter shows only members; deps still reference outsiders.
	ui, err := db.ListTasks(domain.TaskFilter{Topic: "ui"})
	if err != nil || len(ui) != 2 {
		t.Fatalf("ui topic filter: %v len=%d", err, len(ui))
	}
	for _, it := range ui {
		if !reflect.DeepEqual(it.DependsOn, []string{core.ID}) || !it.IsBlocked {
			t.Errorf("%s should depend on and be blocked by %s across topics: %+v", it.ID, core.ID, it.DependsOn)
		}
	}
}

func TestWikiLinkIndexAndBacklinks(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mustTopic(t, db, "Hero", domain.TopicTasks, "")

	a := mustItem(t, db, CreateItemParams{Title: "a"})
	b := mustItem(t, db, CreateItemParams{Title: "b", Description: "needs [[t1#L2]] and [[@hero]] and self [[T-2]], `[[T-99]]` ignored"})
	if !reflect.DeepEqual(b.Links, []string{"@hero", "T-1"}) {
		t.Errorf("b.Links = %v", b.Links)
	}
	got, _ := db.GetTask(a.ID)
	if !reflect.DeepEqual(got.Backlinks, []string{"T-2"}) {
		t.Errorf("a.Backlinks = %v", got.Backlinks)
	}

	// Links added through notes are indexed.
	note := "see [[P-1]] later"
	if _, err := db.UpdateTask(a.ID, nil, nil, nil, nil, nil, nil, nil, nil, &note, "me"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetTask(a.ID)
	if !reflect.DeepEqual(got.Links, []string{"P-1"}) {
		t.Errorf("note links not indexed: %v", got.Links)
	}

	// Editing the description re-indexes (old link removed).
	desc := "nothing here"
	if _, err := db.UpdateTask(b.ID, nil, &desc, nil, nil, nil, nil, nil, nil, nil, "me"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetTask(a.ID)
	if len(got.Backlinks) != 0 {
		t.Errorf("stale backlink after edit: %v", got.Backlinks)
	}

	topicBacklinks, err := db.Backlinks("@hero")
	if err != nil || len(topicBacklinks) != 0 {
		t.Errorf("topic backlinks after edit: %v %v", topicBacklinks, err)
	}
	linkers, err := db.ListTasks(domain.TaskFilter{LinksTo: "P-1"})
	if err != nil || len(linkers) != 1 || linkers[0].ID != a.ID {
		t.Errorf("LinksTo filter: %v %+v", err, linkers)
	}
}

func TestDeleteTopicRequiresDestination(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mustTopic(t, db, "Old", domain.TopicTasks, "")
	mustTopic(t, db, "Sub", domain.TopicTasks, "old")
	mustTopic(t, db, "New", domain.TopicTasks, "")
	mustTopic(t, db, "Board", domain.TopicBoard, "")
	it := mustItem(t, db, CreateItemParams{Title: "x", Topic: "old"})

	if err := db.DeleteTopic("old", ""); err == nil {
		t.Fatal("deleting a non-empty topic without destination must fail")
	}
	if err := db.DeleteTopic("old", "board"); err == nil {
		t.Fatal("moving tasks into a board on delete must fail")
	}
	if err := db.DeleteTopic("old", "old/sub"); err == nil {
		t.Fatal("destination inside the deleted topic must fail")
	}
	if err := db.DeleteTopic("old", "new"); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetTask(it.ID)
	if got.TopicPath != "new" {
		t.Errorf("item should have moved to new, got %q", got.TopicPath)
	}
	sub, err := db.GetTopic("sub")
	if err != nil || sub.Path != "new/sub" {
		t.Errorf("sub-topic should be re-parented under new: %v %+v", err, sub)
	}
	if err := db.DeleteTopic("board", ""); err != nil {
		t.Errorf("empty topic should delete without destination: %v", err)
	}
}

func TestPromotePostLinksBothWays(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mustTopic(t, db, "Bugs", domain.TopicBoard, "")
	mustTopic(t, db, "Hero", domain.TopicTasks, "")
	post := mustItem(t, db, CreateItemParams{Title: "Crash on load", Kind: domain.KindPost, Topic: "bugs", Description: "stack trace", Priority: domain.PriorityHigh})

	if _, _, err := db.PromotePost(post.ID, "bugs", "me"); err == nil {
		t.Error("promoting into a board must fail")
	}
	task, resolved, err := db.PromotePost(post.ID, "hero", "me")
	if err != nil {
		t.Fatal(err)
	}
	if task.Kind != domain.KindTask || task.TopicPath != "hero" || task.Title != post.Title || task.Priority != domain.PriorityHigh {
		t.Errorf("promoted task wrong: %+v", task)
	}
	if !strings.Contains(task.Description, "[[P-1]]") || !reflect.DeepEqual(task.Backlinks, []string{"P-1"}) || !reflect.DeepEqual(task.Links, []string{"P-1"}) {
		t.Errorf("task should link to and be linked from the post: links=%v backlinks=%v", task.Links, task.Backlinks)
	}
	if resolved.Status != domain.StatusDone || resolved.StatusLabel() != "resolved" {
		t.Errorf("post should be resolved, got %s", resolved.Status)
	}
	if _, _, err := db.PromotePost(task.ID, "hero", "me"); err == nil {
		t.Error("promoting a task must fail")
	}
}

func TestDeleteRestoreKeepsKindTopicAndLinks(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mustTopic(t, db, "Bugs", domain.TopicBoard, "")
	target := mustItem(t, db, CreateItemParams{Title: "target"})
	post := mustItem(t, db, CreateItemParams{Title: "p", Kind: domain.KindPost, Topic: "bugs", Description: "[[T-1]]"})

	if err := db.DeleteTask(post.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetTask(target.ID)
	if len(got.Backlinks) != 0 {
		t.Errorf("backlinks must disappear with the deleted item: %v", got.Backlinks)
	}
	restored, err := db.RestoreTaskByID(post.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = restored
	again, _ := db.GetTask(post.ID)
	if again.Kind != domain.KindPost || again.TopicPath != "bugs" {
		t.Errorf("restore lost kind/topic: %s %q", again.Kind, again.TopicPath)
	}
	got, _ = db.GetTask(target.ID)
	if !reflect.DeepEqual(got.Backlinks, []string{post.ID}) {
		t.Errorf("restore must re-index links: %v", got.Backlinks)
	}
	// Post sequence high-water mark survives delete+restore.
	next := mustItem(t, db, CreateItemParams{Title: "p2", Kind: domain.KindPost})
	if next.ID != "P-2" {
		t.Errorf("next post id = %s, want P-2", next.ID)
	}
}

func TestMigrationFromV3AddsTopicsAndIndexesLinks(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v3.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
	CREATE TABLE tasks (
		id TEXT PRIMARY KEY, scope TEXT NOT NULL, repo_path TEXT NOT NULL DEFAULT '', seq_num INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open',
		priority TEXT NOT NULL DEFAULT 'normal', tags TEXT NOT NULL DEFAULT '[]', author TEXT NOT NULL DEFAULT '',
		assignee TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
		closed_at DATETIME, archived_at DATETIME, commit_sha TEXT NOT NULL DEFAULT '', commit_msg TEXT NOT NULL DEFAULT '',
		diff_stat TEXT NOT NULL DEFAULT '', diff_content TEXT NOT NULL DEFAULT '');
	CREATE TABLE task_notes (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		author TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL);
	CREATE TABLE task_sequences (scope TEXT NOT NULL, repo_path TEXT NOT NULL DEFAULT '', last_seq INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (scope, repo_path));
	INSERT INTO tasks (id, scope, repo_path, seq_num, title, description, created_at, updated_at)
		VALUES ('T-1', 'local', '/r', 1, 'one', '', '2026-01-01', '2026-01-01'),
		       ('T-2', 'local', '/r', 2, 'two', 'depends on [[T-1]]', '2026-01-01', '2026-01-01');
	INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES ('n1', 'T-1', 'a', 'cf [[t2#L1]]', '2026-01-01');
	INSERT INTO task_sequences VALUES ('local', '/r', 2);
	PRAGMA user_version = 3;
	`)
	if err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open v3 db: %v", err)
	}
	defer func() { _ = db.Close() }()

	one, err := db.GetTask("T-1")
	if err != nil {
		t.Fatal(err)
	}
	if one.Kind != domain.KindTask || one.TopicID != 0 {
		t.Errorf("migrated task should be an unfiled task: %s %d", one.Kind, one.TopicID)
	}
	if !reflect.DeepEqual(one.Backlinks, []string{"T-2"}) || !reflect.DeepEqual(one.Links, []string{"T-2"}) {
		t.Errorf("migration must backfill link index: links=%v backlinks=%v", one.Links, one.Backlinks)
	}
	next := mustItem(t, db, CreateItemParams{Scope: "local", RepoPath: "/r", Title: "three"})
	if next.ID != "T-3" {
		t.Errorf("task sequence not preserved: %s", next.ID)
	}
	if _, err := db.CreateTopic("Hero", "", "", domain.TopicTasks, ""); err != nil {
		t.Errorf("topics table missing after migration: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "backups", "pre-upgrade-v3-*.db"))
	if len(matches) != 1 {
		t.Errorf("expected a pre-upgrade snapshot, found %v", matches)
	}
}

func TestOrphanMarkerSetByOrphanAndClearedByClaim(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	it := mustItem(t, db, CreateItemParams{Title: "work", Assignee: "a", Status: domain.StatusInProgress, Tags: []string{"keep"}})
	orphaned, err := db.OrphanTask(it.ID, "reaper", "gone")
	if err != nil {
		t.Fatal(err)
	}
	if orphaned.OrphanedAt == nil || !orphaned.IsOrphaned() || !reflect.DeepEqual(orphaned.Tags, []string{"keep"}) {
		t.Fatalf("orphan marker wrong: at=%v tags=%v", orphaned.OrphanedAt, orphaned.Tags)
	}
	claimed, err := db.ClaimTask(it.ID, "b", false)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.OrphanedAt != nil || claimed.IsOrphaned() {
		t.Error("claiming must clear the orphan marker")
	}
}

func TestMigrationConvertsLegacyOrphanTag(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v3.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
	CREATE TABLE tasks (id TEXT PRIMARY KEY, scope TEXT NOT NULL, repo_path TEXT NOT NULL DEFAULT '', seq_num INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open', priority TEXT NOT NULL DEFAULT 'normal',
		tags TEXT NOT NULL DEFAULT '[]', author TEXT NOT NULL DEFAULT '', assignee TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL, closed_at DATETIME, archived_at DATETIME, commit_sha TEXT NOT NULL DEFAULT '',
		commit_msg TEXT NOT NULL DEFAULT '', diff_stat TEXT NOT NULL DEFAULT '', diff_content TEXT NOT NULL DEFAULT '');
	INSERT INTO tasks (id, scope, seq_num, title, tags, created_at, updated_at) VALUES
		('T-1', 'local', 1, 'stale', '["bug","orphaned"]', '2026-01-01', '2026-01-02'),
		('T-2', 'local', 2, 'fine', '["bug"]', '2026-01-01', '2026-01-02');
	PRAGMA user_version = 3;`)
	if err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	one, _ := db.GetTask("T-1")
	if one.OrphanedAt == nil || !reflect.DeepEqual(one.Tags, []string{"bug"}) {
		t.Errorf("legacy tag should become orphaned_at and be stripped: at=%v tags=%v", one.OrphanedAt, one.Tags)
	}
	two, _ := db.GetTask("T-2")
	if two.OrphanedAt != nil || !reflect.DeepEqual(two.Tags, []string{"bug"}) {
		t.Errorf("untagged task must be untouched: at=%v tags=%v", two.OrphanedAt, two.Tags)
	}
}

func TestTopicGroupRollsUpCounts(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	game := mustTopic(t, db, "Game", domain.TopicGroup, "")
	mustTopic(t, db, "Nav", domain.TopicTasks, "game")
	mustTopic(t, db, "Simulation", domain.TopicTasks, "game")

	// Create tasks in nav
	for i := 0; i < 2; i++ {
		task, err := db.CreateTask(domain.ScopeLocal, "", "Nav task open", "", domain.StatusOpen, domain.PriorityNormal, nil, "me", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.MoveItems([]string{task.ID}, "game/nav", "me"); err != nil {
			t.Fatal(err)
		}
	}
	doneTask, err := db.CreateTask(domain.ScopeLocal, "", "Nav task done", "", domain.StatusDone, domain.PriorityNormal, nil, "me", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.MoveItems([]string{doneTask.ID}, "game/nav", "me"); err != nil {
		t.Fatal(err)
	}

	// Create tasks in simulation
	simProg, err := db.CreateTask(domain.ScopeLocal, "", "Sim task active", "", domain.StatusInProgress, domain.PriorityNormal, nil, "me", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.MoveItems([]string{simProg.ID}, "game/simulation", "me"); err != nil {
		t.Fatal(err)
	}

	simClosed, err := db.CreateTask(domain.ScopeLocal, "", "Sim task closed", "", domain.StatusClosed, domain.PriorityNormal, nil, "me", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.MoveItems([]string{simClosed.ID}, "game/simulation", "me"); err != nil {
		t.Fatal(err)
	}

	topics, err := db.ListTopics()
	if err != nil {
		t.Fatal(err)
	}
	bySlug := make(map[string]*domain.Topic)
	for _, tp := range topics {
		bySlug[tp.Slug] = tp
	}

	nav := bySlug["nav"]
	if nav == nil || nav.ItemCount != 3 || nav.OpenCount != 2 {
		t.Fatalf("expected nav items=3 open=2, got %+v", nav)
	}

	sim := bySlug["simulation"]
	if sim == nil || sim.ItemCount != 2 || sim.OpenCount != 1 {
		t.Fatalf("expected simulation items=2 open=1, got %+v", sim)
	}

	gameTopic := bySlug[game.Slug]
	if gameTopic == nil {
		t.Fatalf("game topic not found")
	}
	if gameTopic.ItemCount != 5 {
		t.Errorf("expected rolled-up game ItemCount=5, got %d", gameTopic.ItemCount)
	}
	if gameTopic.OpenCount != 3 {
		t.Errorf("expected rolled-up game OpenCount=3, got %d", gameTopic.OpenCount)
	}

	// Verify ListTasks with Topic="game" returns all 5 tasks even without TopicRecursive: true
	tasks, err := db.ListTasks(domain.TaskFilter{Topic: "game"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 5 {
		t.Fatalf("expected 5 tasks under group 'game', got %d", len(tasks))
	}
}
