package tui

import tea "github.com/charmbracelet/bubbletea"

const listFirstRowY = 1 + listChromeHeight // header line + column header + rule

// paneAt reports which pane contains screen cell (x, y) under the current layout.
func (m Model) paneAt(l paneLayout, x, y int) Pane {
	if l.split {
		if x < l.listWidth {
			return PaneList
		}
		return PaneDetail
	}
	if y < 1+l.listHeight {
		return PaneList
	}
	return PaneDetail
}

// moveCursor moves the list cursor by delta, keeping it visible and resetting the detail viewport.
func (m *Model) moveCursor(delta int) {
	displayCount := len(m.tasks)
	if m.treeViewMode {
		displayCount = len(m.getTreeRows())
	}
	next := min(max(m.cursor+delta, 0), max(displayCount-1, 0))
	if next == m.cursor {
		return
	}
	m.cursor = next
	m.detailScrollOffset = 0
	m.clampScroll()
}

// handleMouse processes mouse wheel, click and split-drag events in both layouts.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	l := m.layout()

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			// Click on or near the vertical divider starts a resize drag (split mode only).
			if l.split && msg.X >= l.listWidth-1 && msg.X <= l.listWidth+1 {
				m.isDraggingSplit = true
				return m, nil
			}
			if m.paneAt(l, msg.X, msg.Y) == PaneDetail {
				m.activePane = PaneDetail
				return m, nil
			}
			m.activePane = PaneList
			if row := msg.Y - listFirstRowY; row >= 0 && row < l.rows {
				m.moveCursor(m.scrollOffset + row - m.cursor)
			}
			return m, nil
		}

	case tea.MouseActionMotion:
		if m.isDraggingSplit && l.split {
			basePercent := 48
			if m.treeViewMode {
				basePercent = 56
			}
			baseTableWidth := l.width * basePercent / 100
			target := min(max(msg.X, minSplitListWidth), l.width-minSplitDetail-splitDividerWidth)
			m.splitOffset = target - baseTableWidth
			return m, nil
		}

	case tea.MouseActionRelease:
		m.isDraggingSplit = false
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if m.paneAt(l, msg.X, msg.Y) == PaneList {
			m.moveCursor(-1)
		} else if m.detailScrollOffset > 0 {
			m.detailScrollOffset--
		}
	case tea.MouseButtonWheelDown:
		if m.paneAt(l, msg.X, msg.Y) == PaneList {
			m.moveCursor(1)
		} else if m.detailScrollOffset < m.maxDetailScrollOffset() {
			m.detailScrollOffset++
		}
	}
	return m, nil
}
