package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var depCmd = &cobra.Command{
	Use:     "dep <command>",
	Aliases: []string{"deps", "dependency", "dependencies"},
	Short:   "Manage task dependencies and prerequisite links (DAG)",
	Long: `Manage prerequisite dependencies and blocker relationships between tasks.
Tasks with unfinished dependencies are marked BLOCKED and cannot be marked done
or closed until all prerequisites are completed (unless overridden with --force).`,
	Example: `  tk dep add T-2 T-1           # T-2 depends on T-1 (T-1 blocks T-2)
  tk dep add T-3 T-1 T-2       # T-3 depends on both T-1 and T-2
  tk dep rm T-2 T-1            # Remove dependency link
  tk dep show T-2              # Show prerequisites and dependents
  tk dep tree T-1              # Show dependency tree rooted at T-1
  tk link T-2 T-1              # Shorthand alias for 'tk dep add'`,
}

var depAddCmd = &cobra.Command{
	Use:     "add <task-id> <prereq-id> [prereq-id...]",
	Aliases: []string{"link"},
	Short:   "Add prerequisite dependency (task depends on prereq)",
	Long: `Add one or more prerequisite dependencies to a task. The target task will depend
on each specified prerequisite task. Cycles in dependencies are strictly rejected.`,
	Example: `  tk dep add T-2 T-1
  tk dep add T-3 T-1 T-2`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		prereqs := args[1:]

		for _, prereqID := range prereqs {
			if err := taskClient.AddDependency(taskID, prereqID); err != nil {
				return fmt.Errorf("failed to link %s -> %s: %w", taskID, prereqID, err)
			}
			fmt.Printf("✓ Added dependency: %s depends on %s (blocks %s until %s is done)\n", taskID, prereqID, taskID, prereqID)
		}
		return nil
	},
}

var depRmCmd = &cobra.Command{
	Use:     "rm <task-id> <prereq-id> [prereq-id...]",
	Aliases: []string{"remove", "delete", "unlink"},
	Short:   "Remove prerequisite dependency",
	Long:    `Remove one or more prerequisite dependency links between tasks.`,
	Example: `  tk dep rm T-2 T-1`,
	Args:    cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		prereqs := args[1:]

		for _, prereqID := range prereqs {
			if err := taskClient.RemoveDependency(taskID, prereqID); err != nil {
				return fmt.Errorf("failed to unlink %s from %s: %w", taskID, prereqID, err)
			}
			fmt.Printf("✓ Removed dependency: %s no longer depends on %s\n", taskID, prereqID)
		}
		return nil
	},
}

var depShowCmd = &cobra.Command{
	Use:     "show <task-id>",
	Aliases: []string{"list", "get"},
	Short:   "Show dependencies and blockers for a task",
	Long:    `Display all prerequisite tasks that this task depends on, and all dependent tasks that this task blocks.`,
	Example: `  tk dep show T-2`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		task, err := taskClient.GetTask(taskID)
		if err != nil {
			return err
		}

		if jsonFlag {
			data, err := taskClient.GetDependencies(taskID)
			if err != nil {
				return err
			}
			return outputJSON(data)
		}

		fmt.Printf("Dependency Graph for %s: %q\n", task.ID, task.Title)
		statusBadge := fmt.Sprintf("[%s]", strings.ToUpper(string(task.Status)))
		if task.IsBlocked {
			statusBadge += " 🔒 BLOCKED"
		} else if task.Status != domain.StatusDone && task.Status != domain.StatusClosed {
			statusBadge += " ⚡ READY"
		}
		fmt.Printf("Current Status: %s\n\n", statusBadge)

		fmt.Println("Prerequisites (Depends On):")
		if len(task.DependsOn) == 0 {
			fmt.Println("  (none - independent root task)")
		} else {
			for _, depID := range task.DependsOn {
				depTask, err := taskClient.GetTask(depID)
				if err != nil {
					fmt.Printf("  • %s (unresolved)\n", depID)
				} else {
					marker := "✓"
					if depTask.Status != domain.StatusDone && depTask.Status != domain.StatusClosed {
						marker = "✗ [UNFINISHED]"
					}
					fmt.Printf("  • %s %s [%s] %s\n", marker, depTask.ID, depTask.Status, depTask.Title)
				}
			}
		}

		fmt.Println("\nDependents (Blocks):")
		if len(task.Blocks) == 0 {
			fmt.Println("  (none - does not block any tasks)")
		} else {
			for _, blkID := range task.Blocks {
				blkTask, err := taskClient.GetTask(blkID)
				if err != nil {
					fmt.Printf("  • %s (unresolved)\n", blkID)
				} else {
					fmt.Printf("  • %s [%s] %s\n", blkTask.ID, blkTask.Status, blkTask.Title)
				}
			}
		}

		return nil
	},
}

// Standalone top-level alias commands
var linkCmd = &cobra.Command{
	Use:     "link <task-id> <prereq-id> [prereq-id...]",
	Short:   "Link a task to depend on prerequisite tasks (alias for 'tk dep add')",
	Example: `  tk link T-2 T-1`,
	Args:    cobra.MinimumNArgs(2),
	RunE:    depAddCmd.RunE,
}

var unlinkCmd = &cobra.Command{
	Use:     "unlink <task-id> <prereq-id> [prereq-id...]",
	Short:   "Unlink a task from prerequisite tasks (alias for 'tk dep rm')",
	Example: `  tk unlink T-2 T-1`,
	Args:    cobra.MinimumNArgs(2),
	RunE:    depRmCmd.RunE,
}

func init() {
	depCmd.AddCommand(depAddCmd)
	depCmd.AddCommand(depRmCmd)
	depCmd.AddCommand(depShowCmd)

	RootCmd.AddCommand(depCmd)
	RootCmd.AddCommand(linkCmd)
	RootCmd.AddCommand(unlinkCmd)
}
