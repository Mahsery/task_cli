package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	claimForce bool
	claimActor string
)

var claimCmd = &cobra.Command{
	Use:   "claim <id> [flags]",
	Short: "Claim a task for atomic, race-free assignment",
	Long: `Atomically claims a task for the current actor.
If the task is already claimed by another actor, the claim will fail unless --force is specified.
Automatically transitions the task status from 'open' to 'in_progress', clears any 'orphaned' tag, and appends an attribution note.

To take over a task from another actor with an audit reason, use 'tk takeover'.
To hand off or transfer a task to another collaborator, use 'tk transfer'.`,
	Example: `  tk claim T-1
  tk claim T-1 --actor agent-worker-1
  tk claim T-1 --force
  tk takeover T-1 -m "Taking over implementation"`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		taskID := args[0]

		actor := resolveActor(claimActor)

		updated, err := taskClient.ClaimTask(taskID, actor, claimForce)
		if err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		fmt.Printf("✓ Claimed %s as %q (status: %s)\n", updated.ID, updated.Assignee, updated.Status)
		return nil
	},
}

func init() {
	claimCmd.Flags().BoolVar(&claimForce, "force", false, "Force claim even if already assigned to another actor")
	claimCmd.Flags().StringVar(&claimActor, "actor", "", "Explicit actor identity (overrides global actor)")
}
