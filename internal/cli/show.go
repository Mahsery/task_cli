package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var showLineNumbers bool

func init() {
	showCmd.Flags().BoolVarP(&showLineNumbers, "lines", "n", false, "Number description lines (useful for [[ID#L12]] anchors)")
}

// annotateLinks appends a reading-order index after every [[wiki link]] in text
// ("[[T-3]]" -> "[[T-3]][1]"), continuing from start. It returns the new text and last index.
func annotateLinks(text string, start int, scope string) (string, int) {
	links := domain.ParseWikiLinks(text, scope)
	if len(links) == 0 {
		return text, start
	}
	var sb strings.Builder
	prev := 0
	for _, l := range links {
		start++
		sb.WriteString(text[prev:l.End])
		fmt.Fprintf(&sb, "[%d]", start)
		prev = l.End
	}
	sb.WriteString(text[prev:])
	return sb.String(), start
}

// numberLines prefixes each line with its 1-based number.
func numberLines(text string) string {
	lines := strings.Split(text, "\n")
	width := len(fmt.Sprint(len(lines)))
	for i, l := range lines {
		lines[i] = fmt.Sprintf("%*d │ %s", width, i+1, l)
	}
	return strings.Join(lines, "\n")
}

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show task details, notes, and attached git commit/diff",
	Long: `Display comprehensive details for a task, including metadata, tags,
chronological activity notes, and attached Linux kernel-style git commit SHA,
commit message, diffstat, and unified patch diff.`,
	Example: `  tk show T-1
  tk -g show G-3
  tk show t1              # IDs are forgiving: t1, T1, t-1, 1
  tk show T-1 -n          # number description lines (for [[T-1#L3]] anchors)
  tk show T-1 --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		taskID := normID(args[0])
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
		fmt.Fprintf(&sb, "Status:   %-18s Priority: %-18s Scope: %s\n", strings.ToUpper(task.StatusLabel()), strings.ToUpper(string(task.Priority)), task.Scope)
		kind := task.Kind
		if kind == "" {
			kind = domain.KindTask
		}
		fmt.Fprintf(&sb, "Kind:     %-18s Topic:    %s\n", kind, topicLabel(task))
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

		linkN := 0
		if task.Description != "" {
			desc, n := annotateLinks(task.Description, linkN, task.Scope)
			linkN = n
			if showLineNumbers {
				desc = numberLines(desc)
			}
			sb.WriteString(desc + "\n\n")
		}

		if len(task.Notes) > 0 {
			sb.WriteString("── Activity & Notes " + strings.Repeat("─", 60) + "\n")
			for i, note := range task.Notes {
				content, n := annotateLinks(note.Content, linkN, task.Scope)
				linkN = n
				fmt.Fprintf(&sb, "#n%d [%s] %s:\n%s\n\n", i+1, note.CreatedAt.Format("2006-01-02 15:04"), note.Author, content)
			}
		}

		if linkN > 0 || len(task.Backlinks) > 0 {
			sb.WriteString("── Links " + strings.Repeat("─", 71) + "\n")
			sb.WriteString(renderLinksSection(task, collectLinks(task)))
			if linkN > 0 {
				fmt.Fprintf(&sb, "  (follow with: tk go %s <n>)\n", task.ID)
			}
			sb.WriteString("\n")
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
