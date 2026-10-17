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
	reviewAssign   string
	reviewNote     string
	reviewNoteAlt  string
	reviewClaim    bool
	reviewClear    bool
)

var reviewCmd = &cobra.Command{
	Use:   "review [id] [flags]",
	Short: "Review a task (stamping 'reviewed-by:<tag>') with an optional reviewer note",
	Long: `Reviews a task by stamping a 'reviewed-by:<tag>' tag and setting the task reviewer.
A reviewer note (-m) is optional and, when provided, is stored in the separate Reviewer Notes section.

In an interactive terminal, your configured user/git identity is used automatically without prompting.
In a dumb or non-interactive terminal (such as an AI agent runner), explicit reviewer identification
is required via -R/--reviewer, --actor, or TK_ACTOR.

To pre-assign a reviewer without completing the review yet, use --assign <name> (or 'tk edit -R <name>').`,
	Example: `  tk review T-1
  tk review T-1 -m "LGTM: verified edge cases and race tests"
  tk review T-1 --actor agent-reviewer -m "Approved after benchmark pass"
  tk review T-1 -R alice
  tk review T-1 --assign bob
  tk review T-1 --clear`,
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

		if cmd.Flags().Changed("assign") {
			assigneeReviewer := strings.TrimSpace(reviewAssign)
			if assigneeReviewer == "" {
				return fmt.Errorf("--assign reviewer name cannot be empty")
			}
			updated, err := taskClient.UpdateTask(protocol.UpdateTaskPayload{
				ID:       taskID,
				Reviewer: &assigneeReviewer,
				Author:   author,
			})
			if err != nil {
				return fmt.Errorf("failed to assign reviewer: %w", err)
			}
			if jsonFlag {
				return outputJSON(updated)
			}
			fmt.Printf("✓ Assigned reviewer %q to %s (awaiting review)\n", updated.Reviewer, updated.ID)
			return nil
		}

		note := strings.TrimSpace(reviewNote)
		if note == "" {
			note = strings.TrimSpace(reviewNoteAlt)
		}

		// Support piped stdin for optional review note
		stat, _ := os.Stdin.Stat()
		piped := (stat.Mode() & os.ModeCharDevice) == 0
		if piped && note == "" {
			bytes, err := io.ReadAll(os.Stdin)
			if err == nil && len(bytes) > 0 {
				note = strings.TrimSpace(string(bytes))
			}
		}

		reviewer := strings.TrimSpace(reviewReviewer)
		if reviewer == "" && strings.TrimSpace(flagActor) != "" {
			reviewer = strings.TrimSpace(flagActor)
		}
		if reviewer == "" && strings.TrimSpace(os.Getenv("TK_ACTOR")) != "" {
			reviewer = strings.TrimSpace(os.Getenv("TK_ACTOR"))
		}

		if reviewer == "" {
			if !IsInteractiveTerminal() {
				return fmt.Errorf("reviewer identification required in non-interactive/dumb terminal: pass -R <tag> or --actor <tag>")
			}
			reviewer = author
		}

		updated, err := taskClient.ReviewTask(taskID, reviewer, note, reviewer)
		if err != nil {
			return fmt.Errorf("failed to review task: %w", err)
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		if note != "" {
			fmt.Printf("✓ Reviewed %s [reviewed-by:%s] (%d review note(s))\n", updated.ID, updated.Reviewer, len(updated.ReviewerNotes))
		} else {
			fmt.Printf("✓ Reviewed %s [reviewed-by:%s]\n", updated.ID, updated.Reviewer)
		}
		return nil
	},
}

func init() {
	reviewCmd.Flags().StringVarP(&reviewReviewer, "reviewer", "R", "", "Reviewer tag/identity (required in dumb/non-interactive terminals)")
	reviewCmd.Flags().StringVar(&reviewAssign, "assign", "", "Pre-assign a reviewer without marking the task as reviewed yet")
	reviewCmd.Flags().StringVarP(&reviewNote, "message", "m", "", "Optional reviewer note / feedback to record")
	reviewCmd.Flags().StringVarP(&reviewNoteAlt, "note", "n", "", "Optional reviewer note (alias for -m, --message)")
	reviewCmd.Flags().BoolVar(&reviewClaim, "claim", false, "Review as the current actor")
	reviewCmd.Flags().BoolVar(&reviewClear, "clear", false, "Clear the assigned reviewer and reviewed-by tag")
}
