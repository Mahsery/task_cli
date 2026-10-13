package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	topicKind    string
	topicParent  string
	topicSlug    string
	topicDesc    string
	topicMoveTo  string
	topicTitle   string
	topicRmForce bool
)

var topicCmd = &cobra.Command{
	Use:     "topic <command>",
	Aliases: []string{"topics"},
	Short:   "Organise work into a tree of topics (boards for posts, task topics, groups)",
	Long: `Topics form a tree that organises items like a local Jira:

  board   holds posts (P-n) such as bug reports or ideas
  tasks   holds tasks (T-n) with dependency chains (dependencies may cross topics)
  group   a folder that only holds other topics

Topics are referenced by path (game/hero-system), by a unique slug (hero-system),
optionally prefixed with '@'. "unfiled" refers to items that belong to no topic.`,
	Example: `  tk topic new "Game" --kind group
  tk topic new "Hero System" --parent game
  tk topic new "Bug Reports" --kind board
  tk topic tree
  tk topic use hero-system      # default topic for 'tk new'
  tk topic mv hero-system --to ui
  tk topic rm old --move-to unfiled`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return topicTreeCmd.RunE(cmd, args)
	},
}

var topicNewCmd = &cobra.Command{
	Use:     "new <title>",
	Aliases: []string{"add", "create"},
	Short:   "Create a topic",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		kind, err := domain.ParseTopicKind(topicKind)
		if err != nil {
			return err
		}
		title := strings.Join(args, " ")
		parent := topicParent
		if parent == "" {
			raw := strings.Trim(strings.TrimPrefix(strings.TrimSpace(title), "@"), "/")
			if idx := strings.LastIndex(raw, "/"); idx > 0 && idx+1 < len(raw) {
				prefix := raw[:idx]
				suffix := raw[idx+1:]
				if !strings.Contains(prefix, " ") && !strings.HasPrefix(suffix, " ") {
					parent = prefix
					title = suffix
				}
			}
		}
		tp, err := taskClient.CreateTopic(protocol.CreateTopicPayload{
			Title: title, Slug: topicSlug, Description: topicDesc, Kind: kind, Parent: parent,
		})
		if err != nil {
			return err
		}
		if jsonFlag {
			return outputJSON(tp)
		}
		fmt.Printf("✓ Created %s topic @%s (%s)\n", tp.Kind, tp.Path, tp.Title)
		return nil
	},
}

var topicTreeCmd = &cobra.Command{
	Use:     "tree",
	Aliases: []string{"list", "ls"},
	Short:   "Show the topic tree with item counts",
	RunE: func(_ *cobra.Command, _ []string) error {
		topics, err := taskClient.ListTopics()
		if err != nil {
			return err
		}
		if jsonFlag {
			return outputJSON(topics)
		}
		unfiled, err := taskClient.ListTasks(domain.TaskFilter{Scope: scope, RepoPath: repoRoot, Topic: domain.UnfiledTopic, OmitNotes: true})
		if err != nil {
			return err
		}
		fmt.Print(renderTopicTree(topics, len(unfiled), currentTopic()))
		return nil
	},
}

// renderTopicTree draws the topic tree with kind glyphs and counts.
func renderTopicTree(topics []*domain.Topic, unfiledCount int, current string) string {
	ascii := IsASCIIOnly()
	var sb strings.Builder
	scopeDesc := "repo"
	if scope == domain.ScopeGlobal {
		scopeDesc = "global"
	}
	fmt.Fprintf(&sb, "Topics (%s)\n", scopeDesc)
	if len(topics) == 0 {
		sb.WriteString("  (none yet — create one with: tk topic new \"Hero System\")\n")
	}
	for _, tp := range topics {
		indent := strings.Repeat("  ", tp.Depth+1)
		marker := " "
		if tp.Path == current {
			marker = "*"
		}
		var counts string
		switch tp.Kind {
		case domain.TopicGroup:
			counts = fmt.Sprintf("%d sub-topic(s)", tp.ChildCount)
		case domain.TopicBoard:
			counts = fmt.Sprintf("%d open / %d posts", tp.OpenCount, tp.ItemCount)
		default:
			counts = fmt.Sprintf("%d open / %d tasks", tp.OpenCount, tp.ItemCount)
		}
		fmt.Fprintf(&sb, "%s%s%s %-*s %-6s %s\n", marker, indent, topicGlyph(tp.Kind, ascii), maxInt(1, 28-2*tp.Depth), tp.Slug, tp.Kind, counts)
	}
	if unfiledCount > 0 {
		glyph := "∅"
		if ascii {
			glyph = "0"
		}
		fmt.Fprintf(&sb, "   %s %-28s %-6s %d item(s)\n", glyph, domain.UnfiledTopic, "", unfiledCount)
	}
	return sb.String()
}

