package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	reviewReviewer string
	reviewNote     string
	reviewNoteAlt  string
	reviewClaim    bool
	reviewClear    bool
)

var reviewCmd = &cobra.Command{
	Use:   "review [id] [flags]",
	Short: "Assign a reviewer or record a reviewer note on a task",
	Long: `Assign or claim the reviewer role for a task and/or append a reviewer note.
Every task requires an assigned reviewer and at least one reviewer note in the
separate Reviewer Notes section before it can be closed.
When [id] is omitted, infers the active task from the current git branch.`,
	Example: `  tk review T-1 --claim
  tk review T-1 -R alice
  tk review T-1 -m "LGTM: verified edge cases and race tests"
  tk review T-1 -R bob -m "Approved after benchmark pass"
  echo "Reviewed diff and tests" | tk review T-1`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, err := resolveOrInferTaskID(args)
		if err != nil {
			return err
		}

		author := resolveAuthor(flagActor)

		if reviewClear {
			empty := ""
			updated, err := taskClient.UpdateTask(protocol.UpdateTaskPayload{
				ID:       taskID,
				Reviewer: &empty,
				Author:   author,
			})
			if err != nil {
				return fmt.Errorf("failed to clear reviewer: %w", err)
			}
			if jsonFlag {
				return outputJSON(updated)
			}
			fmt.Printf("✓ Cleared reviewer on %s: %s\n", updated.ID, updated.Title)
			return nil
		}

		note := strings.TrimSpace(reviewNote)
		if note == "" {
			note = strings.TrimSpace(reviewNoteAlt)
		}

		// Support piped stdin for review note
		stat, _ := os.Stdin.Stat()
		piped := (stat.Mode() & os.ModeCharDevice) == 0
		if piped && note == "" {
			bytes, err := io.ReadAll(os.Stdin)
			if err == nil && len(bytes) > 0 {
				note = strings.TrimSpace(string(bytes))
			}
		}

		reviewer := strings.TrimSpace(reviewReviewer)
		if reviewClaim && reviewer == "" {
			reviewer = author
		}
		// If neither -R nor -m was explicitly provided, default to claiming reviewer as current actor
		if !cmd.Flags().Changed("reviewer") && !reviewClaim && note == "" {
			reviewer = author
		}

		updated, err := taskClient.ReviewTask(taskID, reviewer, note, author)
		if err != nil {
			return fmt.Errorf("failed to review task: %w", err)
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		if note != "" {
			fmt.Printf("✓ Recorded review on %s by %q (%d review note(s) total)\n", updated.ID, updated.Reviewer, len(updated.ReviewerNotes))
		} else {
			fmt.Printf("✓ Assigned reviewer %q to %s: %s\n", updated.Reviewer, updated.ID, updated.Title)
		}
		return nil
	},
}

func init() {
	reviewCmd.Flags().StringVarP(&reviewReviewer, "reviewer", "R", "", "Reviewer identity to assign")
	reviewCmd.Flags().StringVarP(&reviewNote, "message", "m", "", "Reviewer note / feedback to record")
	reviewCmd.Flags().StringVarP(&reviewNoteAlt, "note", "n", "", "Reviewer note (alias for -m, --message)")
	reviewCmd.Flags().BoolVar(&reviewClaim, "claim", false, "Claim reviewer role as the current actor")
	reviewCmd.Flags().BoolVar(&reviewClear, "clear", false, "Clear the assigned reviewer")
}
