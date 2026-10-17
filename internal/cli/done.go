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
	doneCommit   string
	doneDiff     bool
	doneNoCommit bool
	doneForce    bool
	doneScan     bool
	doneQuiet    bool
)

func commitInfosToDomain(infos []*git.CommitInfo) []domain.TaskCommit {
	out := make([]domain.TaskCommit, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		sha := info.ShortSHA
		if sha == "" {
			sha = info.SHA
		}
		out = append(out, domain.TaskCommit{
			SHA:         sha,
			Subject:     info.Subject,
			Author:      info.Author,
			CommittedAt: info.Date,
			DiffStat:    info.DiffStat,
			DiffContent: info.DiffContent,
		})
	}
	return out
}

var doneCmd = &cobra.Command{
	Use:     "done [id] [flags]",
	Aliases: []string{"close", "resolve"},
	Short:   "Mark task as done (automatically attaches git commit/diff in kernel style)",
	Long: `Mark a task as resolved. When executed within a git repository, tk
automatically inspects HEAD and attaches the latest commit hash, subject line,
diffstat, and patch diff in kernel development style.
When [id] is omitted, infers the active task from the current git branch.
Use --diff to snapshot uncommitted working tree changes, -c main..HEAD for a commit range,
--scan to attach all commits mentioning [T-n], or --no-commit to skip git binding.`,
	Example: `  tk done T-1                  # Snapshots latest HEAD commit & diff
  tk done                      # Infer task from current git branch
  tk done T-1 --diff           # Snapshots uncommitted working tree diff
  tk done T-1 -c abc1234       # Attach specific commit ref
  tk done T-1 -c main..HEAD    # Attach commit range
  tk done T-1 --scan           # Scan git log for commits mentioning T-1
  tk done T-1 --no-commit      # Mark done without attaching git info`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		taskID, err := resolveOrInferTaskID(args)
		if err != nil {
			return err
		}

		var commits []domain.TaskCommit
		autoHead := false
		if !doneNoCommit && repoRoot != "" {
			switch {
			case doneDiff:
				ci, err := git.GetWorkingDiff(repoRoot)
				if err != nil {
					return fmt.Errorf("failed to get working tree diff: %w", err)
				}
				commits = commitInfosToDomain([]*git.CommitInfo{ci})
			case doneScan:
				infos, err := git.ScanCommitsForTask(repoRoot, taskID, doneCommit)
				if err != nil {
					return fmt.Errorf("failed to scan git log: %w", err)
				}
				commits = commitInfosToDomain(infos)
			case doneCommit != "" && strings.Contains(doneCommit, ".."):
				infos, err := git.GetCommitRange(repoRoot, doneCommit)
				if err != nil {
					return fmt.Errorf("failed to lookup commit range %q: %w", doneCommit, err)
				}
				commits = commitInfosToDomain(infos)
			case doneCommit != "":
				ci, err := git.GetCommit(repoRoot, doneCommit)
				if err != nil {
					return fmt.Errorf("failed to lookup commit %q: %w", doneCommit, err)
				}
				commits = commitInfosToDomain([]*git.CommitInfo{ci})
			default:
				ci, err := git.GetLatestCommit(repoRoot)
				if err == nil && ci != nil {
					autoHead = true
					commits = commitInfosToDomain([]*git.CommitInfo{ci})
				}
			}
		} else if doneCommit != "" && repoRoot == "" {
			return fmt.Errorf("cannot attach commit %q: not in a git repository", doneCommit)
		}

		var updated *domain.Task
		author := resolveAuthor("")

		if len(commits) > 0 {
			updated, err = taskClient.AttachGitCommits(taskID, commits, true, doneForce)
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

		if doneQuiet {
			fmt.Println(updated.ID)
			return nil
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		fmt.Printf("✓ Marked %s as DONE\n", updated.ID)
		if len(commits) > 1 {
			fmt.Printf("+ Attached %d commits (latest: %s %q)\n", len(commits), updated.CommitSHA, updated.CommitMsg)
		} else if updated.CommitSHA != "" {
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
	doneCmd.Flags().StringVarP(&doneCommit, "commit", "c", "", "Specific git commit hash, ref, or range (e.g. main..HEAD) to attach")
	doneCmd.Flags().BoolVar(&doneDiff, "diff", false, "Attach current uncommitted working tree diff instead of commit")
	doneCmd.Flags().BoolVar(&doneScan, "scan", false, "Scan git log and attach all commits mentioning the task ID")
	doneCmd.Flags().BoolVar(&doneNoCommit, "no-commit", false, "Do not auto-attach git commit")
	doneCmd.Flags().BoolVarP(&doneForce, "force", "f", false, "Force completion even if prerequisite dependencies are unfinished")
	doneCmd.Flags().BoolVarP(&doneQuiet, "quiet", "q", false, "Print only the completed task ID")
}
