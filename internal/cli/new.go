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
	newTitle       string
	newStatus      string
	newPriority    string
	newTags        string
	newAssignee    string
	newReviewer    string
	newMessage     string
	newDescription string
	newDependsOn   string
	newTopic       string
	newQuiet       bool
)

var newCmd = &cobra.Command{
	Use:   "new [title] [flags]",
	Short: "Create a new task (opens $EDITOR or uses CLI flags)",
	Long: `Create a new task in the current scope (local git repository or global).
If no flags are provided and input is not piped, opens $EDITOR with a template
when attached to an interactive terminal.
Supports piped input from stdin, where the first line becomes the title
and subsequent lines form the description.`,
	Example: `  tk new -t "Fix auth crash" -p high -T bugs
  tk new "Fix auth crash" -q
  tk new -t "Refactor storage" -m "Migrate to WAL mode" -R alice
  echo -e "Update docs\nInclude quickstart" | tk new
  tk new -t "Ultimate ability" -T hero-system --after T-12
  tk new              # Opens interactive editor ($EDITOR)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("tags") {
			return fmt.Errorf("tags were replaced by topics; use -T/--topic or 'tk topic import-tags'")
		}

		var title string
		var prio = domain.PriorityNormal
		var tags []string
		var assignee string
		var reviewer string
		var desc string
		var editorTopic string

		hasTitleArg := len(args) > 0 && strings.TrimSpace(strings.Join(args, " ")) != ""
		hasFlags := cmd.Flags().Changed("title") ||
			hasTitleArg ||
			cmd.Flags().Changed("priority") ||
			cmd.Flags().Changed("assignee") ||
			cmd.Flags().Changed("reviewer") ||
			cmd.Flags().Changed("message") ||
			cmd.Flags().Changed("desc")

		hasExplicitTitle := cmd.Flags().Changed("title") || hasTitleArg
		hasExplicitDesc := cmd.Flags().Changed("desc") || cmd.Flags().Changed("message")
		hasExplicitTitleAndDesc := hasExplicitTitle && hasExplicitDesc

		var pipedText string
		var piped bool
		if !hasExplicitTitleAndDesc {
			pipedText, piped = ReadPipedStdin(hasFlags)
		}

		if cmd.Flags().Changed("title") && strings.TrimSpace(newTitle) == "" && !hasTitleArg && !piped {
			return fmt.Errorf("task title cannot be empty")
		}

		if hasFlags || piped {
			title = strings.TrimSpace(newTitle)
			if title == "" && hasTitleArg {
				title = strings.TrimSpace(strings.Join(args, " "))
			}
			if newPriority != "" {
				p, err := domain.ParsePriority(newPriority)
				if err != nil {
					return err
				}
				prio = p
			}
			if newTags != "" {
				tags = splitCommaList(newTags)
			}
			assignee = newAssignee
			reviewer = newReviewer
			desc = newMessage
			if desc == "" {
				desc = newDescription
			}

			if piped && !hasExplicitTitleAndDesc && pipedText != "" {
				if desc == "" {
					desc = pipedText
				} else {
					desc = desc + "\n\n" + pipedText
				}
			}

			if title == "" {
				// Use first line of piped desc if no title
				lines := strings.Split(desc, "\n")
				if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
					return fmt.Errorf("task title cannot be empty (use -t or provide in template)")
				}
				title = lines[0]
				if len(lines) > 1 {
					desc = strings.TrimSpace(strings.Join(lines[1:], "\n"))
				} else {
					desc = ""
				}
			}
		} else {
			if !IsInteractiveTerminal() {
				return fmt.Errorf("cannot open editor in non-interactive terminal: pass -t <title> or pipe input via stdin")
			}
			// Interactive editor
			parsed, err := editor.OpenNewTask(cfg.Editor, "")
			if err != nil {
				return err
			}
			title = parsed.Title
			prio = parsed.Priority
			tags = parsed.Tags
			assignee = parsed.Assignee
			reviewer = parsed.Reviewer
			desc = parsed.Description
			editorTopic = parsed.Topic
		}

		if strings.TrimSpace(title) == "" {
			return fmt.Errorf("task title cannot be empty (use -t or provide in template)")
		}
		normalized, err := domain.NormalizeTitle(title)
		if err != nil {
			return err
		}
		title = normalized

		var status = domain.StatusOpen
		if cmd.Flags().Changed("status") {
			s, err := domain.ParseStatus(newStatus)
			if err != nil {
				return err
			}
			status = s
		}

		author := resolveAuthor("")

		var dependsOn []string
		if newDependsOn != "" {
			dependsOn = normIDs(splitCommaList(newDependsOn))
		}

		topic := newTopic
		if !cmd.Flags().Changed("topic") {
			if editorTopic != "" {
				topic = editorTopic
			} else {
				topic = defaultTopicFor(domain.KindTask)
			}
		}

		task, err := taskClient.CreateTask(protocol.CreateTaskPayload{
			Title:       title,
			Description: desc,
			Status:      status,
			Priority:    prio,
			Tags:        tags,
			Author:      author,
			Assignee:    assignee,
			Reviewer:    reviewer,
			DependsOn:   dependsOn,
			Topic:       topic,
		})
		if err != nil {
			return fmt.Errorf("failed to create task: %w", err)
		}

		if jsonFlag {
			return outputJSON(task)
		}
		if newQuiet {
			fmt.Println(task.ID)
			return nil
		}

		fmt.Printf("✓ Created %s: %s%s\n", task.ID, task.Title, topicSuffix(task))
		if len(task.DependsOn) > 0 {
			if task.IsBlocked {
				fmt.Printf("  🔒 Blocked by unfinished prerequisite(s): [%s]\n", strings.Join(task.BlockedBy, ", "))
			} else {
				fmt.Printf("  ⚡ Prerequisites completed: [%s]\n", strings.Join(task.DependsOn, ", "))
			}
		}
		return nil
	},
}

func init() {
	newCmd.Flags().StringVarP(&newTitle, "title", "t", "", "Task title")
	newCmd.Flags().StringVarP(&newStatus, "status", "s", "open", "Initial status (open, in_progress, done, closed)")
	newCmd.Flags().StringVarP(&newPriority, "priority", "p", "normal", "Priority (low, normal, high, urgent)")
	newCmd.Flags().StringVarP(&newTags, "tags", "l", "", "Comma-separated tags (e.g. bug,backend)")
	newCmd.Flags().StringVarP(&newAssignee, "assignee", "a", "", "Task assignee")
	newCmd.Flags().StringVarP(&newReviewer, "reviewer", "R", "", "Task reviewer")
	newCmd.Flags().StringVarP(&newMessage, "message", "m", "", "Task description")
	newCmd.Flags().StringVarP(&newDescription, "desc", "d", "", "Task description")
	newCmd.Flags().StringVar(&newDependsOn, "after", "", "Comma-separated list of prerequisite task IDs that this task depends on")
	newCmd.Flags().StringVar(&newDependsOn, "depends-on", "", "Alias for --after")
	newCmd.Flags().StringVarP(&newTopic, "topic", "T", "", "Tasks topic to create the task in (default: 'tk topic use' topic, else unfiled)")
	newCmd.Flags().BoolVarP(&newQuiet, "quiet", "q", false, "Print only the new task ID")
	_ = newCmd.Flags().MarkHidden("tags")
}
