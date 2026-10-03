package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) getDetailContentLines(detailWidth int) []string {
	if len(m.tasks) == 0 || m.cursor >= len(m.tasks) {
		return nil
	}
	t := m.currentTask()
	if t == nil {
		return nil
	}
	theme := m.theme
	var contentLines []string

	// Latest update badge
	var latestBadge string
	if len(t.Notes) > 0 {
		lastNote := t.Notes[len(t.Notes)-1]
		author := lastNote.Author
		if author == "" {
			author = "actor"
		}
		latestBadge = fmt.Sprintf(" Latest Note: %s by %s", timeAgo(lastNote.CreatedAt), author)
	} else if t.CommitSHA != "" {
		latestBadge = fmt.Sprintf(" Resolved by %s (%s)", t.CommitSHA, timeAgo(t.UpdatedAt))
	} else if !t.UpdatedAt.IsZero() && t.UpdatedAt != t.CreatedAt {
		latestBadge = fmt.Sprintf(" Updated: %s", timeAgo(t.UpdatedAt))
	} else {
		latestBadge = fmt.Sprintf(" Created: %s", timeAgo(t.CreatedAt))
	}
	latestBadge = truncateToWidth(latestBadge, detailWidth)
	contentLines = append(contentLines, theme.NoteTime.Width(detailWidth).Render(latestBadge), strings.Repeat(" ", detailWidth))

	// Task Title
	for _, tl := range wrapText(t.ID+": "+t.Title, detailWidth-4, "  ") {
		contentLines = append(contentLines, theme.DetailTitle.Width(detailWidth).Render(tl))
	}

	// Metadata badges
	statusDisplay := string(t.Status)
	statusStyle := StatusStyle(t.Status)
	if t.IsOrphaned() {
		statusDisplay = fmt.Sprintf("%s 👻 (ORPHANED)", string(t.Status))
		statusStyle = theme.CountOrphaned
	}
	meta1 := fmt.Sprintf("  %s %s   %s %s   %s %s",
		theme.DetailLabel.Render("Status:"), statusStyle.Render(statusDisplay),
		theme.DetailLabel.Render("Pri:"), PriorityStyle(t.Priority).Render(string(t.Priority)),
		theme.DetailLabel.Render("Scope:"), theme.TaskID.Render(t.Scope),
	)
	var metaParts []string
	if t.Assignee != "" {
		assigneeDisplay := t.Assignee
		if t.IsOrphaned() {
			assigneeDisplay += " [STALE / TIMED OUT]"
		}
		metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Assignee:"), theme.NoteHeader.Render(assigneeDisplay)))
	}
	if len(t.Tags) > 0 {
		metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Tags:"), theme.Tag.Render(strings.Join(t.Tags, ", "))))
	}
	fullMeta := meta1
	if len(metaParts) > 0 {
		fullMeta += "   " + strings.Join(metaParts, "   ")
	}
	if lipgloss.Width(fullMeta) <= detailWidth {
		contentLines = append(contentLines, fullMeta)
	} else {
		contentLines = append(contentLines, meta1)
		if len(metaParts) > 0 {
			contentLines = append(contentLines, wrapText(strings.Join(metaParts, "   "), detailWidth-4, "  ")...)
		}
	}
	contentLines = append(contentLines, strings.Repeat(" ", detailWidth))

	// Dependencies & Blocked Status
	if t.IsBlocked {
		contentLines = append(contentLines, theme.DiffSub.Render("  🔒 BLOCKED: Must complete unfinished prerequisites first:"))
		for _, b := range t.BlockedBy {
			contentLines = append(contentLines, theme.DiffSub.Render(fmt.Sprintf("    • %s", b)))
		}
		contentLines = append(contentLines, strings.Repeat(" ", detailWidth))
	}
	if len(t.DependsOn) > 0 {
		depText := "Depends on: " + strings.Join(t.DependsOn, ", ")
		if t.IsBlocked {
			depText += " 🔒 [BLOCKED]"
		} else {
			depText += " ⚡ [READY]"
		}
		contentLines = append(contentLines, theme.DetailLabel.Render("  "+depText))
	}
	if len(t.Blocks) > 0 {
		blkText := "Blocks: " + strings.Join(t.Blocks, ", ")
		contentLines = append(contentLines, theme.DetailLabel.Render("  "+blkText))
	}
	if len(t.DependsOn) > 0 || len(t.Blocks) > 0 {
		contentLines = append(contentLines, strings.Repeat(" ", detailWidth))
	}

	// Description
	if t.Description != "" {
		descLines := wrapText(t.Description, detailWidth-4, "  ")
		for _, dl := range descLines {
			contentLines = append(contentLines, theme.DetailDesc.Width(detailWidth).Render(dl))
		}
	}

	// Commit resolution & diff
	if t.CommitSHA != "" {
		contentLines = append(contentLines, strings.Repeat(" ", detailWidth))
		resText := fmt.Sprintf("✓ Resolved by commit %s: %s", t.CommitSHA, t.CommitMsg)
		for _, rl := range wrapText(resText, detailWidth-4, "  ") {
			contentLines = append(contentLines, theme.DetailResolved.Width(detailWidth).Render(rl))
		}
		if t.DiffStat != "" {
			for _, sl := range strings.Split(strings.TrimSpace(t.DiffStat), "\n") {
				contentLines = append(contentLines, theme.DiffStat.Width(detailWidth).Render("    "+strings.TrimSpace(sl)))
			}
		}
		if m.showDiff && t.DiffContent != "" {
			contentLines = append(contentLines, theme.Border.Width(detailWidth).Render("  ── Diff Content ──"))
			diffLines := strings.Split(t.DiffContent, "\n")
			for _, dl := range diffLines {
				if strings.HasPrefix(dl, "+") {
					contentLines = append(contentLines, theme.DiffAdd.Width(detailWidth).Render("    "+dl))
				} else if strings.HasPrefix(dl, "-") {
					contentLines = append(contentLines, theme.DiffSub.Width(detailWidth).Render("    "+dl))
				} else if strings.HasPrefix(dl, "@@") {
					contentLines = append(contentLines, theme.DiffHunk.Width(detailWidth).Render("    "+dl))
				} else {
					contentLines = append(contentLines, theme.DetailDesc.Width(detailWidth).Render("    "+dl))
				}
			}
		}
	}

	// Activity Notes
	if len(t.Notes) > 0 {
		contentLines = append(contentLines, strings.Repeat(" ", detailWidth), theme.NoteHeader.Width(detailWidth).Render(fmt.Sprintf("  • Notes & Messages (%d):", len(t.Notes))))
		for _, n := range t.Notes {
			timeStr := theme.NoteTime.Render(timeAgo(n.CreatedAt))
			authorStr := ""
			if n.Author != "" {
				availAuthor := detailWidth - 18
				author := truncateToWidth(n.Author, availAuthor)
				authorStr = " by " + theme.NoteAuthor.Render(author)
			}
			contentLines = append(contentLines, fmt.Sprintf("    • %s%s:", timeStr, authorStr))
			for _, line := range wrapText(n.Content, detailWidth-6, "      ") {
				contentLines = append(contentLines, theme.NoteContent.Width(detailWidth).Render(line))
			}
		}
	}

	// Flatten any wrapped entries and clamp to the pane width so len() equals the rendered
	// line count; scroll math depends on this.
	return fitLines(contentLines, detailWidth)
}
