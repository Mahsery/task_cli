package search

import (
	"testing"

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

	tests := []struct {
		name     string
		query    string
		task     *domain.Task
		expected bool
	}{
		{"empty query matches all", "", task1, true},
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
