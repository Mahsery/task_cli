package cli

import (
	"fmt"

	"task_cli/internal/domain"
	"task_cli/internal/git"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	doneCommit   string
	doneDiff     bool
	doneNoCommit bool
	doneForce    bool
)

var doneCmd = &cobra.Command{
	Use:     "done <id> [flags]",
	Aliases: []string{"close", "resolve"},
	Short:   "Mark task as done (automatically attaches git commit/diff in kernel style)",
	Long: `Mark a task as resolved. When executed within a git repository, tk
automatically inspects HEAD and attaches the latest commit hash, subject line,
diffstat, and patch diff in kernel development style.
Use --diff to snapshot uncommitted working tree changes, or --no-commit to skip git binding.`,
	Example: `  tk done T-1                  # Snapshots latest HEAD commit & diff
  tk done T-1 --diff           # Snapshots uncommitted working tree diff
  tk done T-1 -c abc1234       # Attach specific commit ref
  tk done T-1 --no-commit      # Mark done without attaching git info`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]

		var sha, msg, stat, diff string
		autoHead := false
		if !doneNoCommit && repoRoot != "" {
			var commitInfo *git.CommitInfo
			var err error

			if doneDiff {
				commitInfo, err = git.GetWorkingDiff(repoRoot)
				if err != nil {
					return fmt.Errorf("failed to get working tree diff: %w", err)
				}
			} else if doneCommit != "" {
				commitInfo, err = git.GetCommit(repoRoot, doneCommit)
				if err != nil {
					return fmt.Errorf("failed to lookup commit %q: %w", doneCommit, err)
				}
			} else {
				commitInfo, err = git.GetLatestCommit(repoRoot)
				if err == nil && commitInfo != nil {
					autoHead = true
				}
			}

			if commitInfo != nil {
				sha = commitInfo.ShortSHA
				msg = commitInfo.Subject
				stat = commitInfo.DiffStat
				diff = commitInfo.DiffContent
			}
		} else if doneCommit != "" && repoRoot == "" {
			return fmt.Errorf("cannot attach commit %q: not in a git repository", doneCommit)
		}

		var updated *domain.Task
		var err error

		author := cfg.Actor
		if author == "" {
			author = git.GetAuthor()
		}

		if sha != "" {
			updated, err = taskClient.AttachGitWithForce(taskID, sha, msg, stat, diff, true, doneForce)
		} else {
			status := domain.StatusDone
			updated, err = taskClient.UpdateTask(protocol.UpdateTaskPayload{
				ID:     taskID,
				Status: &status,
				Author: author,
				Force:  doneForce,
			})
		}

		if err != nil {
			return fmt.Errorf("failed to complete task: %w", err)
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		fmt.Printf("✓ Marked %s as DONE\n", updated.ID)
		if updated.CommitSHA != "" {
			if autoHead {
				fmt.Printf("+ Auto-attached HEAD commit %s (%q) [use -c <ref> to bind a specific commit]\n", updated.CommitSHA, updated.CommitMsg)
			} else {
				fmt.Printf("+ Attached commit %s (%q)\n", updated.CommitSHA, updated.CommitMsg)
			}
			if updated.DiffStat != "" {
				fmt.Println(updated.DiffStat)
			}
		}

		return nil
	},
}

func init() {
	doneCmd.Flags().StringVarP(&doneCommit, "commit", "c", "", "Specific git commit hash or ref to attach")
	doneCmd.Flags().BoolVar(&doneDiff, "diff", false, "Attach current uncommitted working tree diff instead of commit")
	doneCmd.Flags().BoolVar(&doneNoCommit, "no-commit", false, "Do not auto-attach git commit")
	doneCmd.Flags().BoolVarP(&doneForce, "force", "f", false, "Force completion even if prerequisite dependencies are unfinished")
}
