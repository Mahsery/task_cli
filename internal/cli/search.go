package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var (
	searchStatus   string
	searchPriority string
	searchTag      string
	searchAssignee string
	searchSort     string
	searchShort    bool
	searchLimit    int
)

var searchCmd = &cobra.Command{
	Use:     "search [query...]",
	Aliases: []string{"find"},
	Short:   "Search tasks using query DSL (AND, OR, NOT, tags, fields)",
	Long: `Search tasks using a zero-flag query DSL inspired by grx.

Supports:
  - Boolean composition: 'AND', 'OR', 'NOT' (e.g. 'bug AND backend', 'fix NOT closed')
  - Implicit AND: multiple terms 'bug engine' match tasks containing both
  - Topic qualifiers: 'topic:backend', 'in:backend/api'
  - Kind qualifiers: 'kind:task', 'type:post'
  - Link qualifiers: 'links:T-1', 'linkedfrom:T-2'
  - Field qualifiers: 'status:open' ('s:open'), 'priority:high' ('p:high'), 'assignee:mehmet' ('a:mehmet'), 'id:T-1'
  - Legacy tag filtering: 'tag:bug', 't:core'
  - Negations: '-wip' or 'NOT wip'
  - Exact quoted phrases: '"memory leak"'`,
	Example: `  tk search bug AND backend
  tk search "memory leak"
  tk search topic:backend priority:high
  tk search kind:post NOT closed
  tk search links:T-1
  tk search s:open p:urgent
  tk search -g backend --json`,
	RunE: func(_ *cobra.Command, args []string) error {
		query := strings.Join(args, " ")

		filter := domain.TaskFilter{
			Scope:    scope,
			RepoPath: repoRoot,
			Tag:      searchTag,
			Assignee: searchAssignee,
			Search:   query,
			Sort:     searchSort,
			Limit:    searchLimit,
		}

		if err := applyStatusFilter(&filter, searchStatus); err != nil {
			return err
		}
		if searchPriority != "" {
			p, err := domain.ParsePriority(searchPriority)
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
			if query != "" {
				fmt.Printf("No tasks matching query %q.\n", query)
			} else {
				fmt.Println("No tasks found.")
			}
			return nil
		}

		if searchShort {
			var sb strings.Builder
			for _, t := range tasks {
				fmt.Fprintf(&sb, "%-6s [%-11s] %s\n", t.ID, t.Status, t.Title)
			}
			return PrintWithPager(sb.String())
		}

		renderTaskTable(tasks)
		return nil
	},
}

func init() {
	searchCmd.Flags().StringVarP(&searchStatus, "status", "s", "", "Filter by status, comma-separated for multiple (open, in_progress, done, closed, orphan)")
	searchCmd.Flags().StringVarP(&searchPriority, "priority", "p", "", "Filter by priority: low, normal, high, urgent")
	searchCmd.Flags().StringVarP(&searchTag, "tag", "l", "", "Filter by exact tag")
	searchCmd.Flags().StringVarP(&searchAssignee, "assignee", "a", "", "Filter by assignee")
	searchCmd.Flags().StringVar(&searchSort, "sort", "", "Sort order: 'status,priority,id' (default), 'id', 'recent', 'created', 'updated'")
	searchCmd.Flags().BoolVar(&searchShort, "short", false, "Print compact one-line output")
	searchCmd.Flags().IntVar(&searchLimit, "limit", 0, "Limit number of results")
}
