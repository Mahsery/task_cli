package cli

import (
	"fmt"
	"os"
	"strings"

	"task_cli/internal/domain"

	"github.com/charmbracelet/x/term"
)

func renderTaskTable(tasks []*domain.Task) {
	width := 88
	fd := os.Stdout.Fd()
	if term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			width = w
		}
	}

	if width < 88 {
		// Responsive layout for 80-column screens (74 cols total)
		fmt.Printf("%-6s %-11s %-6s %-26s %-10s %s\n", "ID", "STATUS", "PRI", "TITLE", "TAGS", "AGE")
		fmt.Println(strings.Repeat("─", 74))
		for _, t := range tasks {
			title := t.Title
			if len(title) > 24 {
				title = title[:22] + ".."
			}
			tags := strings.Join(t.Tags, ",")
			if len(tags) > 8 {
				tags = tags[:6] + ".."
			}
			age := formatAge(t.CreatedAt)
			if t.CommitSHA != "" {
				sha := t.CommitSHA
				if len(sha) > 6 {
					sha = sha[:6]
				}
				age = fmt.Sprintf("%s (%s)", age, sha)
			}
			fmt.Printf("%-6s %-11s %-6s %-26s %-10s %s\n",
				t.ID, string(t.Status), string(t.Priority), title, tags, age,
			)
		}
		return
	}

	// Standard wide layout (88 cols total)
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
}
