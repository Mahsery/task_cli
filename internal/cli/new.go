package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/editor"
	"task_cli/internal/git"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	newTitle       string
	newPriority    string
	newTags        string
	newAssignee    string
	newMessage     string
	newDescription string
)

var newCmd = &cobra.Command{
	Use:   "new [flags]",
	Short: "Create a new task (opens $EDITOR or uses CLI flags)",
	Long: `Create a new task in the current scope (local git repository or global).
If no flags are provided and input is not piped, opens $EDITOR with a template.
Supports piped input from stdin, where the first line becomes the title
and subsequent lines form the description.`,
	Example: `  tk new -t "Fix auth crash" -p high -l bug,auth
  tk new -t "Refactor storage" -m "Migrate to WAL mode"
  echo -e "Update docs\nInclude quickstart" | tk new
  tk new              # Opens interactive editor ($EDITOR)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var title string
		var prio domain.Priority = domain.PriorityNormal
		var tags []string
		var assignee string
		var desc string

		// Check if input is piped through stdin
		stat, _ := os.Stdin.Stat()
		piped := (stat.Mode() & os.ModeCharDevice) == 0

		if cmd.Flags().Changed("title") && strings.TrimSpace(newTitle) == "" && !piped {
			return fmt.Errorf("task title cannot be empty")
		}

		hasFlags := cmd.Flags().Changed("title") ||
			cmd.Flags().Changed("priority") ||
			cmd.Flags().Changed("tags") ||
			cmd.Flags().Changed("assignee") ||
			cmd.Flags().Changed("message") ||
			cmd.Flags().Changed("desc")

		if hasFlags || piped {
			title = strings.TrimSpace(newTitle)
			if newPriority != "" {
				p, err := domain.ParsePriority(newPriority)
				if err != nil {
					return err
				}
				prio = p
			}
			if newTags != "" {
				for _, t := range strings.Split(newTags, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}
			assignee = newAssignee
			desc = newMessage
			if desc == "" {
				desc = newDescription
			}

			if piped {
				bytes, err := io.ReadAll(os.Stdin)
				if err == nil && len(bytes) > 0 {
					if desc == "" {
						desc = strings.TrimSpace(string(bytes))
					} else {
						desc = desc + "\n\n" + strings.TrimSpace(string(bytes))
					}
				}
			}

			if title == "" {
				// Use first line of piped desc if no title
				lines := strings.Split(desc, "\n")
				if len(lines) > 0 && lines[0] != "" {
					title = lines[0]
					if len(lines) > 1 {
						desc = strings.TrimSpace(strings.Join(lines[1:], "\n"))
					}
				} else {
					return fmt.Errorf("task title cannot be empty (use -t or provide in template)")
				}
			}
		} else {
			// Interactive editor
			parsed, err := editor.OpenNewTask(cfg.Editor, "")
			if err != nil {
				return err
			}
			title = parsed.Title
			prio = parsed.Priority
			tags = parsed.Tags
			assignee = parsed.Assignee
			desc = parsed.Description
		}

		title = strings.TrimSpace(title)
		if title == "" {
			return fmt.Errorf("task title cannot be empty (use -t or provide in template)")
		}

		author := cfg.Actor
		if author == "" {
			author = git.GetAuthor()
		}
		task, err := taskClient.CreateTask(protocol.CreateTaskPayload{
			Title:       title,
			Description: desc,
			Status:      domain.StatusOpen,
			Priority:    prio,
			Tags:        tags,
			Author:      author,
			Assignee:    assignee,
		})
		if err != nil {
			return fmt.Errorf("failed to create task: %w", err)
		}

		if jsonFlag {
			return outputJSON(task)
		}

		fmt.Printf("✓ Created %s: %s\n", task.ID, task.Title)
		return nil
	},
}

func init() {
	newCmd.Flags().StringVarP(&newTitle, "title", "t", "", "Task title")
	newCmd.Flags().StringVarP(&newPriority, "priority", "p", "normal", "Priority (low, normal, high, urgent)")
	newCmd.Flags().StringVarP(&newTags, "tags", "l", "", "Comma-separated tags (e.g. bug,backend)")
	newCmd.Flags().StringVarP(&newAssignee, "assignee", "a", "", "Task assignee")
	newCmd.Flags().StringVarP(&newMessage, "message", "m", "", "Task description")
	newCmd.Flags().StringVarP(&newDescription, "desc", "d", "", "Task description")
}
