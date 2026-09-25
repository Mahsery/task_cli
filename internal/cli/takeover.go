package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	takeoverMessage string
	takeoverActor   string
	takeoverForce   bool
)

var takeoverCmd = &cobra.Command{
	Use:     "takeover <task-id> [flags]",
	Aliases: []string{"take"},
	Short:   "Take over an assigned or orphaned task",
	Long: `Explicitly takes over a task from another actor or claims an orphaned task.
Updates the task assignee to the current actor, transitions open tasks to 'in_progress',
removes any 'orphaned' tag, and appends an audit note documenting the handover.`,
	Example: `  # Take over a task with a reason
  tk takeover T-1 -m "I am taking over this thing now"
  tk take T-1 -m "Assignee is away, taking over implementation"

  # Explicit actor override
  tk takeover T-1 --actor agent-antigravity -m "Steering subtask"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]

		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer taskClient.Close()

		actor := cfg.Actor
		if takeoverActor != "" {
			actor = takeoverActor
		}
		if actor == "" {
			actor = "anonymous"
		}

		// Inspect current task to report previous assignee
		existing, err := taskClient.GetTask(taskID)
		if err != nil {
			return err
		}
		prevAssignee := existing.Assignee

		updated, err := taskClient.TransferTask(taskID, actor, actor, takeoverMessage, takeoverForce)
		if err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		if prevAssignee != "" && prevAssignee != actor {
			fmt.Printf("✓ Took over %s as %q (transferred from %q, status: %s)\n",
				updated.ID, updated.Assignee, prevAssignee, updated.Status)
		} else {
			fmt.Printf("✓ Took over %s as %q (status: %s)\n",
				updated.ID, updated.Assignee, updated.Status)
		}
		return nil
	},
}

func init() {
	takeoverCmd.Flags().StringVarP(&takeoverMessage, "message", "m", "", "Audit note explaining the takeover (e.g. 'I am taking over this now')")
	takeoverCmd.Flags().StringVar(&takeoverActor, "actor", "", "Explicit actor identity taking over the task")
	takeoverCmd.Flags().BoolVar(&takeoverForce, "force", false, "Force takeover even if protected")

	RootCmd.AddCommand(takeoverCmd)
}
