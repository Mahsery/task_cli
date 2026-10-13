package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"task_cli/internal/domain"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampScroll()
		return m, nil

	case reloadMsg:
		m.applyReload(msg)
		return m, m.fetchRelatedCmd()

	case reloadErrMsg:
		m.applyReload(msg.tasks)
		m.err = msg.err
		m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
		return m, m.fetchRelatedCmd()

	case topicsMsg:
		m.applyTopics(msg)
		return m, nil

	case topicOpMsg:
		m.statusMsg = msg.status
		var cmds []tea.Cmd
		cmds = append(cmds, m.fetchTopicsCmd())
		if msg.selectPath != nil {
			cmds = append(cmds, m.selectTopic(*msg.selectPath))
		}
		return m, tea.Sequence(cmds...)

	case relatedMsg:
		m.applyRelated(msg)
		return m, nil

	case taskDetailMsg:
		if msg != nil {
			for _, t := range m.tasks {
				if t.ID == msg.ID {
					t.DiffContent = msg.DiffContent
					t.Description = msg.Description
					t.Notes = msg.Notes
					break
				}
			}
			m.clampScroll()
		}
		return m, nil

	case statusMsg:
		m.statusMsg = string(msg)
		return m, nil

	case errMsg:
		m.err = msg
		m.statusMsg = fmt.Sprintf("Error: %v", msg)
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// applyReload installs a fresh task list, keeping cursor/scroll valid and pruning stale selections.
func (m *Model) applyReload(tasks []*domain.Task) {
	m.tasks = tasks
	if m.isBoardView() {
		sortBoard(m.tasks)
	}
	m.taskByID = make(map[string]*domain.Task, len(m.tasks))
	for _, t := range m.tasks {
		m.taskByID[t.ID] = t
	}
	m.clampScroll()
	if len(m.selected) > 0 {
		for id := range m.selected {
			if _, ok := m.taskByID[id]; !ok {
				delete(m.selected, id)
			}
		}
	}
	if m.pendingSelect != nil {
		target := m.pendingSelect
		m.pendingSelect = nil
		m.selectItem(target)
	}
}
