package cli

import (
	"fmt"
	"strings"
	"time"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var (
	listStatus   string
	listPriority string
	listTag      string
	listSearch   string
	listShort    bool
	listLimit    int
)

var listCmd = &cobra.Command{
	Use:     "list [flags]",
	Aliases: []string{"ls"},
	Short:   "List tasks (formatted table, one-liners, or JSON)",
	RunE: func(cmd *cobra.Command, args []string) error {
		filter := domain.TaskFilter{
			Scope:    scope,
			RepoPath: repoRoot,
			Tag:      listTag,
			Search:   listSearch,
			Limit:    listLimit,
		}

		if listStatus != "" {
			s, err := domain.ParseStatus(listStatus)
			if err != nil {
				return err
			}
			filter.Status = &s
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
			return outputJSON(tasks)
		}

		if len(tasks) == 0 {
			fmt.Println("No tasks found.")
			return nil
		}

		if listShort {
			for _, t := range tasks {
				fmt.Printf("%-6s [%-8s] %s\n", t.ID, t.Status, t.Title)
			}
			return nil
		}

		// Clean formatted table
		fmt.Printf("%-6s %-10s %-8s %-34s %-16s %s\n", "ID", "STATUS", "PRI", "TITLE", "TAGS", "AGE")
		fmt.Println(strings.Repeat("─", 84))
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
			fmt.Printf("%-6s %-10s %-8s %-34s %-16s %s\n",
				t.ID, string(t.Status), string(t.Priority), title, tags, age,
			)
		}

		return nil
	},
}

func init() {
	listCmd.Flags().StringVarP(&listStatus, "status", "s", "", "Filter by status (open, in_progress, done, closed)")
	listCmd.Flags().StringVarP(&listPriority, "priority", "p", "", "Filter by priority (low, normal, high, urgent)")
	listCmd.Flags().StringVarP(&listTag, "tag", "t", "", "Filter by tag")
	listCmd.Flags().StringVarP(&listSearch, "search", "q", "", "Search query in title or description")
	listCmd.Flags().BoolVar(&listShort, "short", false, "Output simple one-line list")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Limit number of results")
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
