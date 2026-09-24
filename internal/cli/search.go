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
  - Boolean composition: 'AND', 'OR', 'NOT' (e.g. 'bug AND odin', 'fix NOT closed')
  - Implicit AND: multiple terms 'bug odin' match tasks containing both
  - Tag filtering: 'tag:bug', 't:odin'
  - Field qualifiers: 'status:open' ('s:open'), 'priority:high' ('p:high'), 'assignee:mehmet' ('a:mehmet'), 'id:T-1'
  - Negations: '-wip' or 'NOT wip'
  - Exact quoted phrases: '"memory leak"'`,
	Example: `  tk search bug AND odin
  tk search "memory leak"
  tk search tag:security priority:high
  tk search bug NOT closed
  tk search s:open p:urgent
  tk search -g odin --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		if searchStatus != "" {
			s, err := domain.ParseStatus(searchStatus)
			if err != nil {
				return err
			}
			filter.Status = &s
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
			for _, t := range tasks {
				fmt.Printf("%-6s [%-11s] %s\n", t.ID, t.Status, t.Title)
			}
			return nil
		}

		// Aligned tabular display
		fmt.Printf("%-6s %-12s %-8s %-34s %-16s %s\n", "ID", "STATUS", "PRI", "TITLE", "TAGS", "AGE")
		fmt.Println(strings.Repeat("─", 88))
		for _, t := range tasks {
			title := t.Title
			if len(title) > 32 {
				title = title[:30] + ".."
			}
			tags := strings.Join(t.Tags, ",")
			if len(tags) > 14 {
				tags = tags[:12] + ".."
			}
			age := formatAge(t.CreatedAt)
			if t.CommitSHA != "" {
				sha := t.CommitSHA
				if len(sha) > 6 {
					sha = sha[:6]
				}
				age = fmt.Sprintf("%s (%s)", age, sha)
			}
			fmt.Printf("%-6s %-12s %-8s %-34s %-16s %s\n",
				t.ID, string(t.Status), string(t.Priority), title, tags, age,
			)
		}

		return nil
	},
}

func init() {
	searchCmd.Flags().StringVarP(&searchStatus, "status", "s", "", "Filter by status: open, in_progress, done, closed")
	searchCmd.Flags().StringVarP(&searchPriority, "priority", "p", "", "Filter by priority: low, normal, high, urgent")
	searchCmd.Flags().StringVarP(&searchTag, "tag", "l", "", "Filter by exact tag")
	searchCmd.Flags().StringVarP(&searchAssignee, "assignee", "a", "", "Filter by assignee")
	searchCmd.Flags().StringVar(&searchSort, "sort", "", "Sort order: 'status,priority,id' (default), 'id', 'recent', 'created', 'updated'")
	searchCmd.Flags().BoolVar(&searchShort, "short", false, "Print compact one-line output")
	searchCmd.Flags().IntVar(&searchLimit, "limit", 0, "Limit number of results")
}
