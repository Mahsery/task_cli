package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/editor"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	editTitle       string
	editStatus      string
	editPriority    string
	editTags        string
	editAddTag      string
	editRemoveTag   string
	editAssignee    string
	editReviewer    string
	editNote        string
	editNoteAlias   string
	editReviewNote  string
	editDescription string
	editBranch      string
	editClaim       bool
)

var editCmd = &cobra.Command{
	Use:   "edit [id] [flags]",
	Short: "Edit a task (opens $EDITOR or uses CLI flags)",
	Long: `Update task attributes using CLI flags or open the task in $EDITOR.
Providing flags updates the specified fields directly; omitting all flags
opens an interactive buffer with existing values pre-populated.
When [id] is omitted, infers the active task from the current git branch.`,
	Example: `  tk edit T-1 -s in_progress -p urgent
  tk edit T-1 --claim
  tk edit T-1 -R alice --review-note "LGTM"
  tk edit T-1 -m "Patch sent to mailing list"
  tk edit -m "Progress on current branch task"
  tk edit T-1          # Opens full task buffer in $EDITOR`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("tags") || cmd.Flags().Changed("add-tag") || cmd.Flags().Changed("remove-tag") {
			return fmt.Errorf("tags were replaced by topics; use 'tk move' / 'tk topic' / 'tk topic import-tags'")
		}

		taskID, err := resolveOrInferTaskID(args)
		if err != nil {
			return err
		}
		task, err := taskClient.GetTask(taskID)
		if err != nil {
			return err
		}

		if editClaim {
			actor := resolveAuthor(editAssignee)
			claimed, err := taskClient.ClaimTask(taskID, actor, false)
			if err != nil {
				return err
			}
			task = claimed
		}

		hasFlags := cmd.Flags().Changed("title") ||
			cmd.Flags().Changed("status") ||
			cmd.Flags().Changed("priority") ||
			cmd.Flags().Changed("tags") ||
			cmd.Flags().Changed("add-tag") ||
			cmd.Flags().Changed("remove-tag") ||
			(cmd.Flags().Changed("assignee") && !editClaim) ||
			cmd.Flags().Changed("reviewer") ||
			cmd.Flags().Changed("note") ||
			cmd.Flags().Changed("comment") ||
			cmd.Flags().Changed("review-note") ||
			cmd.Flags().Changed("desc") ||
			cmd.Flags().Changed("branch")

		if editClaim && !hasFlags {
			if jsonFlag {
				return outputJSON(task)
			}
			fmt.Printf("✓ Claimed %s as %q (status: %s)\n", task.ID, task.Assignee, task.Status)
			return nil
		}

		var payload protocol.UpdateTaskPayload
		payload.ID = taskID
		payload.Author = resolveAuthor("")

		if hasFlags {
			if cmd.Flags().Changed("title") && strings.TrimSpace(editTitle) != task.Title {
				normalized, err := domain.NormalizeTitle(editTitle)
				if err != nil {
					return err
				}
				payload.Title = &normalized
			}
			if cmd.Flags().Changed("status") {
				s, err := domain.ParseStatus(editStatus)
				if err != nil {
					return err
				}
				payload.Status = &s
			}
			if cmd.Flags().Changed("priority") {
				p, err := domain.ParsePriority(editPriority)
				if err != nil {
					return err
				}
				payload.Priority = &p
			}
			if cmd.Flags().Changed("assignee") && !editClaim {
				payload.Assignee = &editAssignee
			}
			if cmd.Flags().Changed("reviewer") {
				payload.Reviewer = &editReviewer
			}
			if cmd.Flags().Changed("note") {
				payload.AddNote = &editNote
			} else if cmd.Flags().Changed("comment") {
				payload.AddNote = &editNoteAlias
			}
			if cmd.Flags().Changed("review-note") {
				payload.AddReviewNote = &editReviewNote
			}
			if cmd.Flags().Changed("desc") {
				payload.Description = &editDescription
			}
			if cmd.Flags().Changed("branch") {
				payload.Branch = &editBranch
			}
		} else if !editClaim {
			// Interactive editor
			parsed, err := editor.OpenEditTask(cfg.Editor, task)
			if err != nil {
				return err
			}
			if strings.TrimSpace(parsed.Title) == "" {
				return fmt.Errorf("task title cannot be empty")
			}
			ch := parsed.Changes(task)
			if ch.Empty() {
				fmt.Printf("%s: no changes\n", task.ID)
				return nil
			}
			payload.Title = ch.Title
			payload.Status = ch.Status
			payload.Priority = ch.Priority
			payload.Tags = ch.Tags
			payload.Assignee = ch.Assignee
			payload.Reviewer = ch.Reviewer
			payload.Description = ch.Description
			payload.AddNote = ch.AddNote
			payload.AddReviewNote = ch.AddReviewNote
			if ch.Topic != "" {
				if _, err := taskClient.MoveItems([]string{task.ID}, ch.Topic, resolveAuthor("")); err != nil {
					return fmt.Errorf("failed to move %s to @%s: %w", task.ID, ch.Topic, err)
				}
			}
		}

		var updated *domain.Task
		if payloadIsEmpty(payload) {
			// Only the topic changed (already applied via MoveItems).
			updated, err = taskClient.GetTask(taskID)
		} else {
			updated, err = taskClient.UpdateTask(payload)
		}
		if err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		if jsonFlag {
			return outputJSON(updated)
		}

		fmt.Printf("✓ Updated %s: %s (status: %s)\n", updated.ID, updated.Title, updated.Status)
		return nil
	},
}

