package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/git"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	startBranch     string
	startNoCheckout bool
	startForce      bool
	startActor      string

	branchSetName  string
	branchCheckout bool
)

// resolveOrInferTaskID resolves an explicit task ID argument if provided, or infers the active
// task ID from the current git branch (either via DB branch binding or branch naming convention).
func resolveOrInferTaskID(args []string) (string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return normID(args[0]), nil
	}
	if repoRoot == "" {
		return "", fmt.Errorf("task ID required (not inside a git repository)")
	}
	branch, err := git.CurrentBranch(repoRoot)
	if err != nil || branch == "" {
		return "", fmt.Errorf("task ID required (HEAD is detached or has no branch)")
	}
	return resolveTaskIDForBranch(branch)
}

func resolveTaskIDForBranch(branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("branch name is empty")
	}
	if taskClient != nil {
		tasks, err := taskClient.ListTasks(domain.TaskFilter{
			Scope:           scope,
			RepoPath:        repoRoot,
			Branch:          branch,
			IncludeArchived: true,
			OmitNotes:       true,
		})
		if err == nil && len(tasks) > 0 {
			for _, t := range tasks {
				if t.Status == domain.StatusInProgress || t.Status == domain.StatusOpen {
					return t.ID, nil
				}
			}
			return tasks[0].ID, nil
		}
	}
	if inferred := git.InferTaskIDFromBranchName(branch); inferred != "" {
		return normID(inferred), nil
	}
	return "", fmt.Errorf("no task bound to branch %q (pass <id> explicitly or bind with 'tk start <id>')", branch)
}

var startCmd = &cobra.Command{
	Use:   "start <id> [flags]",
	Short: "Claim a task and create/checkout its bound git branch",
	Long: `Atomically claims a task for the current actor, binds it to a git branch
(default: tk/t-<num>-<slug>), and checks out that branch in the current worktree.
Once on a task-bound branch, commands like 'tk show', 'tk reply', 'tk edit',
'tk attach', and 'tk done' automatically infer the active task ID when omitted.`,
	Example: `  tk start T-12
  tk start T-12 -b feat/custom-branch
  tk start T-12 --no-checkout`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		taskID := normID(args[0])
		task, err := taskClient.GetTask(taskID)
		if err != nil {
			return err
		}
		if task.IsPost() {
			return fmt.Errorf("%s is a post; promote it to a task first with 'tk promote %s'", task.ID, task.ID)
		}

		branchName := strings.TrimSpace(startBranch)
		if branchName == "" {
			if task.Branch != "" {
				branchName = task.Branch
			} else {
				branchName = git.BranchSlugForTask(task.ID, task.Title)
			}
		}

		if !startNoCheckout && repoRoot != "" {
			if err := git.CheckoutOrCreateBranch(repoRoot, branchName); err != nil {
				return err
			}
		}

		actor := resolveActor(startActor)
		updated, err := taskClient.ClaimTaskWithBranch(task.ID, actor, branchName, startForce)
		if err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		if !startNoCheckout && repoRoot != "" {
			fmt.Printf("✓ Started %s as %q on branch %s (status: %s)\n", updated.ID, updated.Assignee, updated.Branch, updated.Status)
		} else {
			fmt.Printf("✓ Claimed %s as %q bound to branch %s (status: %s)\n", updated.ID, updated.Assignee, updated.Branch, updated.Status)
		}
		return nil
	},
}

var branchCmd = &cobra.Command{
	Use:   "branch [id] [flags]",
	Short: "Inspect, bind, or checkout the git branch associated with a task",
	Long: `Without arguments, prints the task bound to the currently checked-out git branch.
With a task ID, prints (or with -c/--checkout switches to) that task's bound git branch,
or binds a custom branch name with -b/--set.`,
	Example: `  tk branch                  # Show the task bound to the current git branch
  tk branch T-12             # Show the branch name for T-12
  tk branch T-12 -c          # Checkout (or create) T-12's branch
  tk branch T-12 -b feat/x   # Bind T-12 to branch feat/x`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			taskID, err := resolveOrInferTaskID(nil)
			if err != nil {
				return err
			}
			task, err := taskClient.GetTask(taskID)
			if err != nil {
				return err
			}
			if jsonFlag {
				return outputJSON(task)
			}
			branch := task.Branch
			if branch == "" && repoRoot != "" {
				branch, _ = git.CurrentBranch(repoRoot)
			}
			fmt.Printf("%s: %s (status: %s, branch: %s)\n", task.ID, task.Title, task.StatusLabel(), branch)
			return nil
		}

		taskID := normID(args[0])
		task, err := taskClient.GetTask(taskID)
		if err != nil {
			return err
		}

		branchName := task.Branch
		if cmd.Flags().Changed("set") {
			branchName = strings.TrimSpace(branchSetName)
			updated, err := taskClient.UpdateTask(protocol.UpdateTaskPayload{
				ID:     task.ID,
				Branch: &branchName,
				Author: resolveAuthor(""),
			})
			if err != nil {
				return err
			}
			task = updated
		} else if branchName == "" {
			branchName = git.BranchSlugForTask(task.ID, task.Title)
			if branchCheckout {
				updated, err := taskClient.UpdateTask(protocol.UpdateTaskPayload{
					ID:     task.ID,
					Branch: &branchName,
					Author: resolveAuthor(""),
				})
				if err != nil {
					return err
				}
				task = updated
			}
		}

		if branchCheckout && branchName != "" {
			if repoRoot == "" {
				return fmt.Errorf("cannot checkout branch %q: not inside a git repository", branchName)
			}
			if err := git.CheckoutOrCreateBranch(repoRoot, branchName); err != nil {
				return err
			}
		}

		if jsonFlag {
			return outputJSON(task)
		}
		if branchCheckout {
			fmt.Printf("✓ Checked out %s for %s (%s)\n", branchName, task.ID, task.Title)
		} else {
			fmt.Println(branchName)
		}
		return nil
	},
}

func init() {
	startCmd.Flags().StringVarP(&startBranch, "branch", "b", "", "Custom git branch name (default: tk/t-<id>-<slug>)")
	startCmd.Flags().BoolVar(&startNoCheckout, "no-checkout", false, "Bind branch without running git checkout")
	startCmd.Flags().BoolVarP(&startForce, "force", "f", false, "Force claim even if already assigned to another actor")
	startCmd.Flags().StringVar(&startActor, "actor", "", "Explicit actor identity (overrides global actor)")

	branchCmd.Flags().StringVarP(&branchSetName, "set", "b", "", "Bind task to the specified branch name")
	branchCmd.Flags().BoolVarP(&branchCheckout, "checkout", "c", false, "Checkout (or create) the task's branch in git")

	RootCmd.AddCommand(startCmd, branchCmd)
}
