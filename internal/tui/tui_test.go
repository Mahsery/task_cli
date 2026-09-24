package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"task_cli/internal/domain"
)

func TestTUIViewResponsiveDimensions(t *testing.T) {
	testTasks := []*domain.Task{
		{
			ID:          "T-1",
			Scope:       "local",
			Title:       "Add mandatory tk workflow and grx quickstart to AGENTS.md with extended instructions",
			Description: "Scope: update only AGENTS.md with mandatory task tracking, a concise tk tutorial, and grx guidance for complex searches. Acceptance: commands verified against installed CLIs; preserve existing user instructions.",
			Status:      domain.StatusInProgress,
			Priority:    domain.PriorityHigh,
			Tags:        []string{"docs", "workflow", "quickstart", "kernel"},
			Author:      "agent-instructions-20260924",
			Assignee:    "agent-instructions-20260924",
			CreatedAt:   time.Now().Add(-5 * time.Minute),
			Notes: []domain.TaskNote{
				{
					Author:    "agent-instructions-20260924",
					Content:   "Claimed task as 'agent-instructions-20260924' and verified background socket listener is ready.",
					CreatedAt: time.Now().Add(-4 * time.Minute),
				},
				{
					Author:    "agent-instructions-20260924",
					Content:   "Added mandatory tk workflow and quickstart at the top of AGENTS.md, plus grx proximity, Boolean, and filename examples.",
					CreatedAt: time.Now().Add(-2 * time.Minute),
				},
			},
			CommitSHA: "f0d6b9e",
			CommitMsg: "docs: require tk task coordination and add search quickstart",
			DiffStat:  "1 file changed, 43 insertions(+)",
			DiffContent: `--- a/AGENTS.md
+++ b/AGENTS.md
@@ -1,3 +1,5 @@
+## Mandatory Task Coordination
+Use tk for managing tasks.`,
		},
		{
			ID:        "T-2",
			Scope:     "local",
			Title:     "Short task",
			Status:    domain.StatusOpen,
			Priority:  domain.PriorityNormal,
			CreatedAt: time.Now().Add(-30 * time.Minute),
		},
		{
			ID:        "T-3",
			Scope:     "local",
			Title:     "Completed task with commit",
			Status:    domain.StatusDone,
			Priority:  domain.PriorityLow,
			Tags:      []string{"cleanup"},
			CommitSHA: "a1b2c3d",
			CommitMsg: "refactor: clean up unused structs",
			CreatedAt: time.Now().Add(-2 * time.Hour),
		},
	}

	widths := []int{55, 65, 80, 95, 120, 160}
	heights := []int{20, 24, 30, 45}

	for _, w := range widths {
		for _, h := range heights {
			m := Model{
				tasks:    testTasks,
				cursor:   0,
				width:    w,
				height:   h,
				theme:    NewKanagawaTheme(),
				scope:    "local",
				repoPath: "/test/repo",
			}

			output := m.View()
			if output == "" {
				t.Fatalf("View() returned empty string for %dx%d", w, h)
			}

			lines := strings.Split(output, "\n")
			for i, line := range lines {
				// Strip trailing newline effects and evaluate visual width
				lineLen := lipgloss.Width(line)
				if lineLen > w {
					t.Errorf("Width %d x Height %d: Line %d exceeds width (actual: %d): %q", w, h, i, lineLen, line)
				}
			}
		}
	}
}

func TestTUIConfirmationView(t *testing.T) {
	m := Model{
		tasks: []*domain.Task{
			{ID: "T-1", Title: "Task to delete", Status: domain.StatusOpen, Priority: domain.PriorityNormal},
		},
		cursor:        0,
		width:         80,
		height:        24,
		theme:         NewKanagawaTheme(),
		confirmDelete: true,
		deleteTaskID:  "T-1",
	}

	out := m.View()
	if !strings.Contains(out, "Delete task T-1 permanently?") && !strings.Contains(out, "Delete T-1? [y/N]") {
		t.Errorf("Expected confirmation prompt in View(), got:\n%s", out)
	}

	// Narrow window
	m.width = 40
	outNarrow := m.View()
	if !strings.Contains(outNarrow, "Delete T-1? [y/N]") {
		t.Errorf("Expected compact confirmation prompt in narrow View(), got:\n%s", outNarrow)
	}
}
