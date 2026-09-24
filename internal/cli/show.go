package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show task details, notes, and attached git commit/diff",
	Long: `Display comprehensive details for a task, including metadata, tags,
chronological activity notes, and attached Linux kernel-style git commit SHA,
commit message, diffstat, and unified patch diff.`,
	Example: `  tk show T-1
  tk -g show G-3
  tk show T-1 --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		task, err := taskClient.GetTask(taskID)
		if err != nil {
			return fmt.Errorf("%w (run 'tk list' to view available tasks)", err)
		}

		if jsonFlag {
			return outputJSON(task)
		}

		var sb strings.Builder
		sb.WriteString(strings.Repeat("─", 80) + "\n")
		sb.WriteString(fmt.Sprintf("%s: %s\n", task.ID, task.Title))
		sb.WriteString(fmt.Sprintf("Status:   %-18s Priority: %-18s Scope: %s\n", strings.ToUpper(string(task.Status)), strings.ToUpper(string(task.Priority)), task.Scope))
		if len(task.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("Tags:     [%s]\n", strings.Join(task.Tags, ", ")))
		}
		if task.Author != "" {
			sb.WriteString(fmt.Sprintf("Author:   %s\n", task.Author))
		}
		if task.Assignee != "" {
			sb.WriteString(fmt.Sprintf("Assignee: %s\n", task.Assignee))
		}
		sb.WriteString(fmt.Sprintf("Created:  %s", task.CreatedAt.Format("2006-01-02 15:04:05")))
		if task.ClosedAt != nil {
			sb.WriteString(fmt.Sprintf("   Closed: %s", task.ClosedAt.Format("2006-01-02 15:04:05")))
		}
		sb.WriteString("\n")
		sb.WriteString(strings.Repeat("─", 80) + "\n")

		if task.Description != "" {
			sb.WriteString(task.Description + "\n\n")
		}

		if len(task.Notes) > 0 {
			sb.WriteString("── Activity & Notes " + strings.Repeat("─", 60) + "\n")
			for _, note := range task.Notes {
				sb.WriteString(fmt.Sprintf("[%s] %s:\n%s\n\n", note.CreatedAt.Format("2006-01-02 15:04"), note.Author, note.Content))
			}
		}

		if task.CommitSHA != "" {
			sb.WriteString("── Commits & Changes " + strings.Repeat("─", 59) + "\n")
			sb.WriteString(fmt.Sprintf("commit %s\n", task.CommitSHA))
			if task.CommitMsg != "" {
				sb.WriteString(fmt.Sprintf("Subject: %s\n\n", task.CommitMsg))
			}
			if task.DiffStat != "" {
				sb.WriteString(task.DiffStat + "\n\n")
			}
			if task.DiffContent != "" {
				sb.WriteString(task.DiffContent + "\n")
			}
		}

		return PrintWithPager(sb.String())
	},
}
