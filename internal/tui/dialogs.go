package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

func (m Model) getTaskTitlesForIDs(ids []string) []string {
	idMap := make(map[string]bool, len(ids))
	for _, id := range ids {
		idMap[id] = true
	}
	var res []string
	for _, t := range m.tasks {
		if idMap[t.ID] {
			res = append(res, fmt.Sprintf("%s: %s", t.ID, t.Title))
			if len(res) >= 5 {
				remaining := len(ids) - 5
				if remaining > 0 {
					res = append(res, fmt.Sprintf("...and %d more", remaining))
				}
				break
			}
		}
	}
	return res
}

func (m Model) activeConfirmDialog() *ConfirmDialog {
	if m.confirmDialog != nil {
		return m.confirmDialog
	}
	if m.confirmDelete {
		if m.deleteTaskID != "" {
			return &ConfirmDialog{
				Kind:      ConfirmDelete,
				Title:     "CONFIRM DELETION",
				Prompt:    fmt.Sprintf("Delete task %s permanently?", m.deleteTaskID),
				TargetIDs: []string{m.deleteTaskID},
			}
		}
		if len(m.selected) > 0 {
			ids := m.getSelectedIDs()
			return &ConfirmDialog{
				Kind:      ConfirmDelete,
				Title:     "CONFIRM BATCH DELETION",
				Prompt:    fmt.Sprintf("Delete %d selected tasks permanently?", len(ids)),
				TargetIDs: ids,
				IsBatch:   true,
			}
		}
	}
	return nil
}

func cloneTaskSnapshot(t, fullTask *domain.Task) TaskUndoSnapshot {
	if fullTask == nil {
		clone := *t
		if t.Tags != nil {
			clone.Tags = append([]string(nil), t.Tags...)
		}
		if t.Notes != nil {
			clone.Notes = append([]domain.TaskNote(nil), t.Notes...)
		}
		fullTask = &clone
	}
	var prevClosed *time.Time
	if t.ClosedAt != nil {
		tCopy := *t.ClosedAt
		prevClosed = &tCopy
	}
	var prevArchived *time.Time
	if t.ArchivedAt != nil {
		tCopy := *t.ArchivedAt
		prevArchived = &tCopy
	}
	return TaskUndoSnapshot{
		Task:               fullTask,
		PreviousStatus:     t.Status,
		PreviousAssignee:   t.Assignee,
		PreviousClosedAt:   prevClosed,
		PreviousArchivedAt: prevArchived,
	}
}

func (m *Model) pushUndo(kind UndoKind, desc string, snapshots ...TaskUndoSnapshot) {
	m.undoStack = append(m.undoStack, UndoAction{
		Kind:        kind,
		Description: desc,
		Snapshots:   snapshots,
	})
	if len(m.undoStack) > 50 {
		m.undoStack = m.undoStack[len(m.undoStack)-50:]
	}
}

func (m *Model) newBatchConfirmDialog(kind ConfirmKind, title, prompt string) *ConfirmDialog {
	ids := m.getSelectedIDs()
	return &ConfirmDialog{
		Kind:      kind,
		Title:     title,
		Prompt:    prompt,
		Details:   m.getTaskTitlesForIDs(ids),
		TargetIDs: ids,
		IsBatch:   true,
	}
}

func (m *Model) newSingleConfirmDialog(kind ConfirmKind, title, prompt string, t *domain.Task) *ConfirmDialog {
	return &ConfirmDialog{
		Kind:      kind,
		Title:     title,
		Prompt:    prompt,
		Details:   []string{fmt.Sprintf("%s: %s", t.ID, t.Title)},
		TargetIDs: []string{t.ID},
		IsBatch:   false,
	}
}

func (m Model) buildUndoAction(dlg *ConfirmDialog) UndoAction {
	var snapshots []TaskUndoSnapshot
	idMap := make(map[string]bool, len(dlg.TargetIDs))
	for _, id := range dlg.TargetIDs {
		idMap[id] = true
	}

	for _, t := range m.tasks {
		if !idMap[t.ID] {
			continue
		}
		var fullTask *domain.Task
		if dlg.Kind == ConfirmDelete && m.client != nil {
			if fetched, err := m.client.GetTask(t.ID); err == nil && fetched != nil {
				fullTask = fetched
			}
		}
		snapshots = append(snapshots, cloneTaskSnapshot(t, fullTask))
	}

	var kind UndoKind
	var desc string
	switch dlg.Kind {
	case ConfirmDelete:
		kind = UndoDelete
		if dlg.IsBatch {
			desc = fmt.Sprintf("Delete %d tasks", len(dlg.TargetIDs))
		} else if len(dlg.TargetIDs) > 0 {
			desc = fmt.Sprintf("Delete %s", dlg.TargetIDs[0])
		}
	case ConfirmClaim:
		kind = UndoClaim
		if dlg.IsBatch {
			desc = fmt.Sprintf("Claim %d tasks", len(dlg.TargetIDs))
		} else if len(dlg.TargetIDs) > 0 {
			desc = fmt.Sprintf("Claim %s", dlg.TargetIDs[0])
		}
	case ConfirmDone, ConfirmForceDone, ConfirmReopen, ConfirmStatus:
		kind = UndoStatusChange
		if dlg.IsBatch {
			desc = fmt.Sprintf("Status change on %d tasks", len(dlg.TargetIDs))
		} else if len(dlg.TargetIDs) > 0 {
			desc = fmt.Sprintf("Status change on %s", dlg.TargetIDs[0])
		}
	}

	return UndoAction{
		Kind:        kind,
		Description: desc,
		Snapshots:   snapshots,
	}
}

