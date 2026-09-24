package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/editor"
	"task_cli/internal/git"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	editTitle       string
	editStatus      string
	editPriority    string
	editTags        string
	editAddTag      string
	editAssignee    string
	editNote        string
	editDescription string
)

var editCmd = &cobra.Command{
	Use:   "edit <id> [flags]",
	Short: "Edit a task (opens $EDITOR or uses CLI flags)",
	Long: `Update task attributes using CLI flags or open the task in $EDITOR.
Providing flags updates the specified fields directly; omitting all flags
opens an interactive buffer with existing values pre-populated.`,
	Example: `  tk edit T-1 -s in_progress -p urgent
  tk edit T-1 --add-tag review
  tk edit T-1 -n "Patch sent to mailing list"
  tk edit T-1          # Opens full task buffer in $EDITOR`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		task, err := taskClient.GetTask(taskID)
		if err != nil {
			return err
		}

		hasFlags := cmd.Flags().Changed("title") ||
			cmd.Flags().Changed("status") ||
			cmd.Flags().Changed("priority") ||
			cmd.Flags().Changed("tags") ||
			cmd.Flags().Changed("add-tag") ||
			cmd.Flags().Changed("assignee") ||
			cmd.Flags().Changed("note") ||
			cmd.Flags().Changed("desc")

		var payload protocol.UpdateTaskPayload
		payload.ID = taskID
		payload.Author = git.GetAuthor()

		if hasFlags {
			if cmd.Flags().Changed("title") {
				payload.Title = &editTitle
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
			if cmd.Flags().Changed("tags") {
				var tags []string
				for _, t := range strings.Split(editTags, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
				payload.Tags = &tags
			}
			if cmd.Flags().Changed("add-tag") {
				newTags := append(task.Tags, strings.TrimSpace(editAddTag))
				payload.Tags = &newTags
			}
			if cmd.Flags().Changed("assignee") {
				payload.Assignee = &editAssignee
			}
			if cmd.Flags().Changed("note") {
				payload.AddNote = &editNote
			}
			if cmd.Flags().Changed("desc") {
				payload.Description = &editDescription
			}
		} else {
			// Interactive editor
			parsed, err := editor.OpenEditTask(cfg.Editor, task)
			if err != nil {
				return err
			}
			payload.Title = &parsed.Title
			payload.Status = &parsed.Status
			payload.Priority = &parsed.Priority
			payload.Tags = &parsed.Tags
			payload.Assignee = &parsed.Assignee
			payload.Description = &parsed.Description
			if parsed.NewNote != "" {
				payload.AddNote = &parsed.NewNote
			}
		}

		updated, err := taskClient.UpdateTask(payload)
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
	editCmd.Flags().StringVar(&editAddTag, "add-tag", "", "Append a single tag")
	editCmd.Flags().StringVarP(&editAssignee, "assignee", "a", "", "Update assignee")
	editCmd.Flags().StringVarP(&editNote, "note", "m", "", "Append a note/comment")
	editCmd.Flags().StringVarP(&editDescription, "desc", "d", "", "Update description")
}
