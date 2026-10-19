package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"task_cli/internal/domain"
)

func (m Model) postReloadCmd() tea.Cmd {
	relCmd := m.fetchRelatedCmd()
	if m.showDiff {
		if t := m.currentTask(); t != nil && t.DiffContent == "" && t.CommitSHA != "" {
			return tea.Batch(relCmd, m.fetchTaskDetailCmd(t.ID))
		}
	}
	return relCmd
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampScroll()
		return m, nil

	case reloadMsg:
		m.applyReload(msg)
		return m, m.postReloadCmd()

	case reloadErrMsg:
		m.applyReload(msg.tasks)
		m.err = msg.err
		m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
		return m, m.postReloadCmd()

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
				if t.ID != msg.ID {
					continue
				}
				t.DiffContent = msg.DiffContent
				t.Description = msg.Description
				t.Reviewer = msg.Reviewer
				t.Notes = msg.Notes
				t.ReviewerNotes = msg.ReviewerNotes
				t.Commits = msg.Commits
				t.Branch = msg.Branch
				t.ExternalRef = msg.ExternalRef
				t.PRURL = msg.PRURL
				break
			}
			m.clampScroll()
		}
		return m, nil

	case streamSubscribedMsg:
		m.eventCh = msg.ch
		wasReconnecting := m.reconnectingStream
		m.reconnectingStream = false
		if wasReconnecting {
			return m, tea.Batch(m.waitForEventCmd(), tea.Sequence(m.fetchTopicsCmd(), m.fetchTasksCmd()))
		}
		return m, m.waitForEventCmd()

	case streamDisconnectedMsg:
		m.eventCh = nil
		if m.client == nil {
			m.reconnectingStream = false
			return m, nil
		}
		m.reconnectingStream = true
		return m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
			return reconnectStreamMsg{}
		})

	case reconnectStreamMsg:
		if m.eventCh != nil || m.client == nil {
			m.reconnectingStream = false
			return m, nil
		}
		return m, m.subscribeEventsCmd()

	case daemonEventMsg:
		return m, tea.Batch(m.waitForEventCmd(), tea.Sequence(m.fetchTopicsCmd(), m.fetchTasksCmd()))

	case editTaskMsg:
		return m.openEditTaskEditor(msg.task)

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
	var prevRow TreeRow
	var hasPrev bool
	if !m.resetCursor {
		prevRow, hasPrev = m.currentRow()
	}
	m.resetCursor = false

	prevByID := m.taskByID
	if prevByID == nil && len(m.tasks) > 0 {
		prevByID = make(map[string]*domain.Task, len(m.tasks))
		for _, old := range m.tasks {
			prevByID[old.ID] = old
		}
	}

	m.tasks = tasks
	if m.isBoardView() {
		sortBoard(m.tasks)
	}
	m.taskByID = make(map[string]*domain.Task, len(m.tasks))
	for _, t := range m.tasks {
		if old := prevByID[t.ID]; old != nil {
			if t.DiffContent == "" && t.CommitSHA != "" && old.CommitSHA == t.CommitSHA {
				t.DiffContent = old.DiffContent
			}
			if len(t.Commits) == 0 && len(old.Commits) > 0 && old.CommitSHA == t.CommitSHA {
				t.Commits = old.Commits
			}
		}
		m.taskByID[t.ID] = t
	}

	if len(m.selected) > 0 {
		for id := range m.selected {
			t, ok := m.taskByID[id]
			if !ok || (m.hideDone && isDoneTask(t)) {
				delete(m.selected, id)
			}
		}
	}

	if m.pendingSelect != nil {
		target := m.pendingSelect
		m.pendingSelect = nil
		if !m.selectItem(target) {
			m.clampScroll()
		}
		return
	}

	if hasPrev && prevRow.Task != nil {
		m.reanchorCursor(prevRow.Task.ID, prevRow.Pointer, prevRow.Ghost)
		return
	}
	m.clampScroll()
}
