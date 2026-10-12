package cli

import (
	"strings"
	"testing"

	"task_cli/internal/domain"
)

func TestNormIDForgivingForms(t *testing.T) {
	old := scope
	defer func() { scope = old }()

	scope = domain.ScopeLocal
	for _, in := range []string{"T-15", "t-15", "t15", "T15", "15", " t15 "} {
		if got := normID(in); got != "T-15" {
			t.Errorf("normID(%q) = %q, want T-15", in, got)
		}
	}
	if got := normID("p3"); got != "P-3" {
		t.Errorf("normID(p3) = %q", got)
	}
	if got := normID("not-an-id"); got != "not-an-id" {
		t.Errorf("invalid input must pass through, got %q", got)
	}
	scope = domain.ScopeGlobal
	if got := normID("7"); got != "G-7" {
		t.Errorf("bare number in global scope = %q, want G-7", got)
	}
}

func TestAnnotateLinksNumbersInReadingOrder(t *testing.T) {
	text := "see [[T-1]] and `[[T-9]]` then [[t2#L3|x]]"
	got, n := annotateLinks(text, 4, domain.ScopeLocal)
	want := "see [[T-1]][5] and `[[T-9]]` then [[t2#L3|x]][6]"
	if got != want || n != 6 {
		t.Errorf("annotateLinks = %q,%d want %q,6", got, n, want)
	}
	if got, n := annotateLinks("no links", 2, domain.ScopeLocal); got != "no links" || n != 2 {
		t.Errorf("text without links must be unchanged: %q %d", got, n)
	}
}

func TestNumberLines(t *testing.T) {
	got := numberLines(strings.Repeat("x\n", 9) + "y")
	lines := strings.Split(got, "\n")
	if len(lines) != 10 || lines[0] != " 1 │ x" || lines[9] != "10 │ y" {
		t.Errorf("numberLines produced %q", lines)
	}
}

func TestTreeRendersSharedChildrenOnceAndGhostsOtherTopics(t *testing.T) {
	// T-1 unblocks T-2 and T-3; T-2 also unblocks T-3 (diamond). T-1 also unblocks
	// T-9, which lives in another topic and is not part of the rendered set.
	t1 := &domain.Task{ID: "T-1", Title: "core", Status: domain.StatusOpen, Blocks: []string{"T-2", "T-3", "T-9"}, TopicPath: "engine"}
	t2 := &domain.Task{ID: "T-2", Title: "stats", Status: domain.StatusOpen, DependsOn: []string{"T-1"}, Blocks: []string{"T-3"}}
	t3 := &domain.Task{ID: "T-3", Title: "abilities", Status: domain.StatusOpen, DependsOn: []string{"T-1", "T-2"}}
	t9 := &domain.Task{ID: "T-9", Title: "hud", Status: domain.StatusOpen, DependsOn: []string{"T-1"}, TopicPath: "ui"}
	tasks := []*domain.Task{t1, t2, t3}
	taskMap := map[string]*domain.Task{"T-1": t1, "T-2": t2, "T-3": t3}
	lookup := func(id string) *domain.Task {
		if id == "T-9" {
			return t9
		}
		return nil
	}

	forest := buildForestWith(tasks, taskMap, false, lookup)
	if len(forest) != 1 {
		t.Fatalf("expected a single root, got %d", len(forest))
	}
	var sb strings.Builder
	renderTreeNode(&sb, forest[0], "", true, true, false)
	out := sb.String()

	if c := strings.Count(out, "T-3 [OPEN]"); c != 1 {
		t.Errorf("T-3 must be rendered in full exactly once, got %d:\n%s", c, out)
	}
	if !strings.Contains(out, "↪ T-3 abilities (shown above)") {
		t.Errorf("second occurrence of T-3 must be a pointer row:\n%s", out)
	}
	if !strings.Contains(out, "⇢ T-9 hud [OPEN] @ui") {
		t.Errorf("cross-topic dependent must be a ghost row labelled with its topic:\n%s", out)
	}

	// Without a lookup (no topic selected) there are no ghost rows.
	plain := buildForest(tasks, taskMap, false)
	sb.Reset()
	renderTreeNode(&sb, plain[0], "", true, true, false)
	if strings.Contains(sb.String(), "T-9") {
		t.Errorf("ghost rows must only appear when a topic is selected:\n%s", sb.String())
	}
}

