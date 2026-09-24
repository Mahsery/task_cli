package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	restoreListFlag bool
)

var restoreCmd = &cobra.Command{
	Use:     "restore [id]",
	Aliases: []string{"undo-delete"},
	Short:   "Restore a deleted task from tombstones",
	Long: `Restore a deleted task from the tombstone archive, preserving its ID, tags, activity notes, and git patch diffs.
Use --list or -l to view recoverable deleted tasks.`,
	Example: `  tk restore T-1           # Restore deleted task T-1
  tk restore -l            # List recoverable deleted tasks
  tk restore --list        # List recoverable deleted tasks
  tk restore -l --json     # Output recoverable tasks in JSON`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if restoreListFlag || len(args) == 0 {
			return runRestoreList()
		}

		taskID := strings.ToUpper(strings.TrimSpace(args[0]))
		task, err := taskClient.RestoreTaskByID(taskID)
		if err != nil {
			return fmt.Errorf("failed to restore %s: %w", taskID, err)
		}

		if jsonFlag {
			return outputJSON(task)
		}

		fmt.Printf("✓ Restored task %s: %q\n", task.ID, task.Title)
		return nil
	},
}

func runRestoreList() error {
	tombstones, err := taskClient.ListTombstones()
	if err != nil {
		return fmt.Errorf("failed to list recoverable tasks: %w", err)
	}

	if jsonFlag {
		return outputJSON(tombstones)
	}

	if len(tombstones) == 0 {
		fmt.Printf("No recoverable deleted tasks found in %s scope.\n", taskClient.Scope)
		return nil
	}

	fmt.Printf("%-8s %-12s %-10s %-40s %s\n", "ID", "STATUS", "PRIORITY", "TITLE", "TAGS")
	fmt.Println(strings.Repeat("─", 88))
	for _, t := range tombstones {
		tagsStr := ""
		if len(t.Tags) > 0 {
			tagsStr = fmt.Sprintf("[%s]", strings.Join(t.Tags, ", "))
		}
		title := t.Title
		if len(title) > 38 {
			title = title[:35] + "..."
		}
		fmt.Printf("%-8s %-12s %-10s %-40s %s\n", t.ID, t.Status, t.Priority, title, tagsStr)
	}
	return nil
}

func init() {
	restoreCmd.Flags().BoolVarP(&restoreListFlag, "list", "l", false, "List recoverable deleted tasks in tombstone archive")
	RootCmd.AddCommand(restoreCmd)
}
