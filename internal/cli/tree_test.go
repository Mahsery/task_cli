package cli

import (
	"strings"
	"testing"
	"time"

	"task_cli/internal/domain"
)

func TestTreeForestAndRendering(t *testing.T) {
	now := time.Now()
	t1 := &domain.Task{
		ID:        "T-1",
		Title:     "Setup Database",
		Status:    domain.StatusDone,
		Priority:  domain.PriorityNormal,
		Blocks:    []string{"T-2"},
		CreatedAt: now,
	}
	t2 := &domain.Task{
		ID:        "T-2",
		Title:     "Build Backend API",
		Status:    domain.StatusInProgress,
		Priority:  domain.PriorityHigh,
		DependsOn: []string{"T-1"},
		Blocks:    []string{"T-3"},
		CreatedAt: now,
	}
	t3 := &domain.Task{
		ID:        "T-3",
		Title:     "Build Web UI",
		Status:    domain.StatusOpen,
		Priority:  domain.PriorityUrgent,
		DependsOn: []string{"T-2"},
		IsBlocked: true,
		BlockedBy: []string{"T-2"},
		CreatedAt: now,
	}
	t4 := &domain.Task{
		ID:        "T-4",
		Title:     "Independent Documentation",
		Status:    domain.StatusOpen,
		Priority:  domain.PriorityLow,
		CreatedAt: now,
	}

	tasks := []*domain.Task{t1, t2, t3, t4}
	taskMap := map[string]*domain.Task{
		t1.ID: t1,
		t2.ID: t2,
		t3.ID: t3,
		t4.ID: t4,
	}

	// 1. Test forward execution forest (Root -> Dependents)
	forest := buildForest(tasks, taskMap, false)
	if len(forest) != 2 {
		t.Fatalf("expected 2 roots (T-1 and T-4), got %d", len(forest))
	}

	root1 := forest[0]
	if root1.Task.ID != "T-1" {
		t.Fatalf("expected first root T-1, got %s", root1.Task.ID)
	}
	if len(root1.Children) != 1 || root1.Children[0].Task.ID != "T-2" {
		t.Fatalf("expected T-1 child to be T-2")
	}
	t2Node := root1.Children[0]
	if len(t2Node.Children) != 1 || t2Node.Children[0].Task.ID != "T-3" {
		t.Fatalf("expected T-2 child to be T-3")
	}

	root2 := forest[1]
	if root2.Task.ID != "T-4" {
		t.Fatalf("expected second root T-4, got %s", root2.Task.ID)
	}
	if len(root2.Children) != 0 {
		t.Fatalf("expected T-4 to have 0 children")
	}

	// 2. Test rendering with Unicode glyphs
	var sb strings.Builder
	for i, root := range forest {
		isLast := (i == len(forest)-1)
		renderTreeNode(&sb, root, "", isLast, true, false)
	}
	rendered := sb.String()

	if !strings.Contains(rendered, "T-1") || !strings.Contains(rendered, "T-2") || !strings.Contains(rendered, "T-3") {
		t.Fatalf("rendered tree missing tasks:\n%s", rendered)
	}
	if !strings.Contains(rendered, "└──") {
		t.Fatalf("rendered tree missing branch glyph:\n%s", rendered)
	}
	if !strings.Contains(rendered, "BLOCKED by T-2") {
		t.Fatalf("rendered tree missing BLOCKED badge for T-3:\n%s", rendered)
	}

	// 3. Test rendering with ASCII glyphs
	var sbASCII strings.Builder
	for i, root := range forest {
		isLast := (i == len(forest)-1)
		renderTreeNode(&sbASCII, root, "", isLast, true, true)
	}
	renderedASCII := sbASCII.String()
	if !strings.Contains(renderedASCII, "\\--") {
		t.Fatalf("rendered ASCII tree missing '\\--' glyph:\n%s", renderedASCII)
	}

	// 4. Test reverse forest (prerequisite breakdown: Root -> DependsOn)
	reverseForest := buildForest(tasks, taskMap, true)
	if len(reverseForest) < 2 {
		t.Fatalf("expected at least 2 roots in reverse forest, got %d", len(reverseForest))
	}
	// T-3 does not block anyone, so T-3 is a root in reverse view
	foundT3Root := false
	for _, r := range reverseForest {
		if r.Task.ID == "T-3" {
			foundT3Root = true
			if len(r.Children) != 1 || r.Children[0].Task.ID != "T-2" {
				t.Fatalf("expected T-3 child to be T-2 in reverse view")
			}
			if len(r.Children[0].Children) != 1 || r.Children[0].Children[0].Task.ID != "T-1" {
				t.Fatalf("expected T-2 child to be T-1 in reverse view")
			}
		}
	}
	if !foundT3Root {
		t.Fatalf("expected T-3 to be a root in reverse tree")
	}
}

