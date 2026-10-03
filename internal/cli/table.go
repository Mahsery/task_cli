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

// tableAgeWidth reserves room for the AGE column, e.g. "3 weeks ago (abc123)".
const tableAgeWidth = 20

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

	// Column widths: compact layout for <88 columns, otherwise the title column
	// absorbs the extra horizontal space (capped so very wide terminals stay readable).
	idW, statusW, priW, tagsW, titleW := 6, 12, 8, 16, 34
	if width < 88 {
		statusW, priW, tagsW, titleW = 11, 6, 10, 26
	} else {
		fixed := idW + statusW + priW + tagsW + tableAgeWidth + 5 // 5 column gaps
		if avail := width - fixed; avail > titleW {
			titleW = avail
		}
		if maxW := domain.MaxTitleLength + 3; titleW > maxW {
			titleW = maxW
		}
	}
	sepWidth := 74
	if width >= 88 {
		sepWidth = idW + statusW + priW + titleW + tagsW + 5 + tableAgeWidth
		if sepWidth > width {
			sepWidth = width
		}
	}

	fmt.Fprintf(&sb, "%s %s %s %s %s %s\n",
		padRight("ID", idW),
		padRight("STATUS", statusW),
		padRight("PRI", priW),
		padRight("TITLE", titleW),
		padRight("TAGS", tagsW),
		"AGE")
	sb.WriteString(strings.Repeat(sepChar, sepWidth) + "\n")
	for _, t := range tasks {
		rawTitle := t.Title
		if t.IsOrphaned() {
			if IsASCIIOnly() {
				rawTitle = "[orphan] " + rawTitle
			} else {
				rawTitle += " 👻"
			}
		}
		title := truncateWithEllipsis(rawTitle, titleW)
		tags := truncateWithEllipsis(strings.Join(t.Tags, ","), tagsW)
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
		fmt.Fprintf(&sb, "%s %s %s %s %s %s\n",
			padRight(t.ID, idW),
			padRight(status, statusW),
			padRight(string(t.Priority), priW),
			padRight(title, titleW),
			padRight(tags, tagsW),
			age)
	}
	_ = PrintWithPager(sb.String())
}