func init() {
	editCmd.Flags().StringVarP(&editTitle, "title", "t", "", "Update title")
	editCmd.Flags().StringVarP(&editStatus, "status", "s", "", "Update status (open, in_progress, done, closed)")
	editCmd.Flags().StringVarP(&editPriority, "priority", "p", "", "Update priority (low, normal, high, urgent)")
	editCmd.Flags().StringVarP(&editTags, "tags", "l", "", "Replace tags (comma-separated)")
	editCmd.Flags().StringVar(&editAddTag, "add-tag", "", "Append a single tag atomically")
	editCmd.Flags().StringVar(&editRemoveTag, "remove-tag", "", "Remove a single tag atomically")
	editCmd.Flags().BoolVar(&editClaim, "claim", false, "Claim the task for the current actor")
	editCmd.Flags().StringVarP(&editAssignee, "assignee", "a", "", "Update assignee")
	editCmd.Flags().StringVarP(&editReviewer, "reviewer", "R", "", "Update reviewer")
	editCmd.Flags().StringVarP(&editNote, "note", "m", "", "Append a note/comment")
	editCmd.Flags().StringVarP(&editNoteAlias, "comment", "n", "", "Append a note/comment (alias for -m, --note)")
	editCmd.Flags().StringVar(&editReviewNote, "review-note", "", "Append a reviewer note")
	editCmd.Flags().StringVarP(&editDescription, "desc", "d", "", "Update description")
	editCmd.Flags().StringVarP(&editBranch, "branch", "b", "", "Update bound git branch name")
	_ = editCmd.Flags().MarkHidden("tags")
	_ = editCmd.Flags().MarkHidden("add-tag")
	_ = editCmd.Flags().MarkHidden("remove-tag")
}

// payloadIsEmpty reports whether p would change nothing on the task.
func payloadIsEmpty(p protocol.UpdateTaskPayload) bool {
	return p.Title == nil && p.Description == nil && p.Status == nil && p.Priority == nil &&
		p.Tags == nil && len(p.AddTags) == 0 && len(p.RemoveTags) == 0 && p.Assignee == nil &&
		p.Reviewer == nil && p.AddNote == nil && p.AddReviewNote == nil &&
		p.Branch == nil && p.ExternalRef == nil && p.PRURL == nil
}