func TestTreeRootListsOutOfTopicPrerequisites(t *testing.T) {
	t2 := &domain.Task{ID: "T-2", Title: "stats", Status: domain.StatusOpen, DependsOn: []string{"T-1"}, IsBlocked: true, BlockedBy: []string{"T-1"}}
	t1 := &domain.Task{ID: "T-1", Title: "core", TopicPath: "engine"}
	forest := buildForestWith([]*domain.Task{t2}, map[string]*domain.Task{"T-2": t2}, false, func(id string) *domain.Task {
		if id == "T-1" {
			return t1
		}
		return nil
	})
	if len(forest) != 1 || len(forest[0].After) != 1 || forest[0].After[0] != "T-1 @engine" {
		t.Fatalf("root should record its out-of-topic prerequisite, got %+v", forest[0].After)
	}
	var sb strings.Builder
	renderTreeNode(&sb, forest[0], "", true, true, false)
	if !strings.Contains(sb.String(), "⇠ after T-1 @engine") {
		t.Errorf("rendered root missing upstream hint:\n%s", sb.String())
	}
}

func TestRenderTopicTree(t *testing.T) {
	old := scope
	defer func() { scope = old }()
	scope = domain.ScopeLocal

	topics := []*domain.Topic{
		{Slug: "game", Path: "game", Kind: domain.TopicGroup, ChildCount: 1},
		{Slug: "hero-system", Path: "game/hero-system", Kind: domain.TopicTasks, Depth: 1, ItemCount: 4, OpenCount: 3},
		{Slug: "bugs", Path: "bugs", Kind: domain.TopicBoard, ItemCount: 2, OpenCount: 1},
	}
	out := renderTopicTree(topics, 5, "game/hero-system")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected header + 3 topics + unfiled, got:\n%s", out)
	}
	if !strings.HasPrefix(lines[2], "*") || !strings.Contains(lines[2], "hero-system") || !strings.Contains(lines[2], "3 open / 4 tasks") {
		t.Errorf("current topic must be marked with counts: %q", lines[2])
	}
	if !strings.Contains(lines[3], "1 open / 2 posts") {
		t.Errorf("board counts posts: %q", lines[3])
	}
	if !strings.Contains(lines[4], "unfiled") || !strings.Contains(lines[4], "5 item(s)") {
		t.Errorf("unfiled bucket missing: %q", lines[4])
	}
	if strings.Index(lines[2], "hero-system") <= strings.Index(lines[1], "game") {
		t.Errorf("child topic must be indented deeper than its parent:\n%s", out)
	}
}

func TestTagDeprecationFlags(t *testing.T) {
	defer func() {
		if f := newCmd.Flags().Lookup("tags"); f != nil {
			f.Changed = false
		}
		if f := editCmd.Flags().Lookup("tags"); f != nil {
			f.Changed = false
		}
		if f := listCmd.Flags().Lookup("tag"); f != nil {
			f.Changed = false
		}
	}()

	// Test newCmd with --tags
	_ = newCmd.Flags().Set("tags", "legacy")
	err := newCmd.RunE(newCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "tags were replaced by topics") {
		t.Fatalf("expected deprecation error for new --tags, got: %v", err)
	}

	// Test editCmd with --tags
	_ = editCmd.Flags().Set("tags", "legacy")
	err = editCmd.RunE(editCmd, []string{"T-1"})
	if err == nil || !strings.Contains(err.Error(), "tags were replaced by topics") {
		t.Fatalf("expected deprecation error for edit --tags, got: %v", err)
	}

	// Test listCmd with --tag
	_ = listCmd.Flags().Set("tag", "legacy")
	err = listCmd.RunE(listCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "tags were replaced by topics") {
		t.Fatalf("expected deprecation error for list --tag, got: %v", err)
	}
}

func TestListPostsAndTasksFlags(t *testing.T) {
	defer func() {
		listPosts = false
		listTasks = false
		listKind = ""
	}()

	// Both --posts and --tasks
	listPosts = true
	listTasks = true
	err := listCmd.RunE(listCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot specify both --posts and --tasks") {
		t.Fatalf("expected error for both --posts and --tasks, got: %v", err)
	}

	// Conflicting --posts and --kind task
	listPosts = true
	listTasks = false
	listKind = "task"
	err = listCmd.RunE(listCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "conflicting options") {
		t.Fatalf("expected conflicting options error for --posts and --kind task, got: %v", err)
	}

	// Conflicting --tasks and --kind post
	listPosts = false
	listTasks = true
	listKind = "post"
	err = listCmd.RunE(listCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "conflicting options") {
		t.Fatalf("expected conflicting options error for --tasks and --kind post, got: %v", err)
	}
}
