package search

import (
	"testing"
	"time"

	"task_cli/internal/domain"
)

func TestSearchMatch(t *testing.T) {
	task1 := &domain.Task{
		ID:          "T-1",
		Title:       "Fix Odin compiler issue",
		Description: "Memory leak in lexer when parsing imports",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityHigh,
		Tags:        []string{"bug", "odin", "compiler"},
		Assignee:    "mehmet",
		Author:      "alice",
		Notes: []domain.TaskNote{
			{Content: "Heap dump analyzed, issue is in token pool"},
		},
	}

	task2 := &domain.Task{
		ID:          "T-2",
		Title:       "Add documentation for CLI",
		Description: "Write comprehensive manual for task commands",
		Status:      domain.StatusDone,
		Priority:    domain.PriorityNormal,
		Tags:        []string{"docs"},
		Assignee:    "john",
		Author:      "bob",
	}

	taskOrphan := &domain.Task{
		ID:        "T-3",
		Title:     "Old Feature",
		Status:    domain.StatusInProgress,
		Assignee:  "alice",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}

	tests := []struct {
		name     string
		query    string
		task     *domain.Task
		expected bool
	}{
		{"empty query matches all", "", task1, true},
		{"status orphan matches orphaned task", "status:orphan", taskOrphan, true},
		{"short status orphan matches orphaned task", "s:orphan", taskOrphan, true},
		{"status orphaned matches orphaned task", "status:orphaned", taskOrphan, true},
		{"status orphan mismatch on active task", "status:orphan", task1, false},
		{"orphan keyword matches orphaned task", "orphan", taskOrphan, true},
		{"single word in title", "odin", task1, true},
		{"single word in tag", "compiler", task1, true},
		{"single word in tag for task1 but not task2", "compiler", task2, false},
		{"search in notes", "token", task1, true},
		{"explicit tag qualifier match", "tag:odin", task1, true},
		{"short tag qualifier match", "t:odin", task1, true},
		{"explicit tag qualifier mismatch", "tag:rust", task1, false},
		{"status qualifier", "s:open", task1, true},
		{"status qualifier mismatch", "s:done", task1, false},
		{"priority qualifier", "p:high", task1, true},
		{"assignee qualifier", "a:mehmet", task1, true},
		{"boolean AND match", "bug AND odin", task1, true},
		{"boolean AND mismatch", "bug AND rust", task1, false},
		{"implicit AND", "bug odin", task1, true},
		{"boolean OR match first", "odin OR rust", task1, true},
		{"boolean OR match second", "rust OR docs", task2, true},
		{"boolean OR mismatch", "rust OR java", task1, false},
		{"boolean NOT match", "odin NOT rust", task1, true},
		{"boolean NOT mismatch", "odin NOT compiler", task1, false},
		{"leading minus negation", "odin -rust", task1, true},
		{"leading minus negation fail", "odin -bug", task1, false},
		{"quoted phrase match", `"memory leak"`, task1, true},
		{"quoted phrase mismatch", `"memory corruption"`, task1, false},
		{"parenthesized expression", `(bug OR feature) AND p:high`, task1, true},
		{"id match", "T-1", task1, true},
		{"id qualifier match", "id:T-1", task1, true},
		{"id qualifier forgiving t1", "id:t1", task1, true},
		{"id qualifier forgiving 1", "id:1", task1, true},
		{"id qualifier forgiving t-1", "id:t-1", task1, true},
		{"id qualifier forgiving mismatch", "id:t2", task1, false},
		{"general term forgiving t1", "t1", task1, true},
		{"general term forgiving t2 matches task2", "t2", task2, true},
		{"leading binary AND does not panic", "AND bug", task1, true},
		{"leading binary OR does not panic", "OR odin", task1, true},
		{"colon double-quoted tag qualifier", `tag:"compiler"`, task1, true},
		{"colon single-quoted tag qualifier", `tag:'odin'`, task1, true},
		{"colon quoted mismatch", `tag:"rust lang"`, task1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Match(tt.query, tt.task)
			if got != tt.expected {
				t.Errorf("Match(%q, task) = %v; want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func BenchmarkSearchMatch_Simple(b *testing.B) {
	task := &domain.Task{
		ID:          "T-1",
		Title:       "Fix Odin compiler issue",
		Description: "Memory leak in lexer when parsing imports",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityHigh,
		Tags:        []string{"bug", "odin", "compiler"},
		Assignee:    "mehmet",
		Author:      "alice",
		Notes: []domain.TaskNote{
			{Content: "Heap dump analyzed, issue is in token pool"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Match("odin", task)
	}
}

func BenchmarkSearchMatch_ComplexDSL(b *testing.B) {
	task := &domain.Task{
		ID:          "T-1",
		Title:       "Fix Odin compiler issue",
		Description: "Memory leak in lexer when parsing imports",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityHigh,
		Tags:        []string{"bug", "odin", "compiler"},
		Assignee:    "mehmet",
		Author:      "alice",
		Notes: []domain.TaskNote{
			{Content: "Heap dump analyzed, issue is in token pool"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Match("(bug OR feature) AND p:high AND t:odin", task)
	}
}

func BenchmarkSearchMatch_Scale1000(b *testing.B) {
	tasks := make([]*domain.Task, 1000)
	for i := 0; i < 1000; i++ {
		tasks[i] = &domain.Task{
			ID:          "T-1",
			Title:       "Fix Odin compiler issue",
			Description: "Memory leak in lexer when parsing imports",
			Status:      domain.StatusOpen,
			Priority:    domain.PriorityHigh,
			Tags:        []string{"bug", "odin", "compiler"},
			Assignee:    "mehmet",
			Author:      "alice",
		}
	}

	query := "bug AND odin"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matched := 0
		for _, t := range tasks {
			if Match(query, t) {
				matched++
			}
		}
	}
}

func TestSearchDSLNilGuards(t *testing.T) {
	task := &domain.Task{ID: "T-1", Title: "Test"}

	var nilAnd *andNode
	if !nilAnd.Eval(task) {
		t.Error("nil andNode should evaluate to true safely")
	}

	partialAnd := &andNode{left: nil, right: &termNode{val: "Test"}}
	if !partialAnd.Eval(task) {
		t.Error("partial andNode with nil left should evaluate safely")
	}

	var nilOr *orNode
	if nilOr.Eval(task) {
		t.Error("nil orNode should evaluate to false safely")
	}

	partialOr := &orNode{left: nil, right: &termNode{val: "Test"}}
	if !partialOr.Eval(task) {
		t.Error("partial orNode with nil left should evaluate safely")
	}

	var nilNot *notNode
	if !nilNot.Eval(task) {
		t.Error("nil notNode should evaluate to true safely")
	}
}

func TestTopicKindAndLinkQualifiers(t *testing.T) {
	hero := &domain.Task{ID: "T-1", Scope: "local", Kind: domain.KindTask, TopicPath: "game/hero-system", Links: []string{"P-2", "@ui"}}
	post := &domain.Task{ID: "P-2", Scope: "local", Kind: domain.KindPost, TopicPath: "bug-reports", Backlinks: []string{"T-1"}}
	loose := &domain.Task{ID: "T-3", Scope: "local"}

	cases := []struct {
		q    string
		task *domain.Task
		want bool
	}{
		{"topic:game", hero, true},
		{"topic:game/hero-system", hero, true},
		{"topic:hero-system", hero, true},
		{"topic:hero", hero, false},
		{"topic:@Bug-Reports", post, true},
		{"topic:unfiled", loose, true},
		{"topic:unfiled", hero, false},
		{"kind:post", post, true},
		{"kind:post", hero, false},
		{"kind:task", loose, true},
		{"links:p2", hero, true},
		{"links:@ui", hero, true},
		{"links:T-9", hero, false},
		{"linkedfrom:t1", post, true},
		{"kind:post AND topic:bug-reports", post, true},
		{"NOT kind:post", hero, true},
	}
	for _, c := range cases {
		if got := Compile(c.q).Match(c.task); got != c.want {
			t.Errorf("%q on %s = %v, want %v", c.q, c.task.ID, got, c.want)
		}
	}
}
