package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"task_cli/internal/domain"
)

func (m Model) renderFooter(width int) string {
	theme := m.theme
	if dlg := m.activeConfirmDialog(); dlg != nil {
		warnText := fmt.Sprintf(" ⚠️  %s  Press [y/Enter] to confirm, [n/Esc] to cancel ", dlg.Prompt)
		if width < len(warnText) {
			switch dlg.Kind {
			case ConfirmDelete:
				if dlg.IsBatch {
					warnText = fmt.Sprintf(" Delete %d tasks? [y/N] ", len(dlg.TargetIDs))
				} else if len(dlg.TargetIDs) > 0 {
					warnText = fmt.Sprintf(" Delete %s? [y/N] ", dlg.TargetIDs[0])
				}
			case ConfirmDone, ConfirmForceDone:
				if dlg.IsBatch {
					warnText = fmt.Sprintf(" Done %d tasks? [y/N] ", len(dlg.TargetIDs))
				} else if len(dlg.TargetIDs) > 0 {
					warnText = fmt.Sprintf(" Done %s? [y/N] ", dlg.TargetIDs[0])
				}
			case ConfirmClaim:
				if dlg.IsBatch {
					warnText = fmt.Sprintf(" Claim %d tasks? [y/N] ", len(dlg.TargetIDs))
				} else if len(dlg.TargetIDs) > 0 {
					warnText = fmt.Sprintf(" Claim %s? [y/N] ", dlg.TargetIDs[0])
				}
			default:
				warnText = fmt.Sprintf(" %s [y/N] ", dlg.Prompt)
			}
		}
		return theme.FooterBg.Width(width).Render(theme.DiffSub.Render(warnText))
	}

	key := func(k, action string) string {
		return theme.FooterKey.Render("["+k+"]") + " " + theme.FooterDesc.Render(action)
	}

	chordBadge := ""
	if m.chordBuffer != "" {
		chordBadge = theme.BatchBadge.Render(fmt.Sprintf("Chord: %s", m.chordBuffer))
	}

	undoKey := ""
	if len(m.undoStack) > 0 {
		undoKey = theme.ModalKey.Render("[u] Undo")
	}

	maxContentWidth := width - 2
	if maxContentWidth < 10 {
		maxContentWidth = width
	}

	var prefixParts []string
	if chordBadge != "" {
		prefixParts = append(prefixParts, chordBadge)
	}
	if m.statusMsg != "" {
		if strings.HasPrefix(m.statusMsg, "Error:") {
			// Errors take priority over the key legend so they are actually readable.
			maxErrLen := max(maxContentWidth-lipgloss.Width(strings.Join(prefixParts, " "))-6, 10)
			prefixParts = append(prefixParts, theme.DiffSub.Render("["+truncateToWidth(m.statusMsg, maxErrLen)+"]"))
		} else {
			maxStatusLen := min(max(width/3, 28), 48)
			if width < 80 {
				maxStatusLen = 18
			}
			prefixParts = append(prefixParts, theme.FooterStatus.Render("["+truncateToWidth(m.statusMsg, maxStatusLen)+"]"))
		}
	}

	prefix := ""
	if len(prefixParts) > 0 {
		prefix = strings.Join(prefixParts, " ") + "  "
	}

	var candidateKeys []string

	if len(m.selected) > 0 {
		selCount := len(m.selected)
		selWord := "sel"
		if width >= 100 {
			selWord = "selected"
		}
		selBadge := theme.BatchBadge.Render(fmt.Sprintf("%d %s", selCount, selWord))
		candidateKeys = []string{
			selBadge,
		}
		if undoKey != "" {
			candidateKeys = append(candidateKeys, undoKey)
		}
		candidateKeys = append(candidateKeys,
			theme.CountDone.Render(fmt.Sprintf("[d] Done(%d)", selCount)),
			theme.CountProg.Render(fmt.Sprintf("[s] Prog(%d)", selCount)),
			theme.CountOpen.Render(fmt.Sprintf("[a] Claim(%d)", selCount)),
			theme.CountArchived.Render(fmt.Sprintf("[A] Arc(%d)", selCount)),
			theme.DiffSub.Render(fmt.Sprintf("[x] Del(%d)", selCount)),
			key("M", fmt.Sprintf("Move(%d)", selCount)),
			key("Space", "Sel"),
			theme.FilterIndicator.Render("[Esc] Clear"),
			theme.Age.Render("[q] Quit"),
		)
	} else if m.activePane == PaneSidebar {
		sideBadge := theme.BatchBadge.Render("TOPICS")
		candidateKeys = []string{
			sideBadge,
			key("j/k", "Nav"),
			key("Enter", "Select"),
			key("Tab", "Tasks"),
			key("O", "Kind"),
			key("N", "New"),
			key("R", "Rename"),
			key("D", "Delete"),
			key("M", "Move"),
			key("Space", "Fold"),
			theme.Age.Render("[q] Quit"),
		}
	} else if m.activePane == PaneDetail {
		curTask := m.currentTask()
		doneLabel := theme.CountProg.Render("[d] Done")
		statusLabel := theme.CountOpen.Render("[s] Prog")
		archiveLabel := theme.CountArchived.Render("[A] Arc")
		if curTask != nil {
			if curTask.IsPost() {
				if curTask.Status == domain.StatusDone || curTask.Status == domain.StatusClosed {
					doneLabel = theme.CountDone.Render("[d] Reopen")
				} else {
					doneLabel = theme.CountProg.Render("[d] Resolve")
				}
			} else {
				if curTask.Status == domain.StatusDone || curTask.Status == domain.StatusClosed {
					doneLabel = theme.CountDone.Render("[d] Reopen")
				}
				if curTask.Status == domain.StatusInProgress {
					statusLabel = theme.CountOpen.Render("[s] Open")
				}
			}
			if curTask.IsArchived() {
				archiveLabel = theme.CountArchived.Render("[A] Unarc")
			}
		}
		detBadge := theme.BatchBadge.Render("DET")
		if width >= 120 {
			detBadge = theme.BatchBadge.Render("DETAILS PANE")
		}
		candidateKeys = []string{
			detBadge,
			key("j/k", "Scroll"),
			key("Tab", "Tasks"),
		}
		if undoKey != "" {
			candidateKeys = append(candidateKeys, undoKey)
		}
		if m.isSplitView() {
			candidateKeys = append(candidateKeys, key("<>", "Split"))
		}
		candidateKeys = append(candidateKeys,
			key("O", "Kind"),
			doneLabel,
		)
		if curTask == nil || !curTask.IsPost() {
			candidateKeys = append(candidateKeys, statusLabel)
		} else {
			candidateKeys = append(candidateKeys, key("p", "Promote"))
		}
		candidateKeys = append(candidateKeys,
			key("Enter", "Follow/Diff"),
			key("[]l", "Link"),
			key("gf", "Follow"),
			key("gl", "Insert"),
			key("gb", "Back"),
			archiveLabel,
			theme.DiffSub.Render("[x] Del"),
			key("L", "Dep"),
		)
		if curTask != nil && len(curTask.DependsOn) > 0 {
			candidateKeys = append(candidateKeys, key("U", "Unlink"))
		}
		candidateKeys = append(candidateKeys,
			theme.Age.Render("[q] Quit"),
		)
	} else {
		// List Pane (Normal mode)
		curTask := m.currentTask()
		doneLabel := theme.CountProg.Render("[d] Done")
		statusLabel := theme.CountOpen.Render("[s] Prog")
		archiveLabel := theme.CountArchived.Render("[A] Arc")
		if curTask != nil {
			if curTask.IsPost() {
				if curTask.Status == domain.StatusDone || curTask.Status == domain.StatusClosed {
					doneLabel = theme.CountDone.Render("[d] Reopen")
				} else {
					doneLabel = theme.CountProg.Render("[d] Resolve")
				}
			} else {
				if curTask.Status == domain.StatusDone || curTask.Status == domain.StatusClosed {
					doneLabel = theme.CountDone.Render("[d] Reopen")
				}
				if curTask.Status == domain.StatusInProgress {
					statusLabel = theme.CountOpen.Render("[s] Open")
				}
			}
			if curTask.IsArchived() {
				archiveLabel = theme.CountArchived.Render("[A] Unarc")
			}
		}

		treeLabel := key("t", "Tree")
		if m.treeViewMode {
			treeLabel = theme.ModalKey.Render("[t] List")
		}

		isPost := curTask != nil && curTask.IsPost()

		if width >= 120 {
			candidateKeys = []string{
				key("j/k", "Nav"),
				key("Tab", "Det"),
			}
			if undoKey != "" {
				candidateKeys = append(candidateKeys, undoKey)
			}
			if m.isSplitView() {
				candidateKeys = append(candidateKeys, key("<>", "Split"))
			}
			candidateKeys = append(candidateKeys,
				treeLabel,
				key("gt", "Topic"),
				key("O", "Kind"),
				key("M", "Move"),
				key("gl", "Link"),
				key("Space", "Sel"),
				key("n", "New"),
				key("e", "Edit"),
			)
			if isPost {
				candidateKeys = append(candidateKeys, key("p", "Promote"))
			} else {
				candidateKeys = append(candidateKeys, key("a", "Claim"))
			}
			candidateKeys = append(candidateKeys, doneLabel)
			if !isPost {
				candidateKeys = append(candidateKeys, statusLabel)
			}
			candidateKeys = append(candidateKeys,
				archiveLabel,
				theme.DiffSub.Render("[x] Del"),
				key("/", "Find"),
				theme.Age.Render("[q] Quit"),
			)
		} else if width >= 80 {
			candidateKeys = []string{
				key("j/k", "Nav"),
				key("Tab", "Det"),
			}
			if undoKey != "" {
				candidateKeys = append(candidateKeys, undoKey)
			}
			if m.isSplitView() {
				candidateKeys = append(candidateKeys, key("<>", "Split"))
			}
			candidateKeys = append(candidateKeys,
				key("gt", "Topic"),
				key("O", "Kind"),
				key("M", "Move"),
				key("Space", "Sel"),
				key("n", "New"),
				key("e", "Edit"),
			)
			if isPost {
				candidateKeys = append(candidateKeys, key("p", "Promote"))
			} else {
				candidateKeys = append(candidateKeys, key("a", "Claim"))
			}
			candidateKeys = append(candidateKeys, doneLabel)
			if !isPost {
				candidateKeys = append(candidateKeys, statusLabel)
			}
			candidateKeys = append(candidateKeys,
				archiveLabel,
				theme.DiffSub.Render("[x] Del"),
				key("/", "Find"),
				theme.Age.Render("[q] Quit"),
			)
		} else {
			// Ultra-compact footer for narrow screens (< 80 cols)
			candidateKeys = []string{
				key("j/k", "Nav"),
			}
			if isPost {
				candidateKeys = append(candidateKeys, key("p", "Promote"))
			} else {
				candidateKeys = append(candidateKeys, key("a", "Claim"))
			}
			if undoKey != "" {
				candidateKeys = append(candidateKeys, undoKey)
			}
			candidateKeys = append(candidateKeys,
				key("O", "Kind"),
				doneLabel,
				archiveLabel,
				theme.DiffSub.Render("[x] Del"),
				key("/", "Find"),
				theme.Age.Render("[q]"),
			)
		}
	}

	currentWidth := lipgloss.Width(prefix)
	var activeKeys []string
	for _, k := range candidateKeys {
		kW := lipgloss.Width(k)
		sepW := 0
		if len(activeKeys) > 0 {
			sepW = 2
		}
		if currentWidth+sepW+kW > maxContentWidth {
			break
		}
		activeKeys = append(activeKeys, k)
		currentWidth += sepW + kW
	}

	line := prefix + strings.Join(activeKeys, "  ")
	if lipgloss.Width(line) > maxContentWidth {
		line = truncateToWidth(line, maxContentWidth)
	}
	rendered := theme.FooterBg.Width(width).Render(line)
	if strings.Contains(rendered, "\n") {
		rendered = strings.Split(rendered, "\n")[0]
	}
	return rendered
}
