package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/git"

	"github.com/spf13/cobra"
)

type StatusSummary struct {
	Total      int `json:"total"`
	Open       int `json:"open"`
	InProgress int `json:"in_progress"`
	Done       int `json:"done"`
	Orphaned   int `json:"orphaned"`
	Archived   int `json:"archived"`
	Ready      int `json:"ready"`
	Blocked    int `json:"blocked"`
}

type StatusReport struct {
	Scope         string         `json:"scope"`
	RepoPath      string         `json:"repo_path,omitempty"`
	Branch        string         `json:"branch,omitempty"`
	Actor         string         `json:"actor,omitempty"`
	Summary       StatusSummary  `json:"summary"`
	ActiveTasks   []*domain.Task `json:"active_tasks"`
	OrphanedTasks []*domain.Task `json:"orphaned_tasks"`
	ReadyTasks    []*domain.Task `json:"ready_tasks"`
	BlockedTasks  []*domain.Task `json:"blocked_tasks"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "High-signal repository status overview",
	Long: `Provides a comprehensive overview of repository tasks:
- Scope and repository path / git branch
- Status summary counts (Open, In Progress, Done, Orphaned, Archived)
- Blocked and Ready summary counts
- Active in-progress tasks
- Unarchived orphaned tasks with action hints`,
	Example: `  tk status
  tk status --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer taskClient.Close()

		actor := cfg.Actor
		if flagActor != "" {
			actor = flagActor
		}
		if actor == "" {
			actor = git.GetAuthor()
		}

		branch := ""
		if scope == "local" && repoRoot != "" {
			branch = getGitBranch(repoRoot)
		}

		allTasks, err := taskClient.ListTasks(domain.TaskFilter{
			Scope:           scope,
			RepoPath:        repoRoot,
			IncludeArchived: true,
		})
		if err != nil {
			return fmt.Errorf("failed to fetch tasks: %w", err)
		}

		var (
			openCount       int
			inProgressCount int
			doneCount       int
			orphanedCount   int
			archivedCount   int
			readyCount      int
			blockedCount    int

			activeTasks   []*domain.Task
			orphanedTasks []*domain.Task
			readyTasks    []*domain.Task
			blockedTasks  []*domain.Task
		)

		for _, t := range allTasks {
			if t.IsArchived() {
				archivedCount++
				continue
			}

			// Orphan check: unarchived tasks that are tagged 'orphaned' or stale unassigned
			if isTaskOrphaned(t) {
				orphanedCount++
				orphanedTasks = append(orphanedTasks, t)
			}

			switch t.Status {
			case domain.StatusInProgress:
				inProgressCount++
				activeTasks = append(activeTasks, t)
			case domain.StatusOpen:
				openCount++
				if t.IsBlocked {
					blockedCount++
					blockedTasks = append(blockedTasks, t)
				} else {
					readyCount++
					readyTasks = append(readyTasks, t)
				}
			case domain.StatusDone, domain.StatusClosed:
				doneCount++
			default:
				openCount++
			}
		}

		report := StatusReport{
			Scope:    scope,
			RepoPath: repoRoot,
			Branch:   branch,
			Actor:    actor,
			Summary: StatusSummary{
				Total:      len(allTasks),
				Open:       openCount,
				InProgress: inProgressCount,
				Done:       doneCount,
				Orphaned:   orphanedCount,
				Archived:   archivedCount,
				Ready:      readyCount,
				Blocked:    blockedCount,
			},
			ActiveTasks:   activeTasks,
			OrphanedTasks: orphanedTasks,
			ReadyTasks:    readyTasks,
			BlockedTasks:  blockedTasks,
		}

		if jsonFlag {
			if report.ActiveTasks == nil {
				report.ActiveTasks = []*domain.Task{}
			}
			if report.OrphanedTasks == nil {
				report.OrphanedTasks = []*domain.Task{}
			}
			if report.ReadyTasks == nil {
				report.ReadyTasks = []*domain.Task{}
			}
			if report.BlockedTasks == nil {
				report.BlockedTasks = []*domain.Task{}
			}
			return outputJSON(report)
		}

		renderStatusText(report)
		return nil
	},
}

