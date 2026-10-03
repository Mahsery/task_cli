package cli

import (
	"fmt"
	"strings"
	"time"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var (
	listStatus    string
	listPriority  string
	listTag       string
	listTagLabel  string
	listAssignee  string
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
)

var listCmd = &cobra.Command{
	Use:     "list [flags]",
	Aliases: []string{"ls"},
	Short:   "List tasks (formatted table, one-liners, or JSON)",
	Long: `List tasks matching the specified status, priority, tag, assignee, or text query.
Renders an aligned tabular view by default, or single-line summaries with --short.`,
	Example: `  tk list
  tk ls -s open -p high
  tk list -s open,in_progress
  tk list -t backend -q "jwt"
  tk ls -l backend --short
  tk list -a alice
  tk ls --orphaned
  tk ls --short --limit 10
  tk -g list --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if listTree {
			return treeCmd.RunE(cmd, args)
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
			Search:          listSearch,
			Sort:            listSort,
			Limit:           listLimit,
			OnlyReady:       listReady,
			OnlyBlocked:     listBlocked,
			OnlyOrphaned:    listOrphaned,
			OnlyArchived:    listArchived,
			IncludeArchived: listArchived,
			DependsOn:       listDependsOn,
			Blocks:          listBlocks,
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
				dispStatus := string(t.Status)
				if t.IsOrphaned() {
					dispStatus = "orphan"
				}
				sb.WriteString(fmt.Sprintf("%-6s [%-11s] %s\n", t.ID, dispStatus, t.Title))
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
	listCmd.Flags().StringVarP(&listSearch, "search", "q", "", "Search query DSL (supports AND, OR, NOT, tags, qualifiers)")
	listCmd.Flags().StringVar(&listSort, "sort", "", "Sort order: 'status,priority,id' (default), 'id', 'recent', 'created', 'updated'")
	listCmd.Flags().BoolVar(&listShort, "short", false, "Output simple one-line list")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Limit number of results")
	listCmd.Flags().BoolVar(&listTree, "tree", false, "Display tasks in a hierarchical dependency tree view")
	listCmd.Flags().BoolVar(&listReady, "ready", false, "Show only actionable tasks whose prerequisites are complete")
	listCmd.Flags().BoolVar(&listBlocked, "blocked", false, "Show only tasks blocked by unfinished prerequisites")
	listCmd.Flags().StringVar(&listDependsOn, "after", "", "Show tasks that depend on the specified task ID")
	listCmd.Flags().StringVar(&listDependsOn, "depends-on", "", "Alias for --after")
	listCmd.Flags().StringVar(&listBlocks, "blocks", "", "Show tasks that are prerequisite blockers for the specified task ID")
	listCmd.Flags().BoolVar(&listOrphaned, "orphaned", false, "Show only tasks marked as orphaned")
	listCmd.Flags().BoolVar(&listArchived, "archived", false, "Show archived tasks")
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
