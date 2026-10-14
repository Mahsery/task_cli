package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"task_cli/internal/domain"
)

func TestPatchSeriesFormattingAndTopoSort(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	t2 := &domain.Task{
		ID:          "T-2",
		Title:       "Second layer",
		Description: "Depends on T-1",
		Status:      domain.StatusDone,
		Priority:    domain.PriorityHigh,
		DependsOn:   []string{"T-1"},
		CreatedAt:   now.Add(time.Minute),
		UpdatedAt:   now.Add(2 * time.Minute),
		Commits: []domain.TaskCommit{
			{
				SHA:         "2222222abcdef",
				Subject:     "feat: second layer [T-2]",
				Author:      "alice <alice@example.com>",
				CommittedAt: "2026-10-14T00:02:00Z",
				DiffStat:    " b.go | 2 ++",
				DiffContent: "diff --git a/b.go b/b.go\n+package b",
			},
		},
	}
	t1 := &domain.Task{
		ID:          "T-1",
		Title:       "Foundation layer",
		Description: "Base specification",
		Status:      domain.StatusDone,
		Priority:    domain.PriorityUrgent,
		Blocks:      []string{"T-2"},
		Branch:      "tk/t-1-foundation-layer",
		CreatedAt:   now,
		UpdatedAt:   now.Add(time.Minute),
		Notes: []domain.TaskNote{
			{Author: "alice", Content: "Verified edge cases"},
		},
		Commits: []domain.TaskCommit{
			{
				SHA:         "1111111abcdef",
				Subject:     "feat: foundation [T-1]",
				Author:      "alice <alice@example.com>",
				CommittedAt: "2026-10-14T00:01:00Z",
				DiffStat:    " a.go | 1 +",
				DiffContent: "diff --git a/a.go b/a.go\n+package a",
			},
		},
	}

	sorted := topoSortTasksForPatch([]*domain.Task{t2, t1})
	if len(sorted) != 2 || sorted[0].ID != "T-1" || sorted[1].ID != "T-2" {
		t.Fatalf("expected topoSortTasksForPatch to order [T-1, T-2], got [%s, %s]", sorted[0].ID, sorted[1].ID)
	}

	entries := collectPatchEntries(sorted)
	if len(entries) != 2 {
		t.Fatalf("expected 2 patch entries, got %d", len(entries))
	}

	cover := formatCoverLetter("@core patch series", sorted, entries)
	if !strings.Contains(cover, "Subject: [PATCH 0/2] @core patch series") {
		t.Errorf("cover letter missing subject header:\n%s", cover)
	}
	if !strings.Contains(cover, "Branch: tk/t-1-foundation-layer") || !strings.Contains(cover, "Verified edge cases") {
		t.Errorf("cover letter missing branch or notes:\n%s", cover)
	}
	if !strings.Contains(cover, "[1/2] 1111111 feat: foundation [T-1] (T-1)") || !strings.Contains(cover, "[2/2] 2222222 feat: second layer [T-2] (T-2)") {
		t.Errorf("cover letter missing commit table:\n%s", cover)
	}

	p1 := formatPatchEntry(&entries[0], 1, 2)
	if !strings.Contains(p1, "Subject: [PATCH 1/2] feat: foundation [T-1]") || !strings.Contains(p1, "Task-ID: T-1") || !strings.Contains(p1, "+package a") {
		t.Errorf("unexpected patch 1 output:\n%s", p1)
	}

	// Verify command registration and flags
	cmd, _, err := RootCmd.Find([]string{"patch"})
	if err != nil || cmd == nil {
		t.Fatalf("patch command not registered: %v", err)
	}
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("expected -o shorthand flag on patch command")
	}

	// Verify patchSlug helper
	if got := patchSlug("feat(git): Add Kernel Patch Series [T-85]!"); got != "feat-git-add-kernel-patch-series-t-85" {
		t.Errorf("unexpected patchSlug: %q", got)
	}

	// Verify writing to output dir works cleanly
	outDir := filepath.Join(t.TempDir(), "patches")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatal(err)
	}
	coverFile := filepath.Join(outDir, "0000-cover-letter.patch")
	if err := os.WriteFile(coverFile, []byte(cover), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(coverFile); err != nil {
		t.Fatalf("expected cover letter file to exist: %v", err)
	}
}