func topicGlyph(k domain.TopicKind, ascii bool) string {
	switch k {
	case domain.TopicBoard:
		if ascii {
			return "#"
		}
		return "▣"
	case domain.TopicGroup:
		if ascii {
			return "+"
		}
		return "▾"
	default:
		if ascii {
			return "-"
		}
		return "•"
	}
}

var topicShowCmd = &cobra.Command{
	Use:   "show <topic>",
	Short: "Show a topic and the items it contains",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := taskClient.GetTopic(args[0])
		if err != nil {
			return err
		}
		if jsonFlag {
			return outputJSON(tp)
		}
		fmt.Printf("@%s — %s (%s)\n", tp.Path, tp.Title, tp.Kind)
		if tp.Description != "" {
			fmt.Printf("\n%s\n", tp.Description)
		}
		backlinks, err := taskClient.ListTasks(domain.TaskFilter{Scope: scope, RepoPath: repoRoot, LinksTo: "@" + tp.Path, OmitNotes: true})
		if err == nil && len(backlinks) > 0 {
			ids := make([]string, len(backlinks))
			for i, b := range backlinks {
				ids[i] = b.ID
			}
			fmt.Printf("Linked from: %s\n", strings.Join(ids, ", "))
		}
		fmt.Println()
		listTopicRef = tp.Path
		if tp.Kind == domain.TopicGroup {
			listRecursive = true
		}
		return listCmd.RunE(cmd, nil)
	},
}

var topicRenameCmd = &cobra.Command{
	Use:   "rename <topic> <new title>",
	Short: "Rename a topic (use --slug to change its path segment too)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := strings.Join(args[1:], " ")
		p := protocol.UpdateTopicPayload{Ref: args[0], Title: &title}
		if cmd.Flags().Changed("slug") {
			p.Slug = &topicSlug
		}
		tp, err := taskClient.UpdateTopic(p)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Renamed @%s to %q\n", tp.Path, tp.Title)
		return nil
	},
}

var topicEditCmd = &cobra.Command{
	Use:   "edit <topic>",
	Short: "Change a topic's title, slug, description or kind",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := protocol.UpdateTopicPayload{Ref: args[0]}
		if cmd.Flags().Changed("title") {
			p.Title = &topicTitle
		}
		if cmd.Flags().Changed("slug") {
			p.Slug = &topicSlug
		}
		if cmd.Flags().Changed("desc") {
			p.Description = &topicDesc
		}
		if cmd.Flags().Changed("kind") {
			k, err := domain.ParseTopicKind(topicKind)
			if err != nil {
				return err
			}
			p.Kind = &k
		}
		if p.Title == nil && p.Slug == nil && p.Description == nil && p.Kind == nil {
			return fmt.Errorf("nothing to change (use --title, --slug, --desc or --kind)")
		}
		tp, err := taskClient.UpdateTopic(p)
		if err != nil {
			return err
		}
		if jsonFlag {
			return outputJSON(tp)
		}
		fmt.Printf("✓ Updated @%s\n", tp.Path)
		return nil
	},
}

var topicMvCmd = &cobra.Command{
	Use:     "mv <topic> --to <parent|root>",
	Aliases: []string{"move"},
	Short:   "Move a topic under another topic (or to the root with --to root)",
	Args:    cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		parent := topicParent
		if strings.EqualFold(domain.NormalizeTopicRef(parent), "root") {
			parent = ""
		}
		tp, err := taskClient.MoveTopic(args[0], parent)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Moved topic to @%s\n", tp.Path)
		return nil
	},
}

