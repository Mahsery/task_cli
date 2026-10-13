package tui

import tea "github.com/charmbracelet/bubbletea"

const listFirstRowY = 1 + listChromeHeight // header line + column header + rule

// paneAt reports which pane contains screen cell (x, y) under the current layout.
func (m Model) paneAt(l paneLayout, x, y int) Pane {
	if l.split {
		listStartX := 0
		if l.sidebarWidth > 0 {
			if x < l.sidebarWidth {
				return PaneSidebar
			}
			listStartX = l.sidebarWidth + splitDividerWidth
		}
		if x < listStartX+l.listWidth {
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
			listStartX := 0
			if l.sidebarWidth > 0 {
				listStartX = l.sidebarWidth + splitDividerWidth
			}
			dividerX := listStartX + l.listWidth
			// Click on or near the vertical divider starts a resize drag (split mode only).
			if l.split && msg.X >= dividerX-1 && msg.X <= dividerX+1 {
				m.isDraggingSplit = true
				return m, nil
			}

			switch m.paneAt(l, msg.X, msg.Y) {
			case PaneSidebar:
				m.activePane = PaneSidebar
				entries := m.sidebarEntries()
				rows := max(l.listHeight-2, 1)
				start := 0
				if m.sidebarCursor >= rows {
					start = m.sidebarCursor - rows + 1
				}
				if row := msg.Y - listFirstRowY; row >= 0 && start+row < len(entries) {
					m.sidebarCursor = start + row
					e := entries[m.sidebarCursor]
					if e.Path != m.topicPath || e.Kind != m.kindFilter {
						m.kindFilter = e.Kind
						cmd := m.selectTopic(e.Path)
						return m, cmd
					}
				}
				return m, nil
			case PaneDetail:
				m.activePane = PaneDetail
				return m, nil
			case PaneList:
				m.activePane = PaneList
				if row := msg.Y - listFirstRowY; row >= 0 && row < l.rows {
					m.moveCursor(m.scrollOffset + row - m.cursor)
				}
				return m, nil
			}
		}

	case tea.MouseActionMotion:
		if m.isDraggingSplit && l.split {
			basePercent := 48
			if m.treeViewMode {
				basePercent = 56
			}
			availWidth := l.width
			listStartX := 0
			if l.sidebarWidth > 0 {
				availWidth -= l.sidebarWidth + splitDividerWidth
				listStartX = l.sidebarWidth + splitDividerWidth
			}
			baseTableWidth := availWidth * basePercent / 100
			mouseX := msg.X - listStartX
			target := min(max(mouseX, minSplitListWidth), availWidth-minSplitDetail-splitDividerWidth)
			m.splitOffset = target - baseTableWidth
			return m, nil
		}

	case tea.MouseActionRelease:
		m.isDraggingSplit = false
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		switch m.paneAt(l, msg.X, msg.Y) {
		case PaneSidebar:
			if m.sidebarCursor > 0 {
				m.sidebarCursor--
			}
		case PaneList:
			m.moveCursor(-1)
		case PaneDetail:
			if m.detailScrollOffset > 0 {
				m.detailScrollOffset--
			}
		}
	case tea.MouseButtonWheelDown:
		switch m.paneAt(l, msg.X, msg.Y) {
		case PaneSidebar:
			entries := m.sidebarEntries()
			if m.sidebarCursor < len(entries)-1 {
				m.sidebarCursor++
			}
		case PaneList:
			m.moveCursor(1)
		case PaneDetail:
			if m.detailScrollOffset < m.maxDetailScrollOffset() {
				m.detailScrollOffset++
			}
		}
	}
	return m, nil
}
