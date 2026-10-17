package cli

import (
	"fmt"
	"strings"
	"time"

	"task_cli/internal/domain"
	"task_cli/internal/termutil"

	"github.com/spf13/cobra"
)

var (
	listStatus    string
	listPriority  string
	listTag       string
	listTagLabel  string
	listAssignee  string
	listReviewer  string
	listSearch    string
	listShort     bool
	listLimit     int
	listTree      bool
	listReady     bool
	listBlocked   bool
	listDependsOn string
	listBlocks    string
	listOrphaned  bool
	listArchived  bool
	listTopicRef  string
	listRecursive bool
	listKind      string
	listPosts     bool
	listTasks     bool
	listBranch    string
	listIDsOnly   bool
)

var listCmd = &cobra.Command{
	Use:     "list [@topic] [flags]",
	Aliases: []string{"ls"},
	Short:   "List tasks and posts (formatted table, one-liners, IDs, or JSON)",
	Long: `List tasks and posts matching the specified topic, status, priority, assignee, reviewer, or text query.
Renders an aligned tabular view by default, single-line summaries with --short, or bare IDs with --ids.
A positional @topic (or --topic) limits the list to one topic; add -r to include sub-topics.`,
	Example: `  tk list
  tk ls @hero-system
  tk ls @game -r
  tk ls -T bug-reports --posts
  tk ls -T unfiled
  tk ls -s open -p high
  tk list -s open,in_progress
  tk list @backend -q "jwt"
  tk ls @backend --short
  tk ls -s open --ids
  tk list -a alice
  tk list -R bob
  tk ls --orphaned
  tk ls --short --limit 10
  tk -g list --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("tag") || cmd.Flags().Changed("label") {
			return fmt.Errorf("tags were replaced by topics; use -T / --topic / 'tk topic'")
		}
		for _, a := range args {
			if !strings.HasPrefix(a, "@") {
				return fmt.Errorf("unexpected argument %q (topics are written as @topic)", a)
			}
			listTopicRef = a
		}
		if listTree {
			treeTopic = listTopicRef
			return treeCmd.RunE(cmd, nil)
		}
		if listPosts && listTasks {
			return fmt.Errorf("cannot specify both --posts and --tasks")
		}
		if listPosts && listKind != "" && listKind != string(domain.KindPost) {
			return fmt.Errorf("conflicting options: --posts and --kind %s", listKind)
		}
		if listTasks && listKind != "" && listKind != string(domain.KindTask) {
			return fmt.Errorf("conflicting options: --tasks and --kind %s", listKind)
		}
		defer func() {
			listPosts = false
			listTasks = false
		}()
		kind, err := domain.ParseItemKind(listKind)
		if err != nil {
			return err
		}
		if listPosts {
			kind = domain.KindPost
		} else if listTasks {
			kind = domain.KindTask
		} else if listKind == "" {
			kind = ""
		}

		tagFilter := listTag
		if tagFilter == "" && listTagLabel != "" {
			tagFilter = listTagLabel
		}

		filter := domain.TaskFilter{
			Scope:           scope,
			RepoPath:        repoRoot,
			Tag:             tagFilter,
			Assignee:        listAssignee,
			Reviewer:        listReviewer,
			Search:          listSearch,
			Sort:            listSort,
			Limit:           listLimit,
			OnlyReady:       listReady,
			OnlyBlocked:     listBlocked,
			OnlyOrphaned:    listOrphaned,
			OnlyArchived:    listArchived,
			IncludeArchived: listArchived,
			DependsOn:       normIDIfSet(listDependsOn),
			Blocks:          normIDIfSet(listBlocks),
			Topic:           listTopicRef,
			TopicRecursive:  listRecursive,
			Kind:            kind,
			Branch:          listBranch,
		}

		if err := applyStatusFilter(&filter, listStatus); err != nil {
			return err
		}
		if listPriority != "" {
			p, err := domain.ParsePriority(listPriority)
			if err != nil {
				return err
			}
			filter.Priority = &p
		}

		tasks, err := taskClient.ListTasks(filter)
		if err != nil {
			return err
		}

		if listIDsOnly {
			for _, t := range tasks {
				fmt.Println(t.ID)
			}
			return nil
		}

		if jsonFlag {
			if tasks == nil {
				tasks = []*domain.Task{}
			}
			return outputJSON(tasks)
		}

		if len(tasks) == 0 {
			scopeDesc := "local git repository"
			if scope == "global" {
				scopeDesc = "global scope"
			}
			if listArchived {
				fmt.Printf("No archived tasks found in %s.\n", scopeDesc)
				return nil
			}
			if listTopicRef != "" {
				fmt.Printf("No matching items in @%s.\n", domain.NormalizeTopicRef(listTopicRef))
				return nil
			}
			fmt.Printf("No tasks found in %s.\n\n", scopeDesc)
			fmt.Println("Get started:")
			fmt.Println("  tk new -t \"Task title\"    Create your first task")
			fmt.Println("  tk                        Launch interactive dashboard")
			if scope == "local" {
				fmt.Println("  tk -g list                View global tasks")
			}
			return nil
		}

		if listShort {
			var sb strings.Builder
			for _, t := range tasks {
				dispStatus := t.StatusLabel()
				if t.IsOrphaned() {
					dispStatus = "orphan"
				}
				fmt.Fprintf(&sb, "%-6s [%-11s] %s\n", t.ID, dispStatus, t.Title)
			}
			return PrintWithPager(sb.String())
		}

		renderTaskTable(tasks)
		return nil
	},
}

var listSort string

// applyStatusFilter parses the -s/--status flag value, which may be a single
// status or a comma-separated list (e.g. "open,in_progress"), into filter.
// A lone "orphan" keeps its dedicated semantics (OnlyOrphaned); when mixed with
// concrete statuses (e.g. "open,orphan") the result is the union of tasks in
// those statuses and orphaned tasks, which the storage layer resolves.
func applyStatusFilter(filter *domain.TaskFilter, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	statuses, err := domain.ParseStatusList(raw)
	if err != nil {
		return err
	}
	if len(statuses) == 1 && statuses[0] == domain.StatusOrphan {
		filter.OnlyOrphaned = true
		return nil
	}
	filter.Statuses = statuses
	return nil
}

func init() {
	listCmd.Flags().StringVarP(&listStatus, "status", "s", "", "Filter by status, comma-separated for multiple (open, in_progress, done, closed, orphan)")
	listCmd.Flags().StringVarP(&listPriority, "priority", "p", "", "Filter by priority (low, normal, high, urgent)")
	listCmd.Flags().StringVarP(&listTag, "tag", "t", "", "Filter by tag")
	listCmd.Flags().StringVarP(&listTagLabel, "label", "l", "", "Filter by tag (alias for -t, --tag)")
	listCmd.Flags().StringVarP(&listAssignee, "assignee", "a", "", "Filter by assignee")
	listCmd.Flags().StringVarP(&listReviewer, "reviewer", "R", "", "Filter by reviewer")
	listCmd.Flags().StringVarP(&listSearch, "search", "q", "", "Search query DSL (supports AND, OR, NOT, tags, qualifiers)")
	listCmd.Flags().StringVar(&listSort, "sort", "", "Sort order: 'status,priority,id' (default), 'id', 'recent', 'created', 'updated'")
	listCmd.Flags().BoolVar(&listShort, "short", false, "Output simple one-line list")
	listCmd.Flags().BoolVar(&listIDsOnly, "ids", false, "Output only item IDs (one per line)")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Limit number of results")
	listCmd.Flags().BoolVar(&listTree, "tree", false, "Display tasks in a hierarchical dependency tree view")
	listCmd.Flags().BoolVar(&listReady, "ready", false, "Show only actionable tasks whose prerequisites are complete")
	listCmd.Flags().BoolVar(&listBlocked, "blocked", false, "Show only tasks blocked by unfinished prerequisites")
	listCmd.Flags().StringVar(&listDependsOn, "after", "", "Show tasks that depend on the specified task ID")
	listCmd.Flags().StringVar(&listDependsOn, "depends-on", "", "Alias for --after")
	listCmd.Flags().StringVar(&listBlocks, "blocks", "", "Show tasks that are prerequisite blockers for the specified task ID")
	listCmd.Flags().BoolVar(&listOrphaned, "orphaned", false, "Show only tasks marked as orphaned")
	listCmd.Flags().BoolVar(&listArchived, "archived", false, "Show archived tasks")
	listCmd.Flags().StringVarP(&listTopicRef, "topic", "T", "", "Only items in this topic (path, slug or 'unfiled')")
	listCmd.Flags().BoolVarP(&listRecursive, "recursive", "r", false, "With --topic, include items in sub-topics")
	listCmd.Flags().StringVarP(&listKind, "kind", "k", "", "Only items of this kind (task, post)")
	listCmd.Flags().BoolVar(&listPosts, "posts", false, "Only posts (same as --kind post)")
	listCmd.Flags().BoolVar(&listTasks, "tasks", false, "Only tasks (same as --kind task)")
	listCmd.Flags().StringVar(&listBranch, "branch", "", "Only tasks bound to this git branch")
	_ = listCmd.Flags().MarkHidden("tag")
	_ = listCmd.Flags().MarkHidden("label")

	postsCmd.Flags().AddFlagSet(listCmd.Flags())
	tasksCmd.Flags().AddFlagSet(listCmd.Flags())
	RootCmd.AddCommand(postsCmd, tasksCmd)
}

var postsCmd = &cobra.Command{
	Use:   "posts [@topic] [flags]",
	Short: "List board posts (shortcut for tk list --posts)",
	Example: `  tk posts
  tk posts @bugs
  tk posts --short`,
	RunE: func(cmd *cobra.Command, args []string) error {
		listPosts = true
		listTasks = false
		for i, a := range args {
			if !strings.HasPrefix(a, "@") {
				args[i] = "@" + a
			}
		}
		return listCmd.RunE(cmd, args)
	},
}

var tasksCmd = &cobra.Command{
	Use:   "tasks [@topic] [flags]",
	Short: "List execution tasks (shortcut for tk list --tasks)",
	Example: `  tk tasks
  tk tasks @hero-system
  tk tasks --short`,
	RunE: func(cmd *cobra.Command, args []string) error {
		listTasks = true
		listPosts = false
		for i, a := range args {
			if !strings.HasPrefix(a, "@") {
				args[i] = "@" + a
			}
		}
		return listCmd.RunE(cmd, args)
	},
}

func formatAge(t time.Time) string {
	return termutil.FormatAge(t)
}