var topicRmCmd = &cobra.Command{
	Use:     "rm <topic>",
	Aliases: []string{"delete", "del"},
	Short:   "Delete a topic (non-empty topics need --move-to <topic|unfiled>)",
	Args:    cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		norm := domain.NormalizeTopicRef(args[0])
		if !topicRmForce && !jsonFlag {
			prompt := fmt.Sprintf("Delete topic @%s permanently? [y/N]: ", norm)
			if topicMoveTo != "" {
				prompt = fmt.Sprintf("Delete topic @%s and move contents to @%s? [y/N]: ", norm, domain.NormalizeTopicRef(topicMoveTo))
			}
			fmt.Print(prompt)
			var resp string
			_, _ = fmt.Scanln(&resp)
			resp = strings.ToLower(strings.TrimSpace(resp))
			if resp != "y" && resp != "yes" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		if err := taskClient.DeleteTopic(args[0], topicMoveTo); err != nil {
			return err
		}
		if domain.NormalizeTopicRef(args[0]) == currentTopic() {
			_ = setCurrentTopic("")
		}
		fmt.Printf("✓ Deleted topic @%s\n", norm)
		return nil
	},
}

var topicUseCmd = &cobra.Command{
	Use:   "use [topic|-]",
	Short: "Set (or with '-' clear) the default topic used by 'tk new' and 'tk post'",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if len(args) == 0 {
			if cur := currentTopic(); cur != "" {
				fmt.Printf("Current topic: @%s\n", cur)
			} else {
				fmt.Println("No default topic set (items are created unfiled unless --topic is given).")
			}
			return nil
		}
		if args[0] == "-" || domain.NormalizeTopicRef(args[0]) == "" {
			if err := setCurrentTopic(""); err != nil {
				return err
			}
			fmt.Println("✓ Cleared default topic")
			return nil
		}
		tp, err := taskClient.GetTopic(args[0])
		if err != nil {
			return err
		}
		if err := setCurrentTopic(tp.Path); err != nil {
			return err
		}
		fmt.Printf("✓ Default topic is now @%s (%s)\n", tp.Path, tp.Kind)
		return nil
	},
}

// currentTopicFile is where the sticky default topic of the current scope is stored.
func currentTopicFile() string {
	if scope == domain.ScopeGlobal || repoRoot == "" {
		if cfg == nil {
			return ""
		}
		return filepath.Join(cfg.GlobalDir, "current_topic")
	}
	return filepath.Join(repoRoot, ".tasks", "current_topic")
}

// currentTopic returns the sticky default topic path for the current scope, or "".
func currentTopic() string {
	path := currentTopicFile()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return domain.NormalizeTopicRef(string(data))
}

func setCurrentTopic(path string) error {
	file := currentTopicFile()
	if file == "" {
		return fmt.Errorf("cannot determine where to store the default topic")
	}
	if path == "" {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(path+"\n"), 0o600)
}

// defaultTopicFor returns the sticky topic if it accepts items of kind, otherwise "".
func defaultTopicFor(kind domain.ItemKind) string {
	cur := currentTopic()
	if cur == "" {
		return ""
	}
	tp, err := taskClient.GetTopic(cur)
	if err != nil || !tp.Kind.Accepts(kind) {
		return ""
	}
	return tp.Path
}

var importTagsDryRun bool

