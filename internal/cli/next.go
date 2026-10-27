package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var nextQuiet bool

var nextCmd = &cobra.Command{
	Use:   "next [flags]",
	Short: "Find the next actionable task (steers agents and humans to unblocked work)",
	Long: `Inspects the backlog and identifies the highest priority, unblocked task ready for work.
Automatically filters out tasks blocked by unfinished prerequisites.
Ideal for AI agents, CI runners, and developers asking 'what should I work on next?'.`,
	Example: `  tk next               # Get next actionable task
  tk next -q            # Print only the next task ID (or empty if none)
  tk next --json        # Machine-readable output for automated agent claiming`,
	RunE: func(_ *cobra.Command, _ []string) error {
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

			if nextQuiet {
				fmt.Println(target.ID)
				return nil
			}

			if jsonFlag {
				return outputJSON(target)
			}

			sep := "─"
			if IsASCIIOnly() {
				sep = "-"
			}

			var sb strings.Builder
			fmt.Fprintln(&sb, strings.Repeat(sep, 80))
			fmt.Fprintf(&sb, "NEXT ACTIONABLE TASK: %s\n", target.ID)
			fmt.Fprintln(&sb, strings.Repeat(sep, 80))
			fmt.Fprintf(&sb, "Title:    %s\n", target.Title)
			fmt.Fprintf(&sb, "Status:   %-12s Priority: %-12s\n", strings.ToUpper(string(target.Status)), strings.ToUpper(string(target.Priority)))
			if target.TopicPath != "" {
				fmt.Fprintf(&sb, "Topic:    @%s\n", target.TopicPath)
			} else if len(target.Tags) > 0 {
				fmt.Fprintf(&sb, "Tags:     [%s]\n", strings.Join(target.Tags, ", "))
			}
			if target.Assignee != "" {
				fmt.Fprintf(&sb, "Assignee: %s\n", target.Assignee)
			}
			if len(target.DependsOn) > 0 {
				fmt.Fprintf(&sb, "Prereqs:  [%s] (All prerequisites completed ✓)\n", strings.Join(target.DependsOn, ", "))
			}
			if len(target.Blocks) > 0 {
				fmt.Fprintf(&sb, "Unblocks: [%s] when completed\n", strings.Join(target.Blocks, ", "))
			}
			if target.Description != "" {
				fmt.Fprintf(&sb, "\nDescription:\n%s\n", target.Description)
			}
			fmt.Fprintln(&sb, strings.Repeat(sep, 80))
			fmt.Fprintln(&sb, "Recommended commands:")
			if target.Status == domain.StatusOpen {
				fmt.Fprintf(&sb, "  tk claim %s             # Claim this task\n", target.ID)
			}
			fmt.Fprintf(&sb, "  tk show %s              # Inspect details and notes\n", target.ID)
			fmt.Fprintf(&sb, "  tk done %s              # Mark completed when finished\n", target.ID)
			return PrintWithPager(sb.String())
		}

		if nextQuiet {
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
			var sb strings.Builder
			fmt.Fprintf(&sb, "No ready tasks available right now.\n\n")
			fmt.Fprintf(&sb, "Found %d open task(s) currently BLOCKED by prerequisites:\n", len(activeBlocked))
			for _, t := range activeBlocked {
				fmt.Fprintf(&sb, "  🔒 %s: %s (waiting on: %s)\n", t.ID, t.Title, strings.Join(t.BlockedBy, ", "))
			}
			fmt.Fprintf(&sb, "\nComplete the unfinished prerequisites above or use 'tk dep rm' to unblock.\n")
			return PrintWithPager(sb.String())
		}

		fmt.Println("No pending tasks! The backlog is clear.")
		return nil
	},
}

func init() {
	nextCmd.Flags().BoolVarP(&nextQuiet, "quiet", "q", false, "Print only the next actionable task ID")
	RootCmd.AddCommand(nextCmd)
}
