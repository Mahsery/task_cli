package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	postTitle    string
	postDesc     string
	postTopic    string
	postPriority string
	replyMessage string
	promoteTo    string
	moveTo       string
	moveFrom     string
)

var postCmd = &cobra.Command{
	Use:   "post [flags]",
	Short: "Create a board post (P-n), e.g. a bug report or idea",
	Long: `Create a post. Posts live in board topics, have no assignee, claims or
dependencies, and move through open -> resolved/closed. Promote a post to a task
with 'tk promote'. The description may contain [[wiki links]].`,
	Example: `  tk post -t "Crash on load" --topic bug-reports -d "Stack trace: ..."
  echo "Idea body" | tk post -t "Better minimap" -T ideas
  tk post -t "Seen with [[T-12]]"     # uses the default board from 'tk topic use'`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		title := strings.TrimSpace(postTitle)
		desc := postDesc
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			if b, err := io.ReadAll(os.Stdin); err == nil && len(b) > 0 {
				piped := strings.TrimSpace(string(b))
				if title == "" {
					lines := strings.SplitN(piped, "\n", 2)
					title = lines[0]
					if len(lines) > 1 {
						piped = strings.TrimSpace(lines[1])
					} else {
						piped = ""
					}
				}
				if piped != "" {
					if desc != "" {
						desc += "\n\n"
					}
					desc += piped
				}
			}
		}
		if title == "" {
			return fmt.Errorf("post title cannot be empty (use -t)")
		}
		prio := domain.PriorityNormal
		if cmd.Flags().Changed("priority") {
			p, err := domain.ParsePriority(postPriority)
			if err != nil {
				return err
			}
			prio = p
		}
		topic := postTopic
		if !cmd.Flags().Changed("topic") {
			topic = defaultTopicFor(domain.KindPost)
		}
		post, err := taskClient.CreateTask(protocol.CreateTaskPayload{
			Title: title, Description: desc, Status: domain.StatusOpen, Priority: prio,
			Author: resolveAuthor(""), Kind: domain.KindPost, Topic: topic,
		})
		if err != nil {
			return fmt.Errorf("failed to create post: %w", err)
		}
		if jsonFlag {
			return outputJSON(post)
		}
		fmt.Printf("✓ Posted %s: %s%s\n", post.ID, post.Title, topicSuffix(post))
		return nil
	},
}

var replyCmd = &cobra.Command{
	Use:     "reply [id] -m <message>",
	Aliases: []string{"comment"},
	Short:   "Add a reply/comment to a post or task (supports [[wiki links]])",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		msg := strings.TrimSpace(replyMessage)
		if msg == "" {
			return fmt.Errorf("reply cannot be empty (use -m)")
		}
		taskID, err := resolveOrInferTaskID(args)
		if err != nil {
			return err
		}
		item, err := taskClient.UpdateTask(protocol.UpdateTaskPayload{ID: taskID, AddNote: &msg, Author: resolveAuthor("")})
		if err != nil {
			return err
		}
		if jsonFlag {
			return outputJSON(item)
		}
		fmt.Printf("✓ Replied on %s (%d note(s))\n", item.ID, len(item.Notes))
		return nil
	},
}

var promoteCmd = &cobra.Command{
	Use:   "promote <post-id> --to <tasks-topic>",
	Short: "Turn a post into a task (the two are linked both ways and the post is resolved)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		to := promoteTo
		if !cmd.Flags().Changed("to") {
			to = defaultTopicFor(domain.KindTask)
		}
		res, err := taskClient.PromotePost(normID(args[0]), to, resolveAuthor(""))
		if err != nil {
			return err
		}
		if jsonFlag {
			return outputJSON(res)
		}
		fmt.Printf("✓ Promoted %s → %s: %s%s\n", res.Post.ID, res.Task.ID, res.Task.Title, topicSuffix(res.Task))
		return nil
	},
}

