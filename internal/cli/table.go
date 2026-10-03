package cli

import (
	"fmt"
	"os"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/termutil"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/term"
)

var truncateWithEllipsis = termutil.Truncate

func renderTaskTable(tasks []*domain.Task) {
	width := 88
	fd := os.Stdout.Fd()
	if term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			width = w
		}
	}

	titleW := width - 54
	if titleW < 24 {
		titleW = 24
	} else if titleW > domain.MaxTitleLength+3 {
		titleW = domain.MaxTitleLength + 3
	}

	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rawTitle := t.Title
		if t.IsOrphaned() {
			if IsASCIIOnly() {
				rawTitle = "[orphan] " + rawTitle
			} else {
				rawTitle += " 👻"
			}
		}
		title := termutil.Truncate(rawTitle, titleW)
		tags := termutil.Truncate(strings.Join(t.Tags, ","), 16)
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
		rows = append(rows, []string{
			t.ID,
			status,
			string(t.Priority),
			title,
			tags,
			age,
		})
	}

	border := lipgloss.NormalBorder()
	if IsASCIIOnly() {
		border = lipgloss.Border{Bottom: "-"}
	}

	tbl := table.New().
		Border(border).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderHeader(true).
		StyleFunc(func(row, _ int) lipgloss.Style {
			s := lipgloss.NewStyle().PaddingRight(1)
			if row == table.HeaderRow {
				return s.Bold(true)
			}
			return s
		}).
		Headers("ID", "STATUS", "PRI", "TITLE", "TAGS", "AGE").
		Rows(rows...)

	_ = PrintWithPager(tbl.Render() + "\n")
}