func isTaskOrphaned(t *domain.Task) bool {
	if t == nil || t.IsArchived() {
		return false
	}
	for _, tag := range t.Tags {
		if strings.EqualFold(tag, "orphaned") {
			return true
		}
	}
	return t.IsOrphaned()
}

func getGitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	b := strings.TrimSpace(string(out))
	if b == "HEAD" {
		cmdSHA := exec.Command("git", "rev-parse", "--short", "HEAD")
		if dir != "" {
			cmdSHA.Dir = dir
		}
		if outSHA, err := cmdSHA.Output(); err == nil {
			return fmt.Sprintf("detached at %s", strings.TrimSpace(string(outSHA)))
		}
	}
	return b
}

func renderStatusText(r StatusReport) {
	sep := strings.Repeat("─", 80)
	if IsASCIIOnly() {
		sep = strings.Repeat("-", 80)
	}

	fmt.Println(sep)
	if r.Scope == "local" {
		fmt.Printf("Repository Status (local):\n")
		if r.RepoPath != "" {
			fmt.Printf("  Repo:    %s\n", r.RepoPath)
		}
		if r.Branch != "" {
			fmt.Printf("  Branch:  %s\n", r.Branch)
		}
	} else {
		fmt.Printf("Status (global scope):\n")
	}
	if r.Actor != "" {
		fmt.Printf("  Actor:   %s\n", r.Actor)
	}
	fmt.Println(sep)

	// Summary
	fmt.Printf("Summary:\n")
	fmt.Printf("  Open: %d  •  Active: %d  •  Done: %d  •  Orphaned: %d  •  Archived: %d\n",
		r.Summary.Open, r.Summary.InProgress, r.Summary.Done, r.Summary.Orphaned, r.Summary.Archived)
	fmt.Printf("  Ready: %d  •  Blocked: %d\n", r.Summary.Ready, r.Summary.Blocked)
	fmt.Println()

	// Active Tasks
	fmt.Printf("Active Tasks (%d):\n", len(r.ActiveTasks))
	if len(r.ActiveTasks) == 0 {
		fmt.Println("  (No tasks currently in progress)")
	} else {
		for _, t := range r.ActiveTasks {
			assigneeDesc := "unassigned"
			if t.Assignee != "" {
				if t.Assignee == r.Actor {
					assigneeDesc = fmt.Sprintf("assigned to you (%s)", t.Assignee)
				} else {
					assigneeDesc = fmt.Sprintf("assigned to %s", t.Assignee)
				}
			}
			age := formatAge(t.CreatedAt)
			fmt.Printf("  • %-6s [%-6s] %s\n", t.ID, t.Priority, t.Title)
			fmt.Printf("    └─ %s, started %s\n", assigneeDesc, age)
		}
	}
	fmt.Println()

	// Orphaned Tasks
	if len(r.OrphanedTasks) > 0 {
		fmt.Printf("⚠️  Orphaned Tasks (%d):\n", len(r.OrphanedTasks))
		for _, t := range r.OrphanedTasks {
			fmt.Printf("  • %-6s [%-6s] %s (age: %s)\n", t.ID, t.Priority, t.Title, formatAge(t.CreatedAt))
			fmt.Printf("    └─ Hint: 'tk takeover %s' to claim, or 'tk archive %s' to archive\n", t.ID, t.ID)
		}
		fmt.Println()
	}

	// Blocked Tasks
	if len(r.BlockedTasks) > 0 {
		fmt.Printf("🔒 Blocked Tasks (%d):\n", len(r.BlockedTasks))
		for _, t := range r.BlockedTasks {
			blockers := strings.Join(t.BlockedBy, ", ")
			if blockers == "" {
				blockers = strings.Join(t.DependsOn, ", ")
			}
			fmt.Printf("  • %-6s [%-6s] %s (blocked by: %s)\n", t.ID, t.Priority, t.Title, blockers)
		}
		fmt.Println()
	}
}