var moveCmd = &cobra.Command{
	Use:     "move <id...|@topic...> --to <topic|unfiled>",
	Aliases: []string{"mv"},
	Short:   "Move tasks/posts to another topic, or topics into another group",
	Long: `Move items or topics.

  tk move T-15 p3 t9 --to hero-system     items into a topic
  tk move T-15 --to unfiled               remove an item from its topic
  tk move @bug-reports --to game          a topic under another topic ('root' for top level)
  tk move --from inbox --to hero-system   every item of one topic into another

Tasks can only go into tasks topics and posts only into boards. Item IDs never
change, so dependencies and [[links]] keep working after a move.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("to") {
			return fmt.Errorf("destination required: --to <topic|unfiled|root>")
		}
		var itemIDs, topicRefs []string
		for _, a := range args {
			if strings.HasPrefix(a, "@") {
				topicRefs = append(topicRefs, a)
			} else {
				itemIDs = append(itemIDs, normID(a))
			}
		}
		if moveFrom != "" {
			items, err := taskClient.ListTasks(domain.TaskFilter{Scope: scope, RepoPath: repoRoot, Topic: moveFrom, IncludeArchived: true, OmitNotes: true})
			if err != nil {
				return err
			}
			for _, it := range items {
				itemIDs = append(itemIDs, it.ID)
			}
			if len(items) == 0 {
				fmt.Printf("No items in @%s\n", domain.NormalizeTopicRef(moveFrom))
			}
		}
		if len(itemIDs) == 0 && len(topicRefs) == 0 && moveFrom == "" {
			return fmt.Errorf("nothing to move: give item IDs, @topics or --from <topic>")
		}

		for _, ref := range topicRefs {
			parent := moveTo
			if domain.NormalizeTopicRef(parent) == "root" {
				parent = ""
			}
			tp, err := taskClient.MoveTopic(ref, parent)
			if err != nil {
				return err
			}
			if !jsonFlag {
				fmt.Printf("✓ Moved topic to @%s\n", tp.Path)
			}
		}
		if len(itemIDs) == 0 {
			return nil
		}
		moved, err := taskClient.MoveItems(itemIDs, moveTo, resolveActor(""))
		if err != nil {
			return err
		}
		if jsonFlag {
			return outputJSON(moved)
		}
		for _, it := range moved {
			fmt.Printf("✓ %s → %s  %s\n", it.ID, topicLabel(it), it.Title)
		}
		return nil
	},
}

// topicLabel renders an item's topic as "@path" or "unfiled".
func topicLabel(t *domain.Task) string {
	if t == nil || t.TopicPath == "" {
		return domain.UnfiledTopic
	}
	return "@" + t.TopicPath
}

func topicSuffix(t *domain.Task) string {
	if t == nil || t.TopicPath == "" {
		return ""
	}
	return "  (@" + t.TopicPath + ")"
}

func init() {
	postCmd.Flags().StringVarP(&postTitle, "title", "t", "", "Post title")
	postCmd.Flags().StringVarP(&postDesc, "desc", "d", "", "Post body")
	postCmd.Flags().StringVarP(&postDesc, "message", "m", "", "Post body (alias for --desc)")
	postCmd.Flags().StringVarP(&postTopic, "topic", "T", "", "Board topic (default: 'tk topic use' board, else unfiled)")
	postCmd.Flags().StringVarP(&postPriority, "priority", "p", "normal", "Priority (low, normal, high, urgent)")

	replyCmd.Flags().StringVarP(&replyMessage, "message", "m", "", "Reply text")

	promoteCmd.Flags().StringVar(&promoteTo, "to", "", "Destination tasks topic (default: 'tk topic use' topic, else unfiled)")

	moveCmd.Flags().StringVar(&moveTo, "to", "", "Destination topic, 'unfiled' (items) or 'root' (topics)")
	moveCmd.Flags().StringVar(&moveFrom, "from", "", "Move every item of this topic")

	RootCmd.AddCommand(postCmd, replyCmd, promoteCmd, moveCmd)
}
