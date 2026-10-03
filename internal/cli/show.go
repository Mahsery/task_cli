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
	RunE: func(_ *cobra.Command, args []string) error {
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
		fmt.Fprintf(&sb, "%s: %s\n", task.ID, task.Title)
		fmt.Fprintf(&sb, "Status:   %-18s Priority: %-18s Scope: %s\n", strings.ToUpper(string(task.Status)), strings.ToUpper(string(task.Priority)), task.Scope)
		if len(task.Tags) > 0 {
			fmt.Fprintf(&sb, "Tags:     [%s]\n", strings.Join(task.Tags, ", "))
		}
		if task.Author != "" {
			fmt.Fprintf(&sb, "Author:   %s\n", task.Author)
		}
		if task.Assignee != "" {
			fmt.Fprintf(&sb, "Assignee: %s\n", task.Assignee)
		}
		if len(task.DependsOn) > 0 {
			blockedText := ""
			if task.IsBlocked {
				blockedText = fmt.Sprintf(" 🔒 [BLOCKED by %s]", strings.Join(task.BlockedBy, ", "))
			} else {
				blockedText = " ⚡ [READY - prerequisites completed]"
			}
			fmt.Fprintf(&sb, "Depends On: [%s]%s\n", strings.Join(task.DependsOn, ", "), blockedText)
		}
		if len(task.Blocks) > 0 {
			fmt.Fprintf(&sb, "Blocks:     [%s]\n", strings.Join(task.Blocks, ", "))
		}
		fmt.Fprintf(&sb, "Created:  %s", task.CreatedAt.Format("2006-01-02 15:04:05"))
		if task.ClosedAt != nil {
			fmt.Fprintf(&sb, "   Closed: %s", task.ClosedAt.Format("2006-01-02 15:04:05"))
		}
		sb.WriteString("\n")
		sb.WriteString(strings.Repeat("─", 80) + "\n")

		if task.IsBlocked {
			sb.WriteString("⚠️  TASK IS BLOCKED: Unfinished prerequisites must be completed first:\n")
			for _, b := range task.BlockedBy {
				fmt.Fprintf(&sb, "   • %s\n", b)
			}
			sb.WriteString("   (Run 'tk show <id>' to inspect blockers, or 'tk done --force' to override)\n\n")
		}

		if task.Description != "" {
			sb.WriteString(task.Description + "\n\n")
		}

		if len(task.Notes) > 0 {
			sb.WriteString("── Activity & Notes " + strings.Repeat("─", 60) + "\n")
			for _, note := range task.Notes {
				fmt.Fprintf(&sb, "[%s] %s:\n%s\n\n", note.CreatedAt.Format("2006-01-02 15:04"), note.Author, note.Content)
			}
		}

		if task.CommitSHA != "" {
			sb.WriteString("── Commits & Changes " + strings.Repeat("─", 59) + "\n")
			fmt.Fprintf(&sb, "commit %s\n", task.CommitSHA)
			if task.CommitMsg != "" {
				fmt.Fprintf(&sb, "Subject: %s\n\n", task.CommitMsg)
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
