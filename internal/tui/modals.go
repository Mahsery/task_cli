package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderConfirmationModal(dlg *ConfirmDialog, termWidth, termHeight int) string {
	theme := m.theme
	boxWidth := 56
	if termWidth > 0 && termWidth-6 < boxWidth {
		boxWidth = termWidth - 6
	}
	if boxWidth < 32 {
		boxWidth = 32
	}

	contentWidth := boxWidth - 4 // interior padding

	var lines []string
	// Title
	titleStr := theme.ModalTitle.Render(" " + dlg.Title + " ")
	lines = append(lines, titleStr, "")

	// Prompt
	promptWrapped := wrapText(dlg.Prompt, contentWidth, "")
	for _, pw := range promptWrapped {
		lines = append(lines, theme.ModalPrompt.Render(pw))
	}

	// Details: one truncated line per entry, capped so the modal always fits on screen.
	if len(dlg.Details) > 0 {
		lines = append(lines, "")
		maxDetails := 8
		if termHeight > 0 {
			if fit := termHeight - 14; fit < maxDetails {
				maxDetails = fit
			}
		}
		if maxDetails < 1 {
			maxDetails = 1
		}
		shown := dlg.Details
		if len(shown) > maxDetails {
			shown = shown[:maxDetails]
		}
		for _, det := range shown {
			lines = append(lines, theme.ModalDesc.Render(truncateToWidth("• "+det, contentWidth)))
		}
		if extra := len(dlg.Details) - len(shown); extra > 0 {
			lines = append(lines, theme.ModalDesc.Render(fmt.Sprintf("  … and %d more", extra)))
		}
	}

	// Actions footer
	lines = append(lines, "")
	confirmKey := theme.ModalKey.Render("[y / Enter]") + " " + theme.ModalPrompt.Render("Confirm")
	if dlg.Kind == ConfirmQuit {
		confirmKey = theme.ModalKey.Render("[q / y / Enter]") + " " + theme.ModalPrompt.Render("Quit")
	}
	cancelKey := theme.ModalCancel.Render("[n / Esc]") + " " + theme.ModalPrompt.Render("Cancel")
	actions := fmt.Sprintf("%s    %s", confirmKey, cancelKey)
	lines = append(lines, actions)

	inner := strings.Join(lines, "\n")
	return theme.ModalBox.Width(boxWidth).Render(inner)
}

func (m Model) linkCandidateItems() []pickerItem {
	var exclude map[string]bool
	if src := m.lookupItem(m.linkTaskID); src != nil {
		exclude = make(map[string]bool, len(src.DependsOn)+1)
		exclude[m.linkTaskID] = true
		for _, depID := range src.DependsOn {
			exclude[depID] = true
		}
	} else {
		exclude = map[string]bool{m.linkTaskID: true}
	}
	q := strings.TrimSpace(m.linkInput)
	var out []pickerItem
	for _, t := range m.tasks {
		if t == nil || t.IsPost() || exclude[t.ID] {
			continue
		}
		if _, ok := fuzzyScore(q, t.ID+" "+t.Title); ok {
			out = append(out, pickerItem{Label: t.ID, Detail: t.Title, Value: t.ID})
		}
	}
	return out
}

