package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	transferMessage string
	transferActor   string
	transferForce   bool
	transferTo      string
)

var transferCmd = &cobra.Command{
	Use:   "transfer <task-id> [new-assignee] [flags]",
	Short: "Transfer task assignment to another actor",
	Long: `Transfers ownership of a task from one actor to another.
Updates the task assignee, removes any 'orphaned' tag, transitions open tasks
to 'in_progress', and appends an audit note documenting the transfer.`,
	Example: `  # Transfer using positional argument
  tk transfer T-1 agent-gamma -m "Handing off database work"

  # Transfer using --to flag
  tk transfer T-1 --to bob -m "Need help with frontend UI"`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(_ *cobra.Command, args []string) error {
		taskID := normID(args[0])
		newAssignee := transferTo
		if len(args) == 2 {
			newAssignee = args[1]
		}
		if newAssignee == "" {
			return fmt.Errorf("new assignee must be specified (e.g. 'tk transfer %s <new-assignee>' or '--to <new-assignee>')", taskID)
		}

		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer func() { _ = taskClient.Close() }()

		actor := resolveActor(transferActor)

		// Inspect current task to report previous assignee
		existing, err := taskClient.GetTask(taskID)
		if err != nil {
			return err
		}
		prevAssignee := existing.Assignee

		updated, err := taskClient.TransferTask(taskID, actor, newAssignee, transferMessage, transferForce)
		if err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		if prevAssignee != "" {
			fmt.Printf("✓ Transferred %s from %q to %q (status: %s)\n",
				updated.ID, prevAssignee, updated.Assignee, updated.Status)
		} else {
			fmt.Printf("✓ Assigned %s to %q (status: %s)\n",
				updated.ID, updated.Assignee, updated.Status)
		}
		return nil
	},
}

func init() {
	transferCmd.Flags().StringVar(&transferTo, "to", "", "Target assignee to transfer task to")
	transferCmd.Flags().StringVarP(&transferMessage, "message", "m", "", "Audit note explaining the transfer")
	transferCmd.Flags().StringVar(&transferActor, "actor", "", "Explicit actor identity initiating the transfer")
	transferCmd.Flags().BoolVar(&transferForce, "force", false, "Force transfer")

	RootCmd.AddCommand(transferCmd)
}
