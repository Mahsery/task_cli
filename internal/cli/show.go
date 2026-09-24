package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show task details, notes, and attached git commit/diff",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		task, err := taskClient.GetTask(taskID)
		if err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(task)
		}

		fmt.Println(strings.Repeat("─", 80))
		fmt.Printf("%s: %s\n", task.ID, task.Title)
		fmt.Printf("Status:   %-18s Priority: %-18s Scope: %s\n", strings.ToUpper(string(task.Status)), strings.ToUpper(string(task.Priority)), task.Scope)
		if len(task.Tags) > 0 {
			fmt.Printf("Tags:     [%s]\n", strings.Join(task.Tags, ", "))
		}
		if task.Author != "" {
			fmt.Printf("Author:   %s\n", task.Author)
		}
		if task.Assignee != "" {
			fmt.Printf("Assignee: %s\n", task.Assignee)
		}
		fmt.Printf("Created:  %s", task.CreatedAt.Format("2006-01-02 15:04:05"))
		if task.ClosedAt != nil {
			fmt.Printf("   Closed: %s", task.ClosedAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Println()
		fmt.Println(strings.Repeat("─", 80))

		if task.Description != "" {
			fmt.Println(task.Description)
			fmt.Println()
		}

		if len(task.Notes) > 0 {
			fmt.Println("── Activity & Notes " + strings.Repeat("─", 60))
			for _, note := range task.Notes {
				fmt.Printf("[%s] %s:\n%s\n\n", note.CreatedAt.Format("2006-01-02 15:04"), note.Author, note.Content)
			}
		}

		if task.CommitSHA != "" {
			fmt.Println("── Commits & Changes " + strings.Repeat("─", 59))
			fmt.Printf("commit %s\n", task.CommitSHA)
			if task.CommitMsg != "" {
				fmt.Printf("Subject: %s\n\n", task.CommitMsg)
			}
			if task.DiffStat != "" {
				fmt.Println(task.DiffStat)
				fmt.Println()
			}
			if task.DiffContent != "" {
				fmt.Println(task.DiffContent)
			}
		}

		return nil
	},
}
