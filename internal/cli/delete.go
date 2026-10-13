package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:     "delete <id>",
	Aliases: []string{"rm"},
	Short:   "Delete a task",
	Long: `Permanently delete a task and all associated activity notes and diffs.
Prompts for confirmation unless --yes (-y) or --force (-f) is specified.`,
	Example: `  tk delete T-1
  tk rm G-4
  tk delete T-1 -y`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		taskID := normID(args[0])

		if !deleteForce && !jsonFlag {
			task, _ := taskClient.GetTask(taskID)
			var titlePrompt string
			if task != nil && task.Title != "" {
				titlePrompt = fmt.Sprintf(" %q", task.Title)
			}
			fmt.Printf("Delete task %s%s permanently? [y/N]: ", taskID, titlePrompt)
			var resp string
			_, _ = fmt.Scanln(&resp)
			resp = strings.ToLower(strings.TrimSpace(resp))
			if resp != "y" && resp != "yes" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		if err := taskClient.DeleteTask(taskID); err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(map[string]any{"id": taskID, "deleted": true})
		}

		fmt.Printf("✓ Deleted %s\n", taskID)
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&deleteForce, "yes", "y", false, "Confirm deletion without interactive prompt")
	deleteCmd.Flags().BoolVarP(&deleteForce, "force", "f", false, "Confirm deletion without interactive prompt")
}
