package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"task_cli/internal/domain"
)

func BenchmarkRestoreTask(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	task := &domain.Task{
		ID:          "T-100",
		Scope:       "local",
		RepoPath:    "/repo/test",
		Title:       "Restored Task",
		Description: "Detailed description of task",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityNormal,
		Tags:        []string{"tag1", "tag2"},
		Author:      "Benchmarker",
		Assignee:    "Worker",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Notes: []domain.TaskNote{
			{ID: "n-1", Author: "Benchmarker", Content: "Note 1", CreatedAt: time.Now()},
			{ID: "n-2", Author: "Benchmarker", Content: "Note 2", CreatedAt: time.Now()},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task.ID = fmt.Sprintf("T-%d", i+1)
		task.Notes = []domain.TaskNote{
			{ID: fmt.Sprintf("n-%d-1", i+1), Author: "Benchmarker", Content: "Note 1", CreatedAt: time.Now()},
			{ID: fmt.Sprintf("n-%d-2", i+1), Author: "Benchmarker", Content: "Note 2", CreatedAt: time.Now()},
		}
		if err := db.RestoreTask(task); err != nil {
			b.Fatalf("restore failed at %d: %v", i, err)
		}
	}
}

func BenchmarkBackupDB(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	// Seed 100 tasks with notes
	for i := 0; i < 100; i++ {
		t, err := db.CreateTask("local", "/repo/test", fmt.Sprintf("Task %d", i), "Desc", domain.StatusOpen, domain.PriorityNormal, []string{"bench"}, "Benchmarker", "")
		if err != nil {
			b.Fatalf("seed failed: %v", err)
		}
		note := "Progress note"
		_, _ = db.UpdateTask(t.ID, nil, nil, nil, nil, nil, nil, nil, nil, &note, "Benchmarker")
	}

	tmpDir := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		targetPath := filepath.Join(tmpDir, fmt.Sprintf("backup-%d.db", i))
		if err := db.Backup(targetPath); err != nil {
			b.Fatalf("backup failed: %v", err)
		}
		_ = os.Remove(targetPath)
	}
}

func BenchmarkListTopics_Flat(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	for i := 0; i < 50; i++ {
		_, err := db.CreateTopic(fmt.Sprintf("Topic %d", i), fmt.Sprintf("topic-%d", i), "desc", domain.TopicTasks, "")
		if err != nil {
			b.Fatalf("failed to create topic: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topics, err := db.ListTopics()
		if err != nil {
			b.Fatalf("failed to list topics: %v", err)
		}
		if len(topics) != 50 {
			b.Fatalf("expected 50 topics, got %d", len(topics))
		}
	}
}

func BenchmarkListTopics_DeepHierarchy(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	// Create 15-level deep nested groups, with leaf task topics
	parent := ""
	for i := 0; i < 15; i++ {
		slug := fmt.Sprintf("group-%d", i)
		title := fmt.Sprintf("Group %d", i)
		_, err := db.CreateTopic(title, slug, "Group description", domain.TopicGroup, parent)
		if err != nil {
			b.Fatalf("failed to create group: %v", err)
		}
		leafSlug := fmt.Sprintf("leaf-%d", i)
		leafTitle := fmt.Sprintf("Leaf %d", i)
		_, err = db.CreateTopic(leafTitle, leafSlug, "Leaf description", domain.TopicTasks, slug)
		if err != nil {
			b.Fatalf("failed to create leaf: %v", err)
		}
		parent = slug
	}

	// Seed some tasks in leaf topics
	for i := 0; i < 100; i++ {
		leafRef := fmt.Sprintf("leaf-%d", i%15)
		_, err := db.CreateItem(CreateItemParams{
			Scope:    "local",
			RepoPath: "/repo/test",
			Kind:     domain.KindTask,
			Topic:    leafRef,
			Title:    fmt.Sprintf("Task in %s", leafRef),
			Status:   domain.StatusOpen,
			Priority: domain.PriorityNormal,
			Author:   "Benchmarker",
		})
		if err != nil {
			b.Fatalf("failed to create task: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topics, err := db.ListTopics()
		if err != nil {
			b.Fatalf("failed to list topics: %v", err)
		}
		if len(topics) != 30 {
			b.Fatalf("expected 30 topics, got %d", len(topics))
		}
	}
}

func BenchmarkParseWikiLinks_LargeDescription(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		if i%10 == 0 {
			fmt.Fprintf(&sb, "This line links to [[T-%d]] and topic [[@group/topic-%d#L10]].\n", i, i)
		} else if i%25 == 0 {
			sb.WriteString("```\nHere is some code [[T-ignore]] inside fence\n```\n")
		} else {
			sb.WriteString("Regular description line with some standard content describing the task requirements.\n")
		}
	}
	content := sb.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		links := domain.ParseWikiLinks(content, "local")
		if len(links) == 0 {
			b.Fatalf("expected parsed links, got 0")
		}
	}
}
