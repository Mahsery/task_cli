package cli

import (
	"fmt"
	"os"
	"strings"

	"task_cli/internal/domain"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"
)

func padRight(s string, targetWidth int) string {
	w := runewidth.StringWidth(s)
	if w >= targetWidth {
		return s
	}
	return s + strings.Repeat(" ", targetWidth-w)
}

func renderTaskTable(tasks []*domain.Task) {
	width := 88
	fd := os.Stdout.Fd()
	if term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			width = w
		}
	}

	var sb strings.Builder
	sepChar := "─"
	if IsASCIIOnly() {
		sepChar = "-"
	}

	if width < 88 {
		// Responsive layout for 80-column screens (74 cols total)
		sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s\n",
			padRight("ID", 6),
			padRight("STATUS", 11),
			padRight("PRI", 6),
			padRight("TITLE", 26),
			padRight("TAGS", 10),
			"AGE",
		))
		sb.WriteString(strings.Repeat(sepChar, 74) + "\n")
		for _, t := range tasks {
			rawTitle := t.Title
			if t.IsOrphaned() {
				if IsASCIIOnly() {
					rawTitle = "[orphan] " + rawTitle
				} else {
					rawTitle = rawTitle + " 👻"
				}
			}
			title := truncateWithEllipsis(rawTitle, 26)
			tags := truncateWithEllipsis(strings.Join(t.Tags, ","), 10)
			age := formatAge(t.CreatedAt)
			if t.CommitSHA != "" {
				sha := t.CommitSHA
				if len(sha) > 6 {
					sha = sha[:6]
				}
				age = fmt.Sprintf("%s (%s)", age, sha)
			}
			status := string(t.Status)
			if t.IsOrphaned() {
				status = "orphan"
			}
			sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s\n",
				padRight(t.ID, 6),
				padRight(status, 11),
				padRight(string(t.Priority), 6),
				padRight(title, 26),
				padRight(tags, 10),
				age,
			))
		}
		_ = PrintWithPager(sb.String())
		return
	}

	// Standard wide layout (88 cols total)
	sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s\n",
		padRight("ID", 6),
		padRight("STATUS", 12),
		padRight("PRI", 8),
		padRight("TITLE", 34),
		padRight("TAGS", 16),
		"AGE",
	))
	sb.WriteString(strings.Repeat(sepChar, 88) + "\n")
	for _, t := range tasks {
		rawTitle := t.Title
		if t.IsOrphaned() {
			if IsASCIIOnly() {
				rawTitle = "[orphan] " + rawTitle
			} else {
				rawTitle = rawTitle + " 👻"
			}
		}
		title := truncateWithEllipsis(rawTitle, 34)
		tags := truncateWithEllipsis(strings.Join(t.Tags, ","), 16)
		age := formatAge(t.CreatedAt)
		if t.CommitSHA != "" {
			sha := t.CommitSHA
			if len(sha) > 6 {
				sha = sha[:6]
			}
			age = fmt.Sprintf("%s (%s)", age, sha)
		}
		status := string(t.Status)
		if t.IsOrphaned() {
			status = "orphan"
		}
		sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s\n",
			padRight(t.ID, 6),
			padRight(status, 12),
			padRight(string(t.Priority), 8),
			padRight(title, 34),
			padRight(tags, 16),
			age,
		))
	}
	_ = PrintWithPager(sb.String())
}
