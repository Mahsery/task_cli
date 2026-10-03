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
	cancelKey := theme.ModalCancel.Render("[n / Esc]") + " " + theme.ModalPrompt.Render("Cancel")
	actions := fmt.Sprintf("%s    %s", confirmKey, cancelKey)
	lines = append(lines, actions)

	inner := strings.Join(lines, "\n")
	return theme.ModalBox.Width(boxWidth).Render(inner)
}

func (m Model) renderLinkModal(termWidth, _ int) string {
	theme := m.theme
	boxWidth := 56
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

	inputStr := fmt.Sprintf("  Prereq Task ID: %s_", m.linkInput)
	lines = append(lines, theme.SearchPrompt.Render(inputStr), "")

	hint := fmt.Sprintf("E.g. enter T-1 to require T-1 before %s can be closed.", m.linkTaskID)
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

func (m Model) renderTagModal(termWidth, _ int) string {
	theme := m.theme
	boxWidth := 56
	if termWidth > 0 && termWidth-6 < boxWidth {
		boxWidth = termWidth - 6
	}
	if boxWidth < 36 {
		boxWidth = 36
	}

	contentWidth := boxWidth - 4
	var lines []string

	titleStr := theme.ModalTitle.Render(" QUICK TAG TASK ")
	lines = append(lines, titleStr, "")

	var promptStr string
	if m.tagTaskID != "" {
		promptStr = fmt.Sprintf("Add or toggle tag for task %s:", m.tagTaskID)
	} else {
		promptStr = fmt.Sprintf("Add tag for %d selected tasks:", len(m.selected))
	}
	for _, pw := range wrapText(promptStr, contentWidth, "") {
		lines = append(lines, theme.ModalPrompt.Render(pw))
	}
	lines = append(lines, "")

	inputStr := fmt.Sprintf("  Tag: %s_", m.tagInput)
	lines = append(lines, theme.SearchPrompt.Render(inputStr), "")

	hint := "Type tag name to add (or toggle off if already present)."
	for _, hw := range wrapText(hint, contentWidth, "") {
		lines = append(lines, theme.ModalDesc.Render(hw))
	}
	lines = append(lines, "")

	confirmKey := theme.ModalKey.Render("[Enter]") + " " + theme.ModalPrompt.Render("Apply")
	cancelKey := theme.ModalCancel.Render("[Esc]") + " " + theme.ModalPrompt.Render("Cancel")
	lines = append(lines, fmt.Sprintf("%s    %s", confirmKey, cancelKey))

	inner := strings.Join(lines, "\n")
	return theme.ModalBox.Width(boxWidth).Render(inner)
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
