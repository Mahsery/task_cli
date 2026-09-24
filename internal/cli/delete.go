package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete <id>",
	Aliases: []string{"rm"},
	Short:   "Delete a task",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		if err := taskClient.DeleteTask(taskID); err != nil {
			return err
		}

		if jsonFlag {
			fmt.Printf("{\"id\":%q,\"deleted\":true}\n", taskID)
			return nil
		}

		fmt.Printf("✓ Deleted %s\n", taskID)
		return nil
	},
}