func TestBridgeHiddenDependencies(t *testing.T) {
	// Chain: T-1 (open) -> T-2 (done) -> T-3 (done) -> T-4 (open)
	// Diamond: T-1 (open) -> T-2 (done) -> T-6 (open) AND T-1 (open) -> T-5 (open) -> T-6 (open)
	t1 := &domain.Task{ID: "T-1", Title: "Root", Status: domain.StatusOpen, Blocks: []string{"T-2", "T-5"}, TopicPath: "backend"}
	t2 := &domain.Task{ID: "T-2", Title: "Done 1", Status: domain.StatusDone, DependsOn: []string{"T-1"}, Blocks: []string{"T-3", "T-6"}, TopicPath: "backend"}
	t3 := &domain.Task{ID: "T-3", Title: "Done 2", Status: domain.StatusDone, DependsOn: []string{"T-2"}, Blocks: []string{"T-4"}, TopicPath: "backend"}
	t4 := &domain.Task{ID: "T-4", Title: "Bridged Leaf", Status: domain.StatusOpen, DependsOn: []string{"T-3"}, TopicPath: "backend"}
	t5 := &domain.Task{ID: "T-5", Title: "Active Branch", Status: domain.StatusOpen, DependsOn: []string{"T-1"}, Blocks: []string{"T-6"}, TopicPath: "backend"}
	t6 := &domain.Task{ID: "T-6", Title: "Diamond Sink", Status: domain.StatusOpen, DependsOn: []string{"T-2", "T-5"}, TopicPath: "backend"}

	allTasks := []*domain.Task{t1, t2, t3, t4, t5, t6}
	visible := []*domain.Task{t1, t4, t5, t6}

	bridged := bridgeHiddenDependencies(visible, allTasks)
	taskMap := make(map[string]*domain.Task, len(bridged))
	for _, tk := range bridged {
		taskMap[tk.ID] = tk
	}

	// Pass a ghostLookup to verify hidden in-topic tasks (T-2, T-3) are NOT queried as cross-topic ghosts
	forest := buildForestWith(bridged, taskMap, false, func(id string) *domain.Task {
		t.Errorf("unexpected ghost lookup for hidden in-topic task %s", id)
		return nil
	})

	if len(forest) != 1 || forest[0].Task.ID != "T-1" {
		t.Fatalf("expected single root T-1, got %d roots", len(forest))
	}
	root := forest[0]
	if len(root.Children) != 2 {
		t.Fatalf("expected T-1 to have 2 children (T-5 and bridged T-4), got %d", len(root.Children))
	}

	// Verify reverse forest also bridges T-4 -> T-1 across hidden T-3 -> T-2
	revForest := buildForestWith(bridged, taskMap, true, nil)
	foundT4 := false
	for _, r := range revForest {
		if r.Task.ID == "T-4" {
			foundT4 = true
			if len(r.Children) != 1 || r.Children[0].Task.ID != "T-1" {
				t.Fatalf("expected reverse tree under T-4 to bridge to T-1, got %+v", r.Children)
			}
		}
	}
	if !foundT4 {
		t.Fatalf("expected T-4 to be a root in reverse forest")
	}
}
