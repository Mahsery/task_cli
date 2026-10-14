package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"task_cli/internal/domain"
	"task_cli/internal/editor"
	"task_cli/internal/protocol"
)

// handleKey dispatches keyboard input: dialogs, search/link/tag prompts, chords and normal-mode bindings.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// An error stays on the legend line until the next keypress acknowledges it.
	if m.err != nil {
		m.err = nil
		m.statusMsg = ""
	}
	if m.showHelp {
		switch msg.String() {
		case "?", "esc", "enter", "q":
			m.showHelp = false
			return m, nil
		}
		return m, nil
	}
	maxRows := m.getMaxRows()
	if dlg := m.activeConfirmDialog(); dlg != nil {
		k := msg.String()
		if k == "y" || k == "Y" || k == "enter" {
			if dlg.Kind != ConfirmDeleteTopic {
				undo := m.buildUndoAction(dlg)
				m.undoStack = append(m.undoStack, undo)
				if len(m.undoStack) > 50 {
					m.undoStack = m.undoStack[len(m.undoStack)-50:]
				}
			}

			m.confirmDialog = nil
			m.confirmDelete = false
			m.deleteTaskID = ""
			if dlg.IsBatch {
				m.selected = make(map[string]bool)
			}
			return m.executeConfirmedDialog(dlg)
		}
		if k == "n" || k == "N" || k == "esc" {
			m.confirmDialog = nil
			m.confirmDelete = false
			m.deleteTaskID = ""
			m.statusMsg = "Action cancelled"
			return m, nil
		}
		return m, nil
	}

	if m.isSearch {
		switch msg.String() {
		case "enter":
			m.isSearch = false
			m.filterText = m.input.Value()
			m.input.Blur()
			return m, m.fetchTasksCmd()
		case "esc":
			m.isSearch = false
			m.filterText = ""
			m.input.Reset()
			m.input.Blur()
			return m, m.fetchTasksCmd()
		default:
			cmd := m.updateInput(msg)
			m.filterText = m.input.Value()
			return m, tea.Batch(cmd, m.fetchTasksCmd())
		}
	}

	if m.isLinking {
		candidates := m.linkCandidateItems()
		switch msg.String() {
		case "down":
			if len(candidates) > 0 {
				m.linkCandidateIdx = min(m.linkCandidateIdx+1, len(candidates)-1)
			}
			return m, nil
		case "up":
			if len(candidates) > 0 {
				m.linkCandidateIdx = max(m.linkCandidateIdx-1, 0)
			}
			return m, nil
		case "enter":
			m.isLinking = false
			prereqID := strings.TrimSpace(m.input.Value())
			if len(candidates) > 0 && m.linkCandidateIdx >= 0 && m.linkCandidateIdx < len(candidates) {
				if prereqID == "" {
					prereqID = candidates[m.linkCandidateIdx].Value
				} else if norm, err := domain.NormalizeID(prereqID, m.scope); err != nil || m.lookupItem(norm) == nil {
					prereqID = candidates[m.linkCandidateIdx].Value
				}
			}
			m.linkInput = prereqID
			m.input.Blur()
			if prereqID != "" {
				return m, m.linkTaskCmd(m.linkTaskID, prereqID)
			}
			m.statusMsg = "Link cancelled"
			return m, nil
		case "esc":
			m.isLinking = false
			m.linkInput = ""
			m.linkCandidateIdx = 0
			m.input.Reset()
			m.input.Blur()
			m.statusMsg = "Link cancelled"
			return m, nil
		default:
			cmd := m.updateInput(msg)
			m.linkInput = m.input.Value()
			m.linkCandidateIdx = 0
			return m, cmd
		}
	}

	if m.isReplying {
		switch msg.String() {
		case "enter":
			m.isReplying = false
			note := strings.TrimSpace(m.input.Value())
			taskID := m.replyTaskID
			m.replyInput = ""
			m.input.Reset()
			m.input.Blur()
			if note == "" {
				m.statusMsg = "Reply cancelled (empty)"
				return m, nil
			}
			if cur := m.lookupItem(taskID); cur != nil {
				cur.Notes = append(cur.Notes, domain.TaskNote{
					Author:    m.resolveActor(),
					Content:   note,
					CreatedAt: time.Now(),
				})
			}
			m.statusMsg = fmt.Sprintf("Added reply to %s", taskID)
			return m, m.replyTaskCmd(taskID, note)
		case "esc":
			m.isReplying = false
			m.replyInput = ""
			m.input.Reset()
			m.input.Blur()
			m.statusMsg = "Reply cancelled"
			return m, nil
		default:
			cmd := m.updateInput(msg)
			m.replyInput = m.input.Value()
			return m, cmd
		}
	}

	if m.topicForm != nil {
		return m.handleTopicFormKey(msg)
	}

	if m.picker != nil {
		return m.handlePickerKey(msg)
	}

	kStr := msg.String()

	// Vim Movement Chords: Accumulate numeric prefix in normal navigation mode
	if len(kStr) == 1 && kStr[0] >= '0' && kStr[0] <= '9' {
		if kStr[0] != '0' || m.chordBuffer != "" {
			m.chordBuffer += kStr
			return m, nil
		}
	}

	if (kStr == "esc" || kStr == "c") && (m.chordBuffer != "" || m.pendingPrefix != "") {
		m.chordBuffer = ""
		m.pendingPrefix = ""
		m.prefixCount = 0
		m.statusMsg = "Chord cancelled"
		return m, nil
	}

	var chordCount int
	if m.chordBuffer != "" {
		// A non-numeric buffer simply leaves chordCount at 0 (no repeat prefix).
		_, _ = fmt.Sscanf(m.chordBuffer, "%d", &chordCount)
		m.chordBuffer = ""
	}

	// Two-key chords: ]l, ]t, [l, [t, gt, gl, gf, gb, gB, gg
	if m.pendingPrefix != "" {
		prefix := m.pendingPrefix
		count := m.prefixCount
		m.pendingPrefix = ""
		m.prefixCount = 0
		switch prefix {
		case "]":
			switch kStr {
			case "l":
				m.moveLinkCursor(1, count)
				return m, nil
			case "t":
				cmd := m.cycleTopic(max(count, 1))
				return m, cmd
			}
		case "[":
			switch kStr {
			case "l":
				m.moveLinkCursor(-1, count)
				return m, nil
			case "t":
				cmd := m.cycleTopic(-max(count, 1))
				return m, cmd
			}
		case "g":
			switch kStr {
			case "t":
				m.openPicker(pickGoTopic, nil, "")
				return m, nil
			case "l":
				cur := m.currentTask()
				if cur != nil {
					m.openPicker(pickInsertLink, []string{cur.ID}, "")
				}
				return m, nil
			case "f":
				cmd := m.followLink(count)
				return m, cmd
			case "b":
				cmd := m.historyBack()
				return m, cmd
			case "B":
				cmd := m.historyForward()
				return m, cmd
			case "g":
				if m.activePane == PaneSidebar {
					m.sidebarCursor = 0
					return m, nil
				}
				if m.activePane == PaneDetail {
					m.detailScrollOffset = 0
					return m, nil
				}
				m.cursor = 0
				m.scrollOffset = 0
				m.detailScrollOffset = 0
				return m, nil
			}
		}
		return m, nil
	}

	if m.activePane == PaneSidebar {
		updated, cmd, consumed := m.handleSidebarKey(kStr, chordCount)
		if consumed {
			return updated, cmd
		}
		switch kStr {
		case "d", "s", "a", "e", "v", "V", "x", "A", "L", "p", "M":
			m.statusMsg = "Tab to Tasks to interact with items"
			return m, nil
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.showHelp = true
		return m, nil

	case "B":
		width, _ := m.termSize()
		if width >= splitMinWidth+15 {
			m.sidebarHidden = !m.sidebarHidden
			if m.sidebarHidden {
				if m.activePane == PaneSidebar {
					m.activePane = PaneList
				}
				m.statusMsg = "Topics sidebar hidden"
			} else {
				m.activePane = PaneSidebar
				m.statusMsg = "Topics sidebar visible"
			}
		} else {
			m.narrowSidebarOpen = !m.narrowSidebarOpen
			if m.narrowSidebarOpen {
				m.activePane = PaneSidebar
				m.statusMsg = "Topics drawer opened"
			} else {
				m.activePane = PaneList
				m.statusMsg = "Topics drawer closed"
			}
		}
		return m, nil

	case "C", "R":
		if m.activePane == PaneSidebar {
			return m, nil
		}
		if cur := m.currentTask(); cur != nil {
			m.isReplying = true
			m.replyInput = ""
			m.replyTaskID = cur.ID
			m.focusInput()
			m.statusMsg = fmt.Sprintf("Replying to %s", cur.ID)
		}
		return m, nil

	case "tab":
		l := m.layout()
		switch m.activePane {
		case PaneSidebar:
			m.activePane = PaneList
			m.statusMsg = "Tasks focused"
		case PaneList:
			m.activePane = PaneDetail
			maxOffset := m.maxDetailScrollOffset()
			if m.detailScrollOffset > maxOffset {
				m.detailScrollOffset = maxOffset
			}
			m.statusMsg = "Details focused"
		case PaneDetail:
			if l.sidebarWidth > 0 {
				m.activePane = PaneSidebar
				m.statusMsg = "Topics focused"
			} else {
				m.activePane = PaneList
				m.statusMsg = "Tasks focused"
			}
		}
		return m, nil

	case "shift+tab":
		l := m.layout()
		switch m.activePane {
		case PaneSidebar:
			m.activePane = PaneDetail
			maxOffset := m.maxDetailScrollOffset()
			if m.detailScrollOffset > maxOffset {
				m.detailScrollOffset = maxOffset
			}
			m.statusMsg = "Details focused"
		case PaneList:
			if l.sidebarWidth > 0 {
				m.activePane = PaneSidebar
				m.statusMsg = "Topics focused"
			} else {
				m.activePane = PaneDetail
				maxOffset := m.maxDetailScrollOffset()
				if m.detailScrollOffset > maxOffset {
					m.detailScrollOffset = maxOffset
				}
				m.statusMsg = "Details focused"
			}
		case PaneDetail:
			m.activePane = PaneList
			m.statusMsg = "Tasks focused"
		}
		return m, nil

	case "l", "right":
		if m.activePane == PaneList && len(m.tasks) > 0 {
			m.activePane = PaneDetail
			maxOffset := m.maxDetailScrollOffset()
			if m.detailScrollOffset > maxOffset {
				m.detailScrollOffset = maxOffset
			}
			m.statusMsg = "Details focused"
			return m, nil
		}

	case "h", "left":
		if m.activePane == PaneDetail {
			m.activePane = PaneList
			m.statusMsg = "Tasks focused"
			return m, nil
		}
		l := m.layout()
		if m.activePane == PaneList && l.sidebarWidth > 0 {
			m.activePane = PaneSidebar
			m.statusMsg = "Topics focused"
			return m, nil
		}

	case "]", "[", "g":
		m.pendingPrefix = msg.String()
		m.prefixCount = chordCount
		return m, nil

	case ">", ".", "alt+right", "alt+l":
		if m.isSplitView() {
			m.splitOffset += 4
		}
		return m, nil

	case "<", ",", "alt+left", "alt+h":
		if m.isSplitView() {
			m.splitOffset -= 4
		}
		return m, nil

	case "=":
		if m.isSplitView() {
			m.splitOffset = 0
		}
		return m, nil

	case "up", "k":
		step := 1
		if chordCount > 0 {
			step = chordCount
		}
		if m.activePane == PaneDetail {
			m.detailScrollOffset -= step
			if m.detailScrollOffset < 0 {
				m.detailScrollOffset = 0
			}
			return m, nil
		}
		if len(m.displayRows()) > 0 {
			m.cursor -= step
			if m.cursor < 0 {
				m.cursor = 0
			}
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
			m.detailScrollOffset = 0
		}

	case "down", "j":
		step := 1
		if chordCount > 0 {
			step = chordCount
		}
		if m.activePane == PaneDetail {
			maxOffset := m.maxDetailScrollOffset()
			m.detailScrollOffset += step
			if m.detailScrollOffset > maxOffset {
				m.detailScrollOffset = maxOffset
			}
			return m, nil
		}
		rows := m.displayRows()
		if len(rows) > 0 {
			m.cursor += step
			if m.cursor >= len(rows) {
				m.cursor = len(rows) - 1
			}
			if m.cursor >= m.scrollOffset+maxRows {
				m.scrollOffset = m.cursor - maxRows + 1
			}
			m.detailScrollOffset = 0
		}

	case "home":
		if m.activePane == PaneDetail {
			m.detailScrollOffset = 0
			return m, nil
		}
		m.cursor = 0
		m.scrollOffset = 0
		m.detailScrollOffset = 0

	case "G", "end":
		if m.activePane == PaneDetail {
			if chordCount > 0 {
				m.detailScrollOffset = chordCount - 1
				maxOffset := m.maxDetailScrollOffset()
				if m.detailScrollOffset > maxOffset {
					m.detailScrollOffset = maxOffset
				}
				if m.detailScrollOffset < 0 {
					m.detailScrollOffset = 0
				}
			} else {
				m.detailScrollOffset = m.maxDetailScrollOffset()
			}
			return m, nil
		}
		rows := m.displayRows()
		if len(rows) > 0 {
			if chordCount > 0 {
				target := chordCount - 1
				if target >= len(rows) {
					target = len(rows) - 1
				}
				if target < 0 {
					target = 0
				}
				m.cursor = target
			} else {
				m.cursor = len(rows) - 1
			}
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
			if m.cursor >= m.scrollOffset+maxRows {
				m.scrollOffset = m.cursor - maxRows + 1
			}
			m.detailScrollOffset = 0
		}

	case "J", "ctrl+d", "pgdown":
		times := 1
		if chordCount > 0 {
			times = chordCount
		}
		if m.activePane == PaneDetail {
			maxOffset := m.maxDetailScrollOffset()
			m.detailScrollOffset += 6 * times
			if m.detailScrollOffset > maxOffset {
				m.detailScrollOffset = maxOffset
			}
			return m, nil
		}
		rows := m.displayRows()
		if len(rows) > 0 {
			step := (maxRows / 2) * times
			if step < 1 {
				step = 1
			}
			m.cursor += step
			if m.cursor >= len(rows) {
				m.cursor = len(rows) - 1
			}
			if m.cursor >= m.scrollOffset+maxRows {
				m.scrollOffset = m.cursor - maxRows + 1
			}
			m.detailScrollOffset = 0
		}

	case "O":
		cmd := m.cycleKindFilter()
		return m, cmd

	case "K", "ctrl+u", "pgup":
		times := 1
		if chordCount > 0 {
			times = chordCount
		}
		if m.activePane == PaneDetail {
			m.detailScrollOffset -= 6 * times
			if m.detailScrollOffset < 0 {
				m.detailScrollOffset = 0
			}
			return m, nil
		}
		if len(m.displayRows()) > 0 {
			step := (maxRows / 2) * times
			if step < 1 {
				step = 1
			}
			m.cursor -= step
			if m.cursor < 0 {
				m.cursor = 0
			}
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
			m.detailScrollOffset = 0
		}

	case " ", "space":
		if m.activePane != PaneList {
			return m, nil
		}
		// Multi-select toggle: toggle selection for current task, then advance cursor down
		rows := m.displayRows()
		if len(rows) > 0 && m.cursor < len(rows) {
			row := rows[m.cursor]
			if !row.Ghost && row.Task != nil {
				if m.selected == nil {
					m.selected = make(map[string]bool)
				}
				if m.selected[row.Task.ID] {
					delete(m.selected, row.Task.ID)
				} else {
					m.selected[row.Task.ID] = true
				}
			}
			if m.cursor < len(rows)-1 {
				m.cursor++
				if m.cursor >= m.scrollOffset+maxRows {
					m.scrollOffset = m.cursor - maxRows + 1
				}
			}
			m.detailScrollOffset = 0
		}

	case "v":
		if m.activePane != PaneList {
			return m, nil
		}
		// Toggle selection for current task without advancing cursor
		rows := m.displayRows()
		if len(rows) > 0 && m.cursor < len(rows) {
			row := rows[m.cursor]
			if !row.Ghost && row.Task != nil {
				if m.selected == nil {
					m.selected = make(map[string]bool)
				}
				if m.selected[row.Task.ID] {
					delete(m.selected, row.Task.ID)
				} else {
					m.selected[row.Task.ID] = true
				}
			}
		}

	case "V":
		if m.activePane != PaneList {
			return m, nil
		}
		// Select all / Deselect all toggle
		if len(m.tasks) > 0 {
			if len(m.selected) == len(m.tasks) {
				m.selected = make(map[string]bool)
				m.statusMsg = "Deselected all"
			} else {
				m.selected = make(map[string]bool, len(m.tasks))
				for _, t := range m.tasks {
					m.selected[t.ID] = true
				}
				m.statusMsg = fmt.Sprintf("Selected all (%d tasks)", len(m.tasks))
			}
		}

	case "esc", "c":
		if m.activePane == PaneDetail {
			m.activePane = PaneList
			m.statusMsg = "Tasks focused"
			return m, nil
		}
		if len(m.selected) > 0 {
			m.selected = make(map[string]bool)
			m.statusMsg = "Selection cleared"
			return m, nil
		}
		if m.filterText != "" {
			m.filterText = ""
			m.statusMsg = "Filter reset"
			return m, m.fetchTasksCmd()
		}

	case "S":
		// Toggle local / global scope
		next := otherScope(m.scope)
		m.setScope(next)
		m.statusMsg = fmt.Sprintf("Switched to %s scope", m.scope)
		return m, tea.Sequence(m.fetchTopicsCmd(), m.fetchTasksCmd())

	case "r":
		m.statusMsg = "Refreshed"
		return m, tea.Sequence(m.fetchTopicsCmd(), m.fetchTasksCmd())

	case "/":
		if m.filterText != "" {
			m.filterText = ""
			m.input.Reset()
			return m, m.fetchTasksCmd()
		}
		m.isSearch = true
		m.prevFilter = m.filterText
		m.filterText = ""
		m.focusInput()
		return m, nil

	case "enter":
		if m.activePane == PaneDetail && m.focusedLinkIndex() > 0 {
			cmd := m.followLink(m.focusedLinkIndex())
			return m, cmd
		}
		m.showDiff = !m.showDiff
		maxOffset := m.maxDetailScrollOffset()
		if m.detailScrollOffset > maxOffset {
			m.detailScrollOffset = maxOffset
		}
		if t := m.currentTask(); m.showDiff && t != nil {
			if t.DiffContent == "" && t.CommitSHA != "" {
				return m, m.fetchTaskDetailCmd(t.ID)
			}
		}
		return m, nil

	case "x":
		// Batch delete prompt if tasks selected in list pane, otherwise single-task
		if m.activePane == PaneList && len(m.selected) > 0 {
			m.confirmDialog = m.newBatchConfirmDialog(ConfirmDelete, "CONFIRM BATCH DELETION", fmt.Sprintf("Permanently delete %d selected tasks?", len(m.selected)))
			return m, nil
		}
		if cur := m.currentTask(); cur != nil {
			m.confirmDialog = m.newSingleConfirmDialog(ConfirmDelete, "CONFIRM DELETION", fmt.Sprintf("Permanently delete task %s?", cur.ID), cur)
			return m, nil
		}

	case "a":
		actor := m.resolveActor()
		if m.activePane == PaneList && len(m.selected) > 0 {
			dlg := m.newBatchConfirmDialog(ConfirmClaim, "CONFIRM BATCH CLAIM", fmt.Sprintf("Claim %d selected tasks as %q?", len(m.selected), actor))
			dlg.TargetAssignee = actor
			m.confirmDialog = dlg
			return m, nil
		}
		if cur := m.currentTask(); cur != nil {
			dlg := m.newSingleConfirmDialog(ConfirmClaim, "CONFIRM TASK CLAIM", fmt.Sprintf("Claim task %s as %q?", cur.ID, actor), cur)
			dlg.TargetAssignee = actor
			m.confirmDialog = dlg
		}
		return m, nil

	case "ctrl+a", "alt+a":
		m.showArchived = !m.showArchived
		if m.showArchived {
			m.statusMsg = "Archived tasks visible"
		} else {
			m.statusMsg = "Archived tasks hidden"
		}
		return m, m.fetchTasksCmd()

	case "A":
		if m.activePane == PaneList && len(m.selected) > 0 {
			ids := m.getSelectedIDs()
			anyUnarchived := false
			for _, id := range ids {
				for _, t := range m.tasks {
					if t.ID == id && !t.IsArchived() {
						anyUnarchived = true
						break
					}
				}
				if anyUnarchived {
					break
				}
			}
			targetArchive := anyUnarchived
			var snapshots []TaskUndoSnapshot
			now := time.Now()
			for _, id := range ids {
				for _, t := range m.tasks {
					if t.ID != id {
						continue
					}
					snapshots = append(snapshots, cloneTaskSnapshot(t, nil))
					if targetArchive {
						t.ArchivedAt = &now
						t.Status = domain.StatusArchived
					} else {
						t.ArchivedAt = nil
						t.Status = domain.StatusOpen
					}
					break
				}
			}
			actionDesc := "Archived"
			if !targetArchive {
				actionDesc = "Unarchived"
			}
			m.pushUndo(UndoArchive, fmt.Sprintf("%s %d tasks", actionDesc, len(ids)), snapshots...)
			m.statusMsg = fmt.Sprintf("%s %d tasks (press 'u' to undo)", actionDesc, len(ids))
			return m, m.batchArchiveCmd(ids, targetArchive)
		}
		cur := m.currentTask()
		if cur != nil {
			now := time.Now()
			snap := cloneTaskSnapshot(cur, nil)
			if cur.IsArchived() {
				m.pushUndo(UndoArchive, fmt.Sprintf("Unarchived %s", cur.ID), snap)
				cur.ArchivedAt = nil
				cur.Status = domain.StatusOpen
				m.statusMsg = fmt.Sprintf("Unarchived %s (press 'u' to undo)", cur.ID)
				return m, m.unarchiveTaskCmd(cur.ID)
			}
			m.pushUndo(UndoArchive, fmt.Sprintf("Archived %s", cur.ID), snap)
			cur.ArchivedAt = &now
			cur.Status = domain.StatusArchived
			m.statusMsg = fmt.Sprintf("Archived %s (press 'u' to undo)", cur.ID)
			return m, m.archiveTaskCmd(cur.ID)
		}
		return m, nil

	case "t":
		m.treeViewMode = !m.treeViewMode
		m.clampScroll()
		if m.treeViewMode {
			m.statusMsg = "Tree view mode enabled"
		} else {
			m.statusMsg = "Standard list view enabled"
		}
		return m, nil

	case "L":
		cur := m.currentTask()
		if cur != nil {
			m.isLinking = true
			m.linkInput = ""
			m.linkTaskID = cur.ID
			m.focusInput()
			m.statusMsg = fmt.Sprintf("Linking %s (enter prerequisite ID)", cur.ID)
		}
		return m, nil

	case "U":
		if m.activePane == PaneSidebar {
			m.statusMsg = "Tab to Tasks to unlink dependencies"
			return m, nil
		}
		cur := m.currentTask()
		if cur == nil {
			return m, nil
		}
		if len(cur.DependsOn) == 0 {
			m.statusMsg = fmt.Sprintf("Task %s has no prerequisites to unlink", cur.ID)
			return m, nil
		}
		m.openPicker(pickUnlinkDep, []string{cur.ID}, "")
		return m, nil

	case "d":
		// Batch Done if tasks selected in list pane, otherwise single-task reversible done
		if m.activePane == PaneList && len(m.selected) > 0 {
			ids := m.getSelectedIDs()
			var blockedIDs []string
			for _, id := range ids {
				for _, task := range m.tasks {
					if task.ID == id && task.IsBlocked {
						blockedIDs = append(blockedIDs, id)
						break
					}
				}
			}
			if len(blockedIDs) > 0 {
				dlg := m.newBatchConfirmDialog(ConfirmForceDone, "FORCE MARK BATCH DONE", fmt.Sprintf("%d task(s) (%s) blocked by prerequisites. Force mark all %d tasks DONE?", len(blockedIDs), strings.Join(blockedIDs, ", "), len(ids)))
				dlg.TargetStatus = domain.StatusDone
				m.confirmDialog = dlg
				return m, nil
			}
			dlg := m.newBatchConfirmDialog(ConfirmDone, "CONFIRM BATCH DONE", fmt.Sprintf("Mark %d selected tasks as DONE?", len(ids)))
			dlg.TargetStatus = domain.StatusDone
			m.confirmDialog = dlg
			return m, nil
		}
		cur := m.currentTask()
		if cur != nil {
			if cur.Status == domain.StatusDone || cur.Status == domain.StatusClosed {
				m.pushUndo(UndoStatusChange, fmt.Sprintf("Reopened %s", cur.ID), cloneTaskSnapshot(cur, nil))
				cur.Status = domain.StatusOpen
				m.statusMsg = fmt.Sprintf("Reopened %s (press 'u' to undo)", cur.ID)
				return m, m.reopenTaskCmd(cur.ID)
			}
			if cur.IsBlocked {
				dlg := m.newSingleConfirmDialog(ConfirmForceDone, "FORCE MARK DONE", fmt.Sprintf("Task %s is blocked by unfinished prerequisite(s) (%s). Force mark DONE?", cur.ID, strings.Join(cur.BlockedBy, ", ")), cur)
				dlg.TargetStatus = domain.StatusDone
				m.confirmDialog = dlg
				return m, nil
			}
			m.pushUndo(UndoStatusChange, fmt.Sprintf("Marked %s as DONE", cur.ID), cloneTaskSnapshot(cur, nil))
			cur.Status = domain.StatusDone
			m.statusMsg = fmt.Sprintf("Marked %s as DONE (press 'u' to undo)", cur.ID)
			return m, m.markDoneCmd(cur.ID)
		}

	case "s":
		// Batch status toggle if tasks selected in list pane, otherwise single-task toggle
		if m.activePane == PaneList && len(m.selected) > 0 {
			m.confirmDialog = m.newBatchConfirmDialog(ConfirmStatus, "CONFIRM BATCH STATUS CYCLE", fmt.Sprintf("Cycle status for %d selected tasks?", len(m.selected)))
			return m, nil
		}
		cur := m.currentTask()
		if cur != nil {
			newStatus := domain.StatusInProgress
			switch cur.Status {
			case domain.StatusInProgress:
				newStatus = domain.StatusOpen
			case domain.StatusDone, domain.StatusClosed:
				newStatus = domain.StatusInProgress
			}
			m.pushUndo(UndoStatusChange, fmt.Sprintf("Status change on %s", cur.ID), cloneTaskSnapshot(cur, nil))
			cur.Status = newStatus
			m.statusMsg = fmt.Sprintf("Updated %s status to %s (press 'u' to undo)", cur.ID, newStatus)
			return m, m.updateStatusCmd(cur.ID, newStatus)
		}

	case "M":
		if m.activePane == PaneList && len(m.selected) > 0 {
			m.openPicker(pickMoveItems, m.getSelectedIDs(), "")
		} else if cur := m.currentTask(); cur != nil {
			m.openPicker(pickMoveItems, []string{cur.ID}, "")
		}
		return m, nil

	case "p":
		cur := m.currentTask()
		if cur != nil && cur.IsPost() {
			m.openPicker(pickPromote, []string{cur.ID}, "")
		} else if cur != nil {
			m.statusMsg = "Only posts can be promoted to tasks"
		}
		return m, nil

	case "u":
		if len(m.undoStack) == 0 {
			m.statusMsg = "Nothing to undo"
			return m, nil
		}
		lastUndo := m.undoStack[len(m.undoStack)-1]
		m.undoStack = m.undoStack[:len(m.undoStack)-1]
		m.statusMsg = fmt.Sprintf("Undid: %s", lastUndo.Description)

		// In-memory restoration for immediate responsiveness
		switch lastUndo.Kind {
		case UndoStatusChange:
			for _, snap := range lastUndo.Snapshots {
				if snap.Task != nil {
					snap.Task.Status = snap.PreviousStatus
				}
				for _, t := range m.tasks {
					if snap.Task != nil && t.ID == snap.Task.ID {
						t.Status = snap.PreviousStatus
					}
				}
			}
		case UndoClaim:
			for _, snap := range lastUndo.Snapshots {
				if snap.Task != nil {
					snap.Task.Assignee = snap.PreviousAssignee
					snap.Task.Status = snap.PreviousStatus
				}
				for _, t := range m.tasks {
					if snap.Task != nil && t.ID == snap.Task.ID {
						t.Assignee = snap.PreviousAssignee
						t.Status = snap.PreviousStatus
					}
				}
			}
		case UndoArchive:
			for _, snap := range lastUndo.Snapshots {
				if snap.Task != nil {
					snap.Task.ArchivedAt = snap.PreviousArchivedAt
					snap.Task.Status = snap.PreviousStatus
				}
				for _, t := range m.tasks {
					if snap.Task != nil && t.ID == snap.Task.ID {
						t.ArchivedAt = snap.PreviousArchivedAt
						t.Status = snap.PreviousStatus
					}
				}
			}
		}

		return m, m.executeUndoCmd(lastUndo)

	case "n":
		// Create new task/post via editor
		top := m.topicPath
		if top == domain.UnfiledTopic {
			top = ""
		}
		if t := m.topicByPath(top); t != nil && t.Kind == domain.TopicGroup {
			accept := []domain.ItemKind{domain.KindTask}
			if m.isBoardView() {
				accept = []domain.ItemKind{domain.KindPost}
			}
			var validChildren []string
			for _, e := range orderedTopics(m.topics) {
				if strings.HasPrefix(e.Path, top+"/") {
					ok := true
					for _, k := range accept {
						ok = ok && e.Topic.Kind.Accepts(k)
					}
					if ok {
						validChildren = append(validChildren, e.Path)
					}
				}
			}
			if len(validChildren) == 0 {
				m.statusMsg = fmt.Sprintf("Group @%s has no writable sub-topics. Create one with 'N' in the sidebar.", top)
				return m, nil
			}
			if len(validChildren) == 1 {
				return m.openNewTaskEditor(validChildren[0])
			}
			m.openPicker(pickNewItemTopic, nil, top)
			return m, nil
		}
		return m.openNewTaskEditor(top)

	case "e":
		// Edit existing task via editor
		if m.activePane == PaneSidebar {
			m.statusMsg = "Tab to Tasks to edit items"
			return m, nil
		}
		if cur := m.currentTask(); cur != nil {
			t := cur
			tmpPath, err := editor.CreateEditTaskFile(t)
			if err != nil {
				return m, func() tea.Msg { return errMsg(err) }
			}
			cmd := editor.BuildEditorCmd(m.editorCmd, tmpPath)
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				if err != nil {
					_ = os.Remove(tmpPath)
					return errMsg(err)
				}
				data, err := os.ReadFile(tmpPath)
				if err != nil {
					return errMsg(err)
				}
				parsed, err := editor.ParseEditTaskContent(string(data), t)
				if err != nil {
					return errMsg(fmt.Errorf("%w (draft preserved at: %s)", err, tmpPath))
				}
				var addNote *string
				if parsed.NewNote != "" {
					addNote = &parsed.NewNote
				}
				_, err = m.client.UpdateTask(protocol.UpdateTaskPayload{
					ID:          t.ID,
					Title:       &parsed.Title,
					Description: &parsed.Description,
					Status:      &parsed.Status,
					Priority:    &parsed.Priority,
					Assignee:    &parsed.Assignee,
					AddNote:     addNote,
					Author:      m.resolveActor(),
				})
				if err != nil {
					return errMsg(fmt.Errorf("%w (draft preserved at: %s)", err, tmpPath))
				}
				if parsed.Topic != "" && parsed.Topic != t.TopicPath {
					_, _ = m.client.MoveItems([]string{t.ID}, parsed.Topic, m.resolveActor())
				}
				_ = os.Remove(tmpPath)
				return m.fetchTasksCmd()()
			})
		}
	}
	return m, nil
}

