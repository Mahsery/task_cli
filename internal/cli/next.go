package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var nextCmd = &cobra.Command{
	Use:   "next [flags]",
	Short: "Find the next actionable task (steers agents and humans to unblocked work)",
	Long: `Inspects the backlog and identifies the highest priority, unblocked task ready for work.
Automatically filters out tasks blocked by unfinished prerequisites.
Ideal for AI agents, CI runners, and developers asking 'what should I work on next?'.`,
	Example: `  tk next               # Get next actionable task
  tk next --json        # Machine-readable output for automated agent claiming`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. First look for in_progress or open tasks that are NOT blocked
		readyFilter := domain.TaskFilter{
			Scope:     scope,
			RepoPath:  repoRoot,
			OnlyReady: true,
			Sort:      "status,priority,id",
		}

		readyTasks, err := taskClient.ListTasks(readyFilter)
		if err != nil {
			return err
		}

		// Filter to active tasks (open or in_progress)
		var actionable []*domain.Task
		for _, t := range readyTasks {
			if t.Status == domain.StatusInProgress || t.Status == domain.StatusOpen {
				actionable = append(actionable, t)
			}
		}

		if len(actionable) > 0 {
			target := actionable[0]

			// If any in_progress task is assigned to current actor, prioritize finishing it
			currentActor := cfg.Actor
			for _, t := range actionable {
				if t.Status == domain.StatusInProgress && t.Assignee != "" && t.Assignee == currentActor {
					target = t
					break
				}
			}

			if jsonFlag {
				return outputJSON(target)
			}

			sep := "─"
			if IsASCIIOnly() {
				sep = "-"
			}

			fmt.Println(strings.Repeat(sep, 80))
			fmt.Printf("NEXT ACTIONABLE TASK: %s\n", target.ID)
			fmt.Println(strings.Repeat(sep, 80))
			fmt.Printf("Title:    %s\n", target.Title)
			fmt.Printf("Status:   %-12s Priority: %-12s\n", strings.ToUpper(string(target.Status)), strings.ToUpper(string(target.Priority)))
			if len(target.Tags) > 0 {
				fmt.Printf("Tags:     [%s]\n", strings.Join(target.Tags, ", "))
			}
			if target.Assignee != "" {
				fmt.Printf("Assignee: %s\n", target.Assignee)
			}
			if len(target.DependsOn) > 0 {
				fmt.Printf("Prereqs:  [%s] (All prerequisites completed ✓)\n", strings.Join(target.DependsOn, ", "))
			}
			if len(target.Blocks) > 0 {
				fmt.Printf("Unblocks: [%s] when completed\n", strings.Join(target.Blocks, ", "))
			}
			if target.Description != "" {
				fmt.Printf("\nDescription:\n%s\n", target.Description)
			}
			fmt.Println(strings.Repeat(sep, 80))
			fmt.Println("Recommended commands:")
			if target.Status == domain.StatusOpen {
				fmt.Printf("  tk claim %s             # Claim this task\n", target.ID)
			}
			fmt.Printf("  tk show %s              # Inspect details and notes\n", target.ID)
			fmt.Printf("  tk done %s              # Mark completed when finished\n", target.ID)
			return nil
		}

		// 2. No ready active tasks found. Check if there are blocked tasks!
		blockedFilter := domain.TaskFilter{
			Scope:       scope,
			RepoPath:    repoRoot,
			OnlyBlocked: true,
		}
		blockedTasks, err := taskClient.ListTasks(blockedFilter)
		if err != nil {
			return err
		}

		var activeBlocked []*domain.Task
		for _, t := range blockedTasks {
			if t.Status == domain.StatusInProgress || t.Status == domain.StatusOpen {
				activeBlocked = append(activeBlocked, t)
			}
		}

		if jsonFlag {
			type NextStatus struct {
				Ready   *domain.Task   `json:"ready"`
				Blocked []*domain.Task `json:"blocked,omitempty"`
				Message string         `json:"message"`
			}
			res := NextStatus{
				Ready:   nil,
				Blocked: activeBlocked,
				Message: "No unblocked tasks ready for work",
			}
			return outputJSON(res)
		}

		if len(activeBlocked) > 0 {
			fmt.Printf("No ready tasks available right now.\n\n")
			fmt.Printf("Found %d open task(s) currently BLOCKED by prerequisites:\n", len(activeBlocked))
			for _, t := range activeBlocked {
				fmt.Printf("  🔒 %s: %s (waiting on: %s)\n", t.ID, t.Title, strings.Join(t.BlockedBy, ", "))
			}
			fmt.Printf("\nComplete the unfinished prerequisites above or use 'tk dep rm' to unblock.\n")
			return nil
		}

		fmt.Println("No pending tasks! The backlog is clear.")
		return nil
	},
}

func init() {
	RootCmd.AddCommand(nextCmd)
}