var topicImportTagsCmd = &cobra.Command{
	Use:   "import-tags [flags]",
	Short: "Migrate legacy task tags into tasks topics",
	Long: `Scans all tasks for legacy tags, creates a 'tasks' topic for each distinct tag
(if one does not exist already), and moves unfiled tasks into their corresponding topic.
If a task has multiple tags, it is moved to the topic corresponding to its first tag.`,
	Example: `  tk topic import-tags
  tk topic import-tags --dry-run`,
	RunE: func(_ *cobra.Command, _ []string) error {
		tasks, err := taskClient.ListTasks(domain.TaskFilter{
			Scope:           scope,
			RepoPath:        repoRoot,
			IncludeArchived: true,
		})
		if err != nil {
			return err
		}

		groups := make(map[string][]*domain.Task)
		var tagOrder []string
		for _, t := range tasks {
			if len(t.Tags) == 0 {
				continue
			}
			if t.TopicID != 0 {
				continue
			}
			firstTag := strings.TrimSpace(t.Tags[0])
			if firstTag == "" {
				continue
			}
			if _, exists := groups[firstTag]; !exists {
				tagOrder = append(tagOrder, firstTag)
			}
			groups[firstTag] = append(groups[firstTag], t)
		}

		if len(groups) == 0 {
			fmt.Println("No unfiled tasks with legacy tags found.")
			return nil
		}

		if importTagsDryRun {
			fmt.Printf("Dry run: %d tag group(s) would be imported:\n", len(groups))
			for _, tag := range tagOrder {
				taskList := groups[tag]
				fmt.Printf("  • @%s: %d task(s)\n", tag, len(taskList))
			}
			return nil
		}

		topicsCreated := 0
		tasksMoved := 0
		for _, tag := range tagOrder {
			taskList := groups[tag]
			tp, err := taskClient.GetTopic(tag)
			if err != nil {
				tp, err = taskClient.CreateTopic(protocol.CreateTopicPayload{
					Title: tag,
					Slug:  tag,
					Kind:  domain.TopicTasks,
				})
				if err != nil {
					return fmt.Errorf("failed to create topic for tag %q: %w", tag, err)
				}
				topicsCreated++
			}
			ids := make([]string, len(taskList))
			for i, t := range taskList {
				ids[i] = t.ID
			}
			if len(ids) > 0 {
				moved, err := taskClient.MoveItems(ids, tp.Path, cfg.Actor)
				if err != nil {
					return fmt.Errorf("failed to move items to topic @%s: %w", tp.Path, err)
				}
				tasksMoved += len(moved)
				fmt.Printf("✓ Moved %d task(s) with tag %q to @%s\n", len(moved), tag, tp.Path)
			}
		}

		fmt.Printf("\nImport complete: %d topic(s) created, %d task(s) organized.\n", topicsCreated, tasksMoved)
		return nil
	},
}

func init() {
	topicNewCmd.Flags().StringVarP(&topicKind, "kind", "k", "tasks", "Topic kind: tasks, board or group")
	topicNewCmd.Flags().StringVar(&topicParent, "parent", "", "Parent topic (path or slug)")
	topicNewCmd.Flags().StringVar(&topicSlug, "slug", "", "Path segment (default: derived from the title)")
	topicNewCmd.Flags().StringVarP(&topicDesc, "desc", "d", "", "Topic description")

	topicRenameCmd.Flags().StringVar(&topicSlug, "slug", "", "Also change the path segment")

	topicEditCmd.Flags().StringVar(&topicTitle, "title", "", "New title")
	topicEditCmd.Flags().StringVar(&topicSlug, "slug", "", "New path segment")
	topicEditCmd.Flags().StringVarP(&topicDesc, "desc", "d", "", "New description")
	topicEditCmd.Flags().StringVarP(&topicKind, "kind", "k", "", "New kind (only if its items fit)")

	topicMvCmd.Flags().StringVar(&topicParent, "to", "", "New parent topic, or 'root'")
	_ = topicMvCmd.MarkFlagRequired("to")

	topicRmCmd.Flags().StringVar(&topicMoveTo, "move-to", "", "Where to move contained items/sub-topics (topic or 'unfiled')")
	topicRmCmd.Flags().BoolVarP(&topicRmForce, "yes", "y", false, "Confirm deletion without interactive prompt")
	topicRmCmd.Flags().BoolVarP(&topicRmForce, "force", "f", false, "Confirm deletion without interactive prompt")

	topicImportTagsCmd.Flags().BoolVar(&importTagsDryRun, "dry-run", false, "Preview topics to create and tasks to move")

	topicCmd.AddCommand(topicNewCmd, topicTreeCmd, topicShowCmd, topicRenameCmd, topicEditCmd, topicMvCmd, topicRmCmd, topicUseCmd, topicImportTagsCmd)
	RootCmd.AddCommand(topicCmd)
}