func (m Model) openNewTaskEditor(top string) (tea.Model, tea.Cmd) {
	tmpPath, err := editor.CreateNewTaskFileInTopic("", top)
	if err != nil {
		return m, func() tea.Msg { return errMsg(err) }
	}
	cmd := editor.BuildEditorCmd(m.editorCmd, tmpPath)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			_ = os.Remove(tmpPath)
			return errMsg(err)
		}
		data, err := os.ReadFile(tmpPath)
		if err != nil {
			return errMsg(err)
		}
		parsed, err := editor.ParseNewTaskContent(string(data))
		if err != nil {
			return errMsg(fmt.Errorf("%w (draft preserved at: %s)", err, tmpPath))
		}
		kind := domain.KindTask
		if m.isBoardView() {
			kind = domain.KindPost
		}
		destTopic := parsed.Topic
		if destTopic == "" {
			destTopic = top
		}
		_, err = m.client.CreateTask(protocol.CreateTaskPayload{
			Title:       parsed.Title,
			Description: parsed.Description,
			Status:      domain.StatusOpen,
			Priority:    parsed.Priority,
			Author:      m.resolveActor(),
			Assignee:    parsed.Assignee,
			Kind:        kind,
			Topic:       destTopic,
		})
		if err != nil {
			return errMsg(fmt.Errorf("%w (draft preserved at: %s)", err, tmpPath))
		}
		_ = os.Remove(tmpPath)
		return m.fetchTasksCmd()()
	})
}
