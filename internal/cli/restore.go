package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	restoreListFlag  bool
	restoreDBFlag    bool
	restoreForceFlag bool
)

var restoreCmd = &cobra.Command{
	Use:     "restore [id|latest]",
	Aliases: []string{"undo-delete"},
	Short:   "Restore a deleted task from tombstones, or database with --db",
	Long: `Restore a deleted task from the tombstone archive, preserving its ID, tags, activity notes, and git patch diffs.
With --db (or -d), restores the entire task database from the latest or specified backup snapshot.`,
	Example: `  tk restore T-1           # Restore deleted task T-1
  tk restore -l            # List recoverable deleted tasks
  tk restore --db          # Restore database from latest backup (central or local)
  tk restore --db latest   # Restore database from latest backup
  tk restore --db -f       # Restore database without interactive confirmation`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if restoreDBFlag {
			target := "latest"
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				target = args[0]
			}
			return RunBackupRestore(target, restoreForceFlag)
		}

		if restoreListFlag || len(args) == 0 {
			return runRestoreList()
		}

		taskID := normID(args[0])
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

	var sb strings.Builder
	fmt.Fprintf(&sb, "%-8s %-12s %-10s %-40s %s\n", "ID", "STATUS", "PRIORITY", "TITLE", "TOPIC")
	fmt.Fprintln(&sb, strings.Repeat("─", 88))
	for _, t := range tombstones {
		topicStr := "-"
		if t.TopicPath != "" {
			topicStr = "@" + t.TopicPath
		} else if len(t.Tags) > 0 {
			topicStr = fmt.Sprintf("[%s]", strings.Join(t.Tags, ", "))
		}
		title := t.Title
		if len(title) > 38 {
			title = title[:35] + "..."
		}
		fmt.Fprintf(&sb, "%-8s %-12s %-10s %-40s %s\n", t.ID, t.Status, t.Priority, title, topicStr)
	}
	return PrintWithPager(sb.String())
}

func init() {
	restoreCmd.Flags().BoolVarP(&restoreListFlag, "list", "l", false, "List recoverable deleted tasks in tombstone archive")
	restoreCmd.Flags().BoolVarP(&restoreDBFlag, "db", "d", false, "Restore task database from backup rather than individual task")
	restoreCmd.Flags().BoolVarP(&restoreForceFlag, "force", "f", false, "Force database restore without interactive confirmation")
	RootCmd.AddCommand(restoreCmd)
}