func (m Model) renderLinkModal(termWidth, termHeight int) string {
	theme := m.theme
	boxWidth := 60
	if termWidth > 0 && termWidth-6 < boxWidth {
		boxWidth = termWidth - 6
	}
	if boxWidth < 36 {
		boxWidth = 36
	}

	contentWidth := boxWidth - 4
	var lines []string

	titleStr := theme.ModalTitle.Render(" LINK PREREQUISITE DEPENDENCY ")
	lines = append(lines, titleStr, "")

	promptStr := fmt.Sprintf("Make task %s depend on prerequisite:", m.linkTaskID)
	for _, pw := range wrapText(promptStr, contentWidth, "") {
		lines = append(lines, theme.ModalPrompt.Render(pw))
	}
	lines = append(lines, "")

	inputStr := fmt.Sprintf("  Prereq Task ID: %s", m.inputViewOrText(m.linkInput))
	lines = append(lines, theme.SearchPrompt.Render(inputStr), "")

	candidates := m.linkCandidateItems()
	if len(candidates) > 0 {
		maxRows := max(min(6, termHeight-16), 2)
		idx := min(max(m.linkCandidateIdx, 0), len(candidates)-1)
		start := 0
		if idx >= maxRows {
			start = idx - maxRows + 1
		}
		for i := start; i < min(start+maxRows, len(candidates)); i++ {
			c := candidates[i]
			row := truncateToWidth(fmt.Sprintf("%s  %s", c.Label, c.Detail), contentWidth-2)
			if i == idx {
				lines = append(lines, theme.RowSelected.Render("► "+row))
			} else {
				lines = append(lines, theme.RowNormal.Render("  "+row))
			}
		}
		lines = append(lines, "")
	}

	hint := fmt.Sprintf("E.g. enter T-1 or use ↑/↓ to require prerequisite before %s can be closed.", m.linkTaskID)
	for _, hw := range wrapText(hint, contentWidth, "") {
		lines = append(lines, theme.ModalDesc.Render(hw))
	}
	lines = append(lines, "")

	confirmKey := theme.ModalKey.Render("[Enter]") + " " + theme.ModalPrompt.Render("Link")
	cancelKey := theme.ModalCancel.Render("[Esc]") + " " + theme.ModalPrompt.Render("Cancel")
	lines = append(lines, fmt.Sprintf("%s    %s", confirmKey, cancelKey))

	inner := strings.Join(lines, "\n")
	return theme.ModalBox.Width(boxWidth).Render(inner)
}

func (m Model) renderReplyModal(termWidth, _ int) string {
	theme := m.theme
	boxWidth := 64
	if termWidth > 0 && termWidth-6 < boxWidth {
		boxWidth = termWidth - 6
	}
	if boxWidth < 36 {
		boxWidth = 36
	}
	contentWidth := boxWidth - 4

	lines := []string{
		theme.ModalTitle.Render(fmt.Sprintf(" ADD REPLY / NOTE TO %s ", m.replyTaskID)),
		"",
		theme.SearchPrompt.Render(fmt.Sprintf("  > %s", m.inputViewOrText(m.replyInput))),
		"",
	}
	hint := "Supports [[T-12]], [[T-12#L5]], and [[@topic]] wikilinks."
	for _, hw := range wrapText(hint, contentWidth, "") {
		lines = append(lines, theme.ModalDesc.Render(hw))
	}
	lines = append(lines, "")
	confirmKey := theme.ModalKey.Render("[Enter]") + " " + theme.ModalPrompt.Render("Post Reply")
	cancelKey := theme.ModalCancel.Render("[Esc]") + " " + theme.ModalPrompt.Render("Cancel")
	lines = append(lines, fmt.Sprintf("%s    %s", confirmKey, cancelKey))
	return theme.ModalBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
}

func (m Model) renderHelpModal(termWidth, termHeight int) string {
	theme := m.theme
	boxWidth := min(max(termWidth-6, 44), 76)
	contentWidth := boxWidth - 4

	rows := []string{
		theme.ModalTitle.Render(" KEYBINDING CHEATSHEET (?) "),
		"",
		theme.DetailLabel.Render("Navigation & Layout"),
		"  j / k, ↑ / ↓       Move cursor / scroll detail pane",
		"  J / K, Ctrl+d/u    Half-page down / up",
		"  gg / G             Jump to top / bottom (supports count prefix)",
		"  Tab / Shift+Tab    Cycle active pane (Topics ↔ Tasks ↔ Details)",
		"  t                  Toggle dependency DAG Tree View mode",
		"  B                  Toggle Topics sidebar / narrow-screen drawer",
		"  < / > / =          Resize or reset split pane divider",
		"",
		theme.DetailLabel.Render("Task & Post Actions"),
		"  n / e              Create new item / Edit item in $EDITOR",
		"  R / C              Inline reply / comment modal (with [[wikilinks]])",
		"  d                  Mark DONE / Resolve (or reopen completed item)",
		"  s / a              Toggle In-Progress / Claim task",
		"  L / U              Link prerequisite (fuzzy) / Unlink prerequisite",
		"  M / p              Move item(s) to topic / Promote post to task",
		"  A / Ctrl+a         Archive or unarchive / Toggle archive visibility",
		"  Space / v / V      Multi-select item / Toggle all / u to Undo",
		"",
		theme.DetailLabel.Render("Topics, Search & Wikilinks"),
		"  /                  Search & DSL filter (branch:, ext:, pr:, topic:, s:)",
		"  O / S              Cycle kind (All/Tasks/Posts) / Toggle Local/Global",
		"  gt / ]t / [t       Fuzzy jump to topic / Next or previous topic",
		"  gl / ]l / [l / gf  Insert [[link]] / Cycle links / Follow link (gb back)",
		"",
		theme.ModalKey.Render("[? / Esc / Enter]") + " " + theme.ModalPrompt.Render("Close Help"),
	}

	maxLines := max(termHeight-6, 10)
	if len(rows) > maxLines {
		rows = append(rows[:maxLines-1], rows[len(rows)-1])
	}
	for i := range rows {
		rows[i] = truncateToWidth(rows[i], contentWidth)
	}
	return theme.ModalBox.Width(boxWidth).Render(strings.Join(rows, "\n"))
}

