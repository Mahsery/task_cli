package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/git"

	"github.com/spf13/cobra"
)

var (
	attachDiff bool
	attachScan bool
)

var attachCmd = &cobra.Command{
	Use:   "attach [id] [commit-ref-or-range]",
	Short: "Attach a git commit, commit range, or working tree diff to a task",
	Long: `Attach git commit metadata and patch diffs to an existing task without
changing its completion status. If commit-ref is omitted, HEAD is attached.
Supports revision ranges (e.g. main..HEAD) and --scan to auto-discover commits
mentioning [T-n] in git log. When [id] is omitted, infers the active task from the current git branch.`,
	Example: `  tk attach T-1               # Attach latest commit (HEAD)
  tk attach                   # Infer task from current git branch and attach HEAD
  tk attach T-1 HEAD~1        # Attach specific ancestor commit
  tk attach T-1 main..HEAD    # Attach all commits in range main..HEAD
  tk attach T-1 --scan        # Scan git log for commits mentioning T-1
  tk attach T-1 --diff        # Attach uncommitted working tree diff`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		if repoRoot == "" {
			return fmt.Errorf("must be inside a git repository to attach git diffs or commits")
		}

		var taskID, refArg string
		switch len(args) {
		case 0:
			inferred, err := resolveOrInferTaskID(nil)
			if err != nil {
				return err
			}
			taskID = inferred
		case 1:
			if !strings.Contains(args[0], "..") {
				if norm, err := domain.NormalizeID(args[0], scope); err == nil {
					taskID = norm
				}
			}
			if taskID == "" {
				inferred, err := resolveOrInferTaskID(nil)
				if err != nil {
					taskID = normID(args[0])
				} else {
					taskID = inferred
					refArg = args[0]
				}
			}
		default:
			taskID = normID(args[0])
			refArg = args[1]
		}

		var infos []*git.CommitInfo
		var err error

		switch {
		case attachDiff:
			ci, dErr := git.GetWorkingDiff(repoRoot)
			if dErr != nil {
				return fmt.Errorf("failed to get git info: %w", dErr)
			}
			infos = []*git.CommitInfo{ci}
		case attachScan:
			infos, err = git.ScanCommitsForTask(repoRoot, taskID, refArg)
			if err != nil {
				return fmt.Errorf("failed to scan git log: %w", err)
			}
			if len(infos) == 0 {
				return fmt.Errorf("no commits found mentioning %s", taskID)
			}
		case refArg != "" && strings.Contains(refArg, ".."):
			infos, err = git.GetCommitRange(repoRoot, refArg)
			if err != nil {
				return fmt.Errorf("failed to get commit range: %w", err)
			}
		case refArg != "":
			ci, cErr := git.GetCommit(repoRoot, refArg)
			if cErr != nil {
				return fmt.Errorf("failed to get git info: %w", cErr)
			}
			infos = []*git.CommitInfo{ci}
		default:
			ci, cErr := git.GetLatestCommit(repoRoot)
			if cErr != nil {
				return fmt.Errorf("failed to get git info: %w", cErr)
			}
			infos = []*git.CommitInfo{ci}
		}

		commits := commitInfosToDomain(infos)
		updated, err := taskClient.AttachGitCommits(taskID, commits, false, false)
		if err != nil {
			return fmt.Errorf("failed to attach git info: %w", err)
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		if len(commits) > 1 {
			fmt.Printf("✓ Attached %d commits to %s (latest: %s)\n", len(commits), updated.ID, updated.CommitSHA)
		} else {
			fmt.Printf("✓ Attached %s to %s\n", commits[0].SHA, updated.ID)
			if updated.DiffStat != "" {
				fmt.Println(updated.DiffStat)
			}
		}
		return nil
	},
}

func init() {
	attachCmd.Flags().BoolVar(&attachDiff, "diff", false, "Attach current uncommitted working tree diff")
	attachCmd.Flags().BoolVar(&attachScan, "scan", false, "Scan git log for commits mentioning the task ID")
}
