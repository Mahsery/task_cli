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
	maxRows := m.getMaxRows()
	if dlg := m.activeConfirmDialog(); dlg != nil {
		k := msg.String()
		if k == "y" || k == "Y" || k == "enter" {
			undo := m.buildUndoAction(dlg)
			m.undoStack = append(m.undoStack, undo)
			if len(m.undoStack) > 50 {
				m.undoStack = m.undoStack[len(m.undoStack)-50:]
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
			return m, m.fetchTasksCmd()
		case "esc":
			m.isSearch = false
			m.filterText = ""
			return m, m.fetchTasksCmd()
		case "backspace":
			runes := []rune(m.filterText)
			if len(runes) > 0 {
				m.filterText = string(runes[:len(runes)-1])
				return m, m.fetchTasksCmd()
			}
		default:
			if len(msg.Runes) > 0 {
				m.filterText += string(msg.Runes)
				return m, m.fetchTasksCmd()
			}
		}
		return m, nil
	}

	if m.isLinking {
		switch msg.String() {
		case "enter":
			m.isLinking = false
			prereqID := strings.TrimSpace(m.linkInput)
			if prereqID != "" {
				targetID := m.linkTaskID
				return m, m.linkTaskCmd(targetID, prereqID)
			}
			m.statusMsg = "Link cancelled"
			return m, nil
		case "esc":
			m.isLinking = false
			m.statusMsg = "Link cancelled"
			return m, nil
		case "backspace":
			runes := []rune(m.linkInput)
			if len(runes) > 0 {
				m.linkInput = string(runes[:len(runes)-1])
			}
			return m, nil
		default:
			if len(msg.Runes) > 0 {
				m.linkInput += string(msg.Runes)
			}
			return m, nil
		}
	}

	if m.isTagging {
		switch msg.String() {
		case "enter":
			m.isTagging = false
			tag := strings.TrimSpace(m.tagInput)
			if tag != "" {
				if m.tagTaskID != "" {
					targetID := m.tagTaskID
					cur := m.currentTask()
					if cur != nil && cur.ID == targetID {
						prevTags := append([]string(nil), cur.Tags...)
						exists := false
						for _, t := range cur.Tags {
							if t == tag {
								exists = true
								break
							}
						}
						var newTags []string
						if exists {
							for _, t := range cur.Tags {
								if t != tag {
									newTags = append(newTags, t)
								}
							}
							m.statusMsg = fmt.Sprintf("Removed tag %q from %s (press 'u' to undo)", tag, targetID)
						} else {
							newTags = append(append(make([]string, 0, len(cur.Tags)+1), cur.Tags...), tag)
							m.statusMsg = fmt.Sprintf("Tagged %s with %q (press 'u' to undo)", targetID, tag)
						}
						cur.Tags = newTags

						undo := UndoAction{
							Kind:        UndoTag,
							Description: fmt.Sprintf("Tag change on %s", targetID),
							Snapshots: []TaskUndoSnapshot{
								{
									Task:         cur,
									PreviousTags: prevTags,
								},
							},
						}
						m.undoStack = append(m.undoStack, undo)
						if len(m.undoStack) > 50 {
							m.undoStack = m.undoStack[len(m.undoStack)-50:]
						}
						return m, m.updateTaskTagsCmd(targetID, newTags)
					}
				} else if len(m.selected) > 0 {
					ids := m.getSelectedIDs()
					var snapshots []TaskUndoSnapshot
					for _, id := range ids {
						for _, t := range m.tasks {
							if t.ID != id {
								continue
							}
							prevTags := append([]string(nil), t.Tags...)
							snapshots = append(snapshots, TaskUndoSnapshot{
								Task:         t,
								PreviousTags: prevTags,
							})
							exists := false
							for _, exTag := range t.Tags {
								if exTag == tag {
									exists = true
									break
								}
							}
							if !exists {
								t.Tags = append(t.Tags, tag)
							}
							break
						}
					}
					undo := UndoAction{
						Kind:        UndoTag,
						Description: fmt.Sprintf("Tag %q on %d tasks", tag, len(ids)),
						Snapshots:   snapshots,
					}
					m.undoStack = append(m.undoStack, undo)
					if len(m.undoStack) > 50 {
						m.undoStack = m.undoStack[len(m.undoStack)-50:]
					}
					m.statusMsg = fmt.Sprintf("Tagged %d tasks with %q (press 'u' to undo)", len(ids), tag)
					return m, m.batchTagCmd(ids, tag)
				}
			}
			m.statusMsg = "Tagging cancelled"
			return m, nil

		case "esc":
			m.isTagging = false
			m.statusMsg = "Tagging cancelled"
			return m, nil

		case "backspace":
			runes := []rune(m.tagInput)
			if len(runes) > 0 {
				m.tagInput = string(runes[:len(runes)-1])
			}
			return m, nil

		default:
			if len(msg.Runes) > 0 {
				m.tagInput += string(msg.Runes)
			}
			return m, nil
		}
	}

	// Vim Movement Chords: Accumulate numeric prefix in normal navigation mode
	kStr := msg.String()
	if len(kStr) == 1 && kStr[0] >= '0' && kStr[0] <= '9' {
		if kStr[0] != '0' || m.chordBuffer != "" {
			m.chordBuffer += kStr
			return m, nil
		}
	}

	if (kStr == "esc" || kStr == "c") && m.chordBuffer != "" {
		m.chordBuffer = ""
		m.statusMsg = "Chord cancelled"
		return m, nil
	}

	var chordCount int
	if m.chordBuffer != "" {
		// A non-numeric buffer simply leaves chordCount at 0 (no repeat prefix).
		_, _ = fmt.Sscanf(m.chordBuffer, "%d", &chordCount)
		m.chordBuffer = ""
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		if m.activePane == PaneList {
			m.activePane = PaneDetail
			maxOffset := m.maxDetailScrollOffset()
			if m.detailScrollOffset > maxOffset {
				m.detailScrollOffset = maxOffset
			}
			m.statusMsg = "Details focused"
		} else {
			m.activePane = PaneList
			m.statusMsg = "Tasks focused"
		}
		return m, nil

	case "shift+tab":
		if m.activePane == PaneDetail {
			m.activePane = PaneList
			m.statusMsg = "Tasks focused"
		} else {
			m.activePane = PaneDetail
			maxOffset := m.maxDetailScrollOffset()
			if m.detailScrollOffset > maxOffset {
				m.detailScrollOffset = maxOffset
			}
			m.statusMsg = "Details focused"
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
		if len(m.tasks) > 0 {
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
		if len(m.tasks) > 0 {
			m.cursor += step
			if m.cursor >= len(m.tasks) {
				m.cursor = len(m.tasks) - 1
			}
			if m.cursor >= m.scrollOffset+maxRows {
				m.scrollOffset = m.cursor - maxRows + 1
			}
			m.detailScrollOffset = 0
		}

	case "g", "home":
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
		if len(m.tasks) > 0 {
			if chordCount > 0 {
				target := chordCount - 1
				if target >= len(m.tasks) {
					target = len(m.tasks) - 1
				}
				if target < 0 {
					target = 0
				}
				m.cursor = target
			} else {
				m.cursor = len(m.tasks) - 1
			}
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
			if m.cursor >= m.scrollOffset+maxRows {
				m.scrollOffset = m.cursor - maxRows + 1
			}
			m.detailScrollOffset = 0
		}

	case "J", "ctrl+d", "]", "pgdown":
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
		if len(m.tasks) > 0 {
			step := (maxRows / 2) * times
			if step < 1 {
				step = 1
			}
			m.cursor += step
			if m.cursor >= len(m.tasks) {
				m.cursor = len(m.tasks) - 1
			}
			if m.cursor >= m.scrollOffset+maxRows {
				m.scrollOffset = m.cursor - maxRows + 1
			}
			m.detailScrollOffset = 0
		}

	case "K", "ctrl+u", "[", "pgup":
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
		if len(m.tasks) > 0 {
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
		// Multi-select toggle: toggle selection for current task, then advance cursor down
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			t := m.tasks[m.cursor]
			if m.selected == nil {
				m.selected = make(map[string]bool)
			}
			if m.selected[t.ID] {
				delete(m.selected, t.ID)
			} else {
				m.selected[t.ID] = true
			}
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
				if m.cursor >= m.scrollOffset+maxRows {
					m.scrollOffset = m.cursor - maxRows + 1
				}
			}
			m.detailScrollOffset = 0
		}

	case "v":
		// Toggle selection for current task without advancing cursor
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			t := m.tasks[m.cursor]
			if m.selected == nil {
				m.selected = make(map[string]bool)
			}
			if m.selected[t.ID] {
				delete(m.selected, t.ID)
			} else {
				m.selected[t.ID] = true
			}
		}

	case "V":
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
		if m.scope == "local" {
			m.scope = "global"
		} else {
			m.scope = "local"
		}
		m.client.Scope = m.scope
		m.statusMsg = fmt.Sprintf("Switched to %s scope", m.scope)
		return m, m.fetchTasksCmd()

	case "r":
		m.statusMsg = "Refreshed"
		return m, m.fetchTasksCmd()

	case "/":
		m.isSearch = true
		m.prevFilter = m.filterText
		m.filterText = ""
		return m, nil

	case "enter":
		m.showDiff = !m.showDiff
		maxOffset := m.maxDetailScrollOffset()
		if m.detailScrollOffset > maxOffset {
			m.detailScrollOffset = maxOffset
		}
		if m.showDiff && len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			t := m.tasks[m.cursor]
			if t.DiffContent == "" && t.CommitSHA != "" {
				return m, m.fetchTaskDetailCmd(t.ID)
			}
		}
		return m, nil

	case "x":
		// Batch delete prompt if tasks selected, otherwise single-task
		if len(m.selected) > 0 {
			m.confirmDialog = m.newBatchConfirmDialog(ConfirmDelete, "CONFIRM BATCH DELETION", fmt.Sprintf("Permanently delete %d selected tasks?", len(m.selected)))
			return m, nil
		}
		if cur := m.currentTask(); cur != nil {
			m.confirmDialog = m.newSingleConfirmDialog(ConfirmDelete, "CONFIRM DELETION", fmt.Sprintf("Permanently delete task %s?", cur.ID), cur)
			return m, nil
		}

	case "a":
		actor := m.resolveActor()
		if len(m.selected) > 0 {
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

	case "A", "O":
		if len(m.selected) > 0 {
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
					var prevArchived *time.Time
					if t.ArchivedAt != nil {
						c := *t.ArchivedAt
						prevArchived = &c
					}
					snapshots = append(snapshots, TaskUndoSnapshot{
						Task:               t,
						PreviousStatus:     t.Status,
						PreviousAssignee:   t.Assignee,
						PreviousClosedAt:   t.ClosedAt,
						PreviousArchivedAt: prevArchived,
						PreviousTags:       append([]string(nil), t.Tags...),
					})
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
			undo := UndoAction{
				Kind:        UndoArchive,
				Description: fmt.Sprintf("%s %d tasks", actionDesc, len(ids)),
				Snapshots:   snapshots,
			}
			m.undoStack = append(m.undoStack, undo)
			if len(m.undoStack) > 50 {
				m.undoStack = m.undoStack[len(m.undoStack)-50:]
			}
			m.statusMsg = fmt.Sprintf("%s %d tasks (press 'u' to undo)", actionDesc, len(ids))
			return m, m.batchArchiveCmd(ids, targetArchive)
		}
		cur := m.currentTask()
		if cur != nil {
			now := time.Now()
			var prevArchived *time.Time
			if cur.ArchivedAt != nil {
				c := *cur.ArchivedAt
				prevArchived = &c
			}
			undo := UndoAction{
				Kind: UndoArchive,
				Snapshots: []TaskUndoSnapshot{
					{
						Task:               cur,
						PreviousStatus:     cur.Status,
						PreviousAssignee:   cur.Assignee,
						PreviousClosedAt:   cur.ClosedAt,
						PreviousArchivedAt: prevArchived,
						PreviousTags:       append([]string(nil), cur.Tags...),
					},
				},
			}
			if cur.IsArchived() {
				undo.Description = fmt.Sprintf("Unarchived %s", cur.ID)
				m.undoStack = append(m.undoStack, undo)
				if len(m.undoStack) > 50 {
					m.undoStack = m.undoStack[len(m.undoStack)-50:]
				}
				cur.ArchivedAt = nil
				cur.Status = domain.StatusOpen
				m.statusMsg = fmt.Sprintf("Unarchived %s (press 'u' to undo)", cur.ID)
				return m, m.unarchiveTaskCmd(cur.ID)
			}
			undo.Description = fmt.Sprintf("Archived %s", cur.ID)
			m.undoStack = append(m.undoStack, undo)
			if len(m.undoStack) > 50 {
				m.undoStack = m.undoStack[len(m.undoStack)-50:]
			}
			cur.ArchivedAt = &now
			cur.Status = domain.StatusArchived
			m.statusMsg = fmt.Sprintf("Archived %s (press 'u' to undo)", cur.ID)
			return m, m.archiveTaskCmd(cur.ID)
		}
		return m, nil

	case "t":
		m.treeViewMode = !m.treeViewMode
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
			m.statusMsg = fmt.Sprintf("Linking %s (enter prerequisite ID)", cur.ID)
		}
		return m, nil

	case "d":
		// Batch Done if tasks selected, otherwise single-task reversible done
		if len(m.selected) > 0 {
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
				m.statusMsg = fmt.Sprintf("⚠️ Cannot mark done: %d task(s) are blocked by prerequisites (%s)", len(blockedIDs), strings.Join(blockedIDs, ", "))
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
				m.statusMsg = fmt.Sprintf("⚠️ Cannot mark %s DONE: blocked by unfinished prerequisite(s): %s", cur.ID, strings.Join(cur.BlockedBy, ", "))
				return m, nil
			}
			m.pushUndo(UndoStatusChange, fmt.Sprintf("Marked %s as DONE", cur.ID), cloneTaskSnapshot(cur, nil))
			cur.Status = domain.StatusDone
			m.statusMsg = fmt.Sprintf("Marked %s as DONE (press 'u' to undo)", cur.ID)
			return m, m.markDoneCmd(cur.ID)
		}

	case "s":
		// Batch status toggle if tasks selected, otherwise single-task toggle
		if len(m.selected) > 0 {
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

	case "m", "T":
		if len(m.selected) > 0 {
			m.isTagging = true
			m.tagInput = ""
			m.tagTaskID = ""
			m.statusMsg = fmt.Sprintf("Tagging %d selected tasks (enter tag name)", len(m.selected))
			return m, nil
		}
		cur := m.currentTask()
		if cur != nil {
			m.isTagging = true
			m.tagInput = ""
			m.tagTaskID = cur.ID
			m.statusMsg = fmt.Sprintf("Tagging %s (enter tag name)", cur.ID)
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
		case UndoTag:
			for _, snap := range lastUndo.Snapshots {
				if snap.Task != nil {
					snap.Task.Tags = append([]string(nil), snap.PreviousTags...)
				}
				for _, t := range m.tasks {
					if snap.Task != nil && t.ID == snap.Task.ID {
						t.Tags = append([]string(nil), snap.PreviousTags...)
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
		// Create new task via editor
		tmpPath, err := editor.CreateNewTaskFile("")
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
			_, err = m.client.CreateTask(protocol.CreateTaskPayload{
				Title:       parsed.Title,
				Description: parsed.Description,
				Status:      domain.StatusOpen,
				Priority:    parsed.Priority,
				Tags:        parsed.Tags,
				Author:      m.resolveActor(),
				Assignee:    parsed.Assignee,
			})
			if err != nil {
				return errMsg(fmt.Errorf("%w (draft preserved at: %s)", err, tmpPath))
			}
			_ = os.Remove(tmpPath)
			return m.fetchTasksCmd()()
		})

	case "e":
		// Edit existing task via editor
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			t := m.tasks[m.cursor]
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
					Tags:        &parsed.Tags,
					Assignee:    &parsed.Assignee,
					AddNote:     addNote,
					Author:      m.resolveActor(),
				})
				if err != nil {
					return errMsg(fmt.Errorf("%w (draft preserved at: %s)", err, tmpPath))
				}
				_ = os.Remove(tmpPath)
				return m.fetchTasksCmd()()
			})
		}
	}
	return m, nil
}
