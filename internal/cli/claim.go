package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/git"

	"github.com/spf13/cobra"
)

var (
	claimForce  bool
	claimActor  string
	claimBranch string
)

var claimCmd = &cobra.Command{
	Use:   "claim <id> [flags]",
	Short: "Claim a task for atomic, race-free assignment",
	Long: `Atomically claims a task for the current actor.
If the task is already claimed by another actor, the claim will fail unless --force is specified.
Automatically transitions the task status from 'open' to 'in_progress', clears any 'orphaned' tag, and appends an attribution note.
Use -b/--branch to also create, checkout, and bind a git branch to the task.

To take over a task from another actor with an audit reason, use 'tk takeover'.
To hand off or transfer a task to another collaborator, use 'tk transfer'.`,
	Example: `  tk claim T-1
  tk claim T-1 -b
  tk claim T-1 -b feat/custom-branch
  tk claim T-1 --actor agent-worker-1
  tk claim T-1 --force
  tk takeover T-1 -m "Taking over implementation"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := normID(args[0])
		actor := resolveActor(claimActor)

		var branchName string
		if cmd.Flags().Changed("branch") {
			branchName = strings.TrimSpace(claimBranch)
			if branchName == "" || branchName == "auto" {
				t, err := taskClient.GetTask(taskID)
				if err != nil {
					return err
				}
				if t.Branch != "" {
					branchName = t.Branch
				} else {
					branchName = git.BranchSlugForTask(t.ID, t.Title)
				}
			}
			if repoRoot != "" {
				if err := git.CheckoutOrCreateBranch(repoRoot, branchName); err != nil {
					return err
				}
			}
		}

		updated, err := taskClient.ClaimTaskWithBranch(taskID, actor, branchName, claimForce)
		if err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		if updated.Branch != "" && cmd.Flags().Changed("branch") {
			fmt.Printf("✓ Claimed %s as %q on branch %s (status: %s)\n", updated.ID, updated.Assignee, updated.Branch, updated.Status)
		} else {
			fmt.Printf("✓ Claimed %s as %q (status: %s)\n", updated.ID, updated.Assignee, updated.Status)
		}
		return nil
	},
}

func init() {
	claimCmd.Flags().BoolVar(&claimForce, "force", false, "Force claim even if already assigned to another actor")
	claimCmd.Flags().StringVar(&claimActor, "actor", "", "Explicit actor identity (overrides global actor)")
	claimCmd.Flags().StringVarP(&claimBranch, "branch", "b", "", "Create/checkout and bind a git branch (default: tk/t-<id>-<slug>)")
	if f := claimCmd.Flags().Lookup("branch"); f != nil {
		f.NoOptDefVal = "auto"
	}
}
