package cli

import (
	"fmt"

	"task_cli/internal/git"

	"github.com/spf13/cobra"
)

var attachDiff bool

var attachCmd = &cobra.Command{
	Use:   "attach <id> [commit-ref]",
	Short: "Attach a git commit or working tree diff to a task",
	Long: `Attach git commit metadata and patch diffs to an existing task without
changing its completion status. If commit-ref is omitted, HEAD is attached.
Must be executed within a git repository.`,
	Example: `  tk attach T-1               # Attach latest commit (HEAD)
  tk attach T-1 HEAD~1        # Attach specific ancestor commit
  tk attach T-1 --diff        # Attach uncommitted working tree diff`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		if repoRoot == "" {
			return fmt.Errorf("must be inside a git repository to attach git diffs or commits")
		}

		var commitInfo *git.CommitInfo
		var err error

		if attachDiff {
			commitInfo, err = git.GetWorkingDiff(repoRoot)
		} else if len(args) > 1 {
			commitInfo, err = git.GetCommit(repoRoot, args[1])
		} else {
			commitInfo, err = git.GetLatestCommit(repoRoot)
		}

		if err != nil {
			return fmt.Errorf("failed to get git info: %w", err)
		}

		updated, err := taskClient.AttachGit(taskID, commitInfo.ShortSHA, commitInfo.Subject, commitInfo.DiffStat, commitInfo.DiffContent, false)
		if err != nil {
			return fmt.Errorf("failed to attach git info: %w", err)
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		fmt.Printf("✓ Attached %s to %s\n", commitInfo.ShortSHA, updated.ID)
		if updated.DiffStat != "" {
			fmt.Println(updated.DiffStat)
		}
		return nil
	},
}

func init() {
	attachCmd.Flags().BoolVar(&attachDiff, "diff", false, "Attach current uncommitted working tree diff")
}