func (m Model) executeConfirmedDialog(dlg *ConfirmDialog) (tea.Model, tea.Cmd) {
	switch dlg.Kind {
	case ConfirmDelete:
		if dlg.IsBatch {
			m.statusMsg = fmt.Sprintf("Deleted %d tasks (press 'u' to undo)", len(dlg.TargetIDs))
			return m, m.batchDeleteCmd(dlg.TargetIDs)
		}
		if len(dlg.TargetIDs) > 0 {
			m.statusMsg = fmt.Sprintf("Deleted %s (press 'u' to undo)", dlg.TargetIDs[0])
			return m, m.deleteTaskCmd(dlg.TargetIDs[0])
		}

	case ConfirmDone:
		if dlg.IsBatch {
			m.statusMsg = fmt.Sprintf("Marked %d tasks as DONE (press 'u' to undo)", len(dlg.TargetIDs))
			return m, m.batchDoneCmd(dlg.TargetIDs)
		}
		if len(dlg.TargetIDs) > 0 {
			m.statusMsg = fmt.Sprintf("Marked %s as DONE (press 'u' to undo)", dlg.TargetIDs[0])
			return m, m.markDoneCmd(dlg.TargetIDs[0])
		}

	case ConfirmForceDone:
		if dlg.IsBatch {
			m.statusMsg = fmt.Sprintf("Force marked %d tasks as DONE (press 'u' to undo)", len(dlg.TargetIDs))
			return m, m.batchDoneWithForceCmd(dlg.TargetIDs, true)
		}
		if len(dlg.TargetIDs) > 0 {
			m.statusMsg = fmt.Sprintf("Force marked %s as DONE (press 'u' to undo)", dlg.TargetIDs[0])
			return m, m.markDoneWithForceCmd(dlg.TargetIDs[0], true)
		}

	case ConfirmReopen:
		if len(dlg.TargetIDs) > 0 {
			m.statusMsg = fmt.Sprintf("Reopened %s (press 'u' to undo)", dlg.TargetIDs[0])
			return m, m.reopenTaskCmd(dlg.TargetIDs[0])
		}

	case ConfirmStatus:
		if dlg.IsBatch {
			m.statusMsg = fmt.Sprintf("Cycled status on %d tasks (press 'u' to undo)", len(dlg.TargetIDs))
			return m, m.batchStatusCmd(dlg.TargetIDs)
		}
		if len(dlg.TargetIDs) > 0 {
			m.statusMsg = fmt.Sprintf("Updated %s status to %s (press 'u' to undo)", dlg.TargetIDs[0], dlg.TargetStatus)
			return m, m.updateStatusCmd(dlg.TargetIDs[0], dlg.TargetStatus)
		}

	case ConfirmClaim:
		if dlg.IsBatch {
			m.statusMsg = fmt.Sprintf("Claimed %d tasks (press 'u' to undo)", len(dlg.TargetIDs))
			return m, m.batchClaimCmd(dlg.TargetIDs)
		}
		if len(dlg.TargetIDs) > 0 {
			m.statusMsg = fmt.Sprintf("Claimed %s as %s (press 'u' to undo)", dlg.TargetIDs[0], dlg.TargetAssignee)
			return m, m.claimTaskCmd(dlg.TargetIDs[0])
		}

	case ConfirmDeleteTopic:
		m.statusMsg = fmt.Sprintf("Deleting topic @%s...", dlg.TopicRef)
		return m, m.deleteTopicCmd(dlg.TopicRef, dlg.MoveTo)
	}
	return m, nil
}

func (m Model) executeUndoCmd(undo UndoAction) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		switch undo.Kind {
		case UndoStatusChange:
			for _, snap := range undo.Snapshots {
				note := fmt.Sprintf("Undo: reverted status to %s", snap.PreviousStatus)
				_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
					ID:      snap.Task.ID,
					Status:  &snap.PreviousStatus,
					AddNote: &note,
				})
				if err != nil {
					return errMsg(fmt.Errorf("failed to undo status for %s: %w", snap.Task.ID, err))
				}
			}
		case UndoClaim:
			for _, snap := range undo.Snapshots {
				note := fmt.Sprintf("Undo: reverted claim to %q", snap.PreviousAssignee)
				prevAssignee := snap.PreviousAssignee
				_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
					ID:       snap.Task.ID,
					Status:   &snap.PreviousStatus,
					Assignee: &prevAssignee,
					AddNote:  &note,
				})
				if err != nil {
					return errMsg(fmt.Errorf("failed to undo claim for %s: %w", snap.Task.ID, err))
				}
			}
		case UndoDelete:
			for _, snap := range undo.Snapshots {
				_, err := m.client.RestoreTask(snap.Task)
				if err != nil {
					return errMsg(fmt.Errorf("failed to restore %s: %w", snap.Task.ID, err))
				}
			}
		case UndoArchive:
			for _, snap := range undo.Snapshots {
				note := "Undo: reverted archive state"
				prevStatus := snap.PreviousStatus
				prevAssignee := snap.PreviousAssignee
				_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
					ID:       snap.Task.ID,
					Status:   &prevStatus,
					Assignee: &prevAssignee,
					AddNote:  &note,
				})
				if err != nil {
					return errMsg(fmt.Errorf("failed to undo archive for %s: %w", snap.Task.ID, err))
				}
			}
		}
		return m.reloadAfter(nil)
	}
}
