package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// maxHistory bounds the jump history (gb / gB).
const maxHistory = 100

// navEntry is one position in the jump history: where the user was, not where they went.
type navEntry struct {
	Scope        string
	TopicPath    string
	ItemID       string
	DetailScroll int
}

// navTarget is an item to select once the list for the current topic has loaded.
type navTarget struct {
	ItemID       string
	Anchor       *detailAnchor
	DetailScroll int // restored scroll offset when Anchor is nil; -1 = top
}

// detailAnchor highlights description lines or a note of one item in the detail pane.
type detailAnchor struct {
	TaskID    string
	LineStart int
	LineEnd   int
	Note      int
}

func (m Model) currentNavEntry() navEntry {
	e := navEntry{Scope: m.scope, TopicPath: m.topicPath, DetailScroll: m.detailScrollOffset}
	if t := m.currentTask(); t != nil {
		e.ItemID = t.ID
	}
	return e
}

// pushHistory records the current position before a jump and clears the forward stack.
func (m *Model) pushHistory() {
	m.backStack = append(m.backStack, m.currentNavEntry())
	if len(m.backStack) > maxHistory {
		m.backStack = m.backStack[len(m.backStack)-maxHistory:]
	}
	m.fwdStack = nil
}

// historyBack (gb) returns to the previous position, remembering the current one for gB.
func (m *Model) historyBack() tea.Cmd {
	if len(m.backStack) == 0 {
		m.statusMsg = "Already at the oldest position"
		return nil
	}
	e := m.backStack[len(m.backStack)-1]
	m.backStack = m.backStack[:len(m.backStack)-1]
	m.fwdStack = append(m.fwdStack, m.currentNavEntry())
	m.statusMsg = "Back"
	return m.restoreNav(e)
}

// historyForward (gB) re-does a jump undone with gb.
func (m *Model) historyForward() tea.Cmd {
	if len(m.fwdStack) == 0 {
		m.statusMsg = "Already at the newest position"
		return nil
	}
	e := m.fwdStack[len(m.fwdStack)-1]
	m.fwdStack = m.fwdStack[:len(m.fwdStack)-1]
	m.backStack = append(m.backStack, m.currentNavEntry())
	m.statusMsg = "Forward"
	return m.restoreNav(e)
}

// restoreNav returns to a history entry, switching scope and topic when needed.
func (m *Model) restoreNav(e navEntry) tea.Cmd {
	target := &navTarget{ItemID: e.ItemID, DetailScroll: e.DetailScroll}
	var cmds []tea.Cmd
	if e.Scope != "" && e.Scope != m.scope {
		m.setScope(e.Scope)
		cmds = append(cmds, m.fetchTopicsCmd())
	}
	if e.TopicPath != m.topicPath || len(cmds) > 0 {
		status := m.statusMsg
		cmds = append(cmds, m.selectTopic(e.TopicPath))
		m.statusMsg = status
		m.pendingSelect = target
		return tea.Sequence(cmds...)
	}
	m.selectItem(target)
	return nil
}

// selectItem moves the list cursor to target.ItemID (if visible) and applies its
// anchor or scroll position. It reports whether the item was found.
func (m *Model) selectItem(target *navTarget) bool {
	findRow := func(rows []TreeRow) int {
		for i, r := range rows {
			if r.Task != nil && r.Task.ID == target.ItemID && !r.Pointer && !r.Ghost {
				return i
			}
		}
		for i, r := range rows {
			if r.Task != nil && r.Task.ID == target.ItemID {
				return i
			}
		}
		return -1
	}

	idx := findRow(m.displayRows())
	if idx < 0 && m.hideDone {
		if t := m.lookupItem(target.ItemID); isDoneTask(t) {
			m.hideDone = false
			idx = findRow(m.displayRows())
		}
	}
	if idx < 0 {
		return false
	}
	m.cursor = idx
	m.detailScrollOffset = 0
	m.linkCursor = 0
	m.anchor = target.Anchor
	m.clampScroll()
	switch {
	case target.Anchor != nil:
		m.scrollToAnchor()
	case target.DetailScroll > 0:
		m.detailScrollOffset = min(target.DetailScroll, m.maxDetailScrollOffset())
	}
	return true
}