func (m Model) renderNarrowSidebarModal(termWidth, termHeight int) string {
	theme := m.theme
	boxWidth := min(max(termWidth-6, 36), 56)
	contentWidth := boxWidth - 4

	title := " TOPICS DRAWER "
	if m.scope == "global" {
		title = " TOPICS DRAWER · GLOBAL "
	}
	lines := []string{theme.ModalTitle.Render(title), ""}

	entries := m.sidebarEntries()
	maxRows := max(min(12, termHeight-12), 4)
	start := 0
	if m.sidebarCursor >= maxRows {
		start = m.sidebarCursor - maxRows + 1
	}
	end := min(start+maxRows, len(entries))
	for i := start; i < end; i++ {
		e := entries[i]
		collapsed := e.Topic != nil && m.collapsed[e.Path]
		text := strings.Repeat("  ", e.Depth) + topicGlyph(e, collapsed) + " " + e.Label
		if e.Topic != nil && e.Topic.OpenCount > 0 {
			text += fmt.Sprintf(" (%d)", e.Topic.OpenCount)
		}
		row := truncateToWidth(text, contentWidth-2)
		if i == m.sidebarCursor {
			lines = append(lines, theme.RowSelected.Render("► "+row))
		} else {
			lines = append(lines, theme.RowNormal.Render("  "+row))
		}
	}
	lines = append(lines, "", theme.ModalDesc.Render(truncateToWidth("j/k nav · Enter select · N new · R rename · D del · B/Esc close", contentWidth)))
	return theme.ModalBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
}

func (m Model) overlayModal(bg string, dlg *ConfirmDialog) string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}

	modalStr := m.renderConfirmationModal(dlg, width, height)
	return m.overlayModalString(bg, modalStr)
}

func (m Model) overlayModalString(bg, modalStr string) string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}

	modalLines := strings.Split(modalStr, "\n")
	bgLines := strings.Split(bg, "\n")

	modalHeight := len(modalLines)
	if modalHeight == 0 {
		return bg
	}

	// Ensure bgLines has at least height lines, preserving the footer on the bottom-most line
	if len(bgLines) < height {
		footer := ""
		if len(bgLines) > 0 {
			footer = bgLines[len(bgLines)-1]
			bgLines = bgLines[:len(bgLines)-1]
		}
		for len(bgLines) < height-1 {
			bgLines = append(bgLines, strings.Repeat(" ", width))
		}
		bgLines = append(bgLines, footer)
	}

	startY := (len(bgLines) - modalHeight) / 2
	if startY < 2 {
		startY = 2
	}
	if startY+modalHeight >= len(bgLines) {
		startY = len(bgLines) - modalHeight - 1
		if startY < 0 {
			startY = 0
		}
	}

	for i, mLine := range modalLines {
		targetY := startY + i
		centeredLine := lipgloss.PlaceHorizontal(width, lipgloss.Center, mLine)
		if targetY < len(bgLines) {
			bgLines[targetY] = centeredLine
		} else {
			bgLines = append(bgLines, centeredLine)
		}
	}

	return strings.Join(bgLines, "\n")
}
