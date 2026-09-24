package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"task_cli/internal/client"
	"task_cli/internal/domain"
	"task_cli/internal/editor"
	"task_cli/internal/git"
	"task_cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Pane int

const (
	PaneList Pane = iota
	PaneDetail
)

type ConfirmKind int

const (
	ConfirmDelete ConfirmKind = iota
	ConfirmDone
	ConfirmReopen
	ConfirmStatus
	ConfirmClaim
)

type ConfirmDialog struct {
	Kind           ConfirmKind
	Title          string
	Prompt         string
	Details        []string
	TargetIDs      []string
	IsBatch        bool
	TargetStatus   domain.Status
	TargetAssignee string
}

type UndoKind int

const (
	UndoStatusChange UndoKind = iota
	UndoClaim
	UndoDelete
)

type TaskUndoSnapshot struct {
	Task             *domain.Task
	PreviousStatus   domain.Status
	PreviousAssignee string
	PreviousClosedAt *time.Time
}

type UndoAction struct {
	Kind        UndoKind
	Description string
	Snapshots   []TaskUndoSnapshot
}

type Model struct {
	client             *client.Client
	editorCmd          string
	theme              Theme
	tasks              []*domain.Task
	selected           map[string]bool
	activePane         Pane
	cursor             int
	scrollOffset       int
	detailScrollOffset int
	width              int
	height             int
	scope              string
	repoPath           string
	filterText         string
	prevFilter         string
	isSearch           bool
	statusMsg          string
	err                error
	showDiff           bool
	confirmDelete      bool
	deleteTaskID       string
	confirmDialog      *ConfirmDialog
	undoStack          []UndoAction
}

func NewModel(c *client.Client, editorCmd, scope, repoPath string) Model {
	return Model{
		client:             c,
		editorCmd:          editorCmd,
		theme:              NewKanagawaTheme(),
		scope:              scope,
		repoPath:           repoPath,
		selected:           make(map[string]bool),
		activePane:         PaneList,
		cursor:             0,
		scrollOffset:       0,
		detailScrollOffset: 0,
	}
}

type reloadMsg []*domain.Task
type taskDetailMsg *domain.Task
type statusMsg string
type errMsg error

func (m Model) Init() tea.Cmd {
	return m.fetchTasksCmd()
}

func (m Model) fetchTaskDetailCmd(id string) tea.Cmd {
	return func() tea.Msg {
		task, err := m.client.GetTask(id)
		if err != nil {
			return nil
		}
		return taskDetailMsg(task)
	}
}

func (m Model) fetchTasksCmd() tea.Cmd {
	return func() tea.Msg {
		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, err := m.client.ListTasks(filter)
		if err != nil {
			return errMsg(err)
		}
		return reloadMsg(tasks)
	}
}

func (m Model) isSplitView() bool {
	return m.width >= 110
}

func (m Model) getMaxRows() int {
	if m.height <= 0 {
		return 8
	}
	if m.isSplitView() {
		bodyHeight := m.height - 4
		rows := bodyHeight - 2
		if rows < 4 {
			return 4
		}
		if rows > 40 {
			return 40
		}
		return rows
	}

	detailReserved := 10
	if m.showDiff {
		detailReserved = 16
	}
	rows := m.height - 6 - detailReserved
	if rows < 4 {
		return 4
	}
	if rows > 25 {
		return 25
	}
	return rows
}

func (m Model) getDetailDimensions() (detailWidth, visibleBody int) {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 24
	}

	dividerWidth := 1
	tableWidth := width * 48 / 100
	if tableWidth < 52 {
		tableWidth = 52
	}
	if tableWidth > 75 {
		tableWidth = 75
	}
	detailWidth = width - tableWidth - dividerWidth
	if detailWidth < 20 {
		detailWidth = 20
	}

	bodyHeight := height - 4
	if bodyHeight < 10 {
		bodyHeight = 10
	}
	visibleBody = bodyHeight - 1 // 1 line reserved for pinned header
	if visibleBody < 1 {
		visibleBody = 1
	}
	return detailWidth, visibleBody
}

func (m Model) getDetailContentLines(detailWidth int) []string {
	if len(m.tasks) == 0 || m.cursor >= len(m.tasks) {
		return nil
	}
	t := m.tasks[m.cursor]
	theme := m.theme
	var contentLines []string

	// Latest update badge
	var latestBadge string
	if len(t.Notes) > 0 {
		lastNote := t.Notes[len(t.Notes)-1]
		author := lastNote.Author
		if author == "" {
			author = "actor"
		}
		latestBadge = fmt.Sprintf(" Latest Note: %s by %s", timeAgo(lastNote.CreatedAt), author)
	} else if t.CommitSHA != "" {
		latestBadge = fmt.Sprintf(" Resolved by %s (%s)", t.CommitSHA, timeAgo(t.UpdatedAt))
	} else if !t.UpdatedAt.IsZero() && t.UpdatedAt != t.CreatedAt {
		latestBadge = fmt.Sprintf(" Updated: %s", timeAgo(t.UpdatedAt))
	} else {
		latestBadge = fmt.Sprintf(" Created: %s", timeAgo(t.CreatedAt))
	}
	if lipgloss.Width(latestBadge) > detailWidth {
		latestBadge = latestBadge[:detailWidth]
	}
	contentLines = append(contentLines, theme.NoteTime.Width(detailWidth).Render(latestBadge))
	contentLines = append(contentLines, strings.Repeat(" ", detailWidth))

	// Task Title
	for _, tl := range wrapText(t.ID+": "+t.Title, detailWidth-4, "  ") {
		contentLines = append(contentLines, theme.DetailTitle.Width(detailWidth).Render(tl))
	}

	// Metadata badges
	meta1 := fmt.Sprintf("  %s %s   %s %s   %s %s",
		theme.DetailLabel.Render("Status:"), StatusStyle(t.Status).Render(string(t.Status)),
		theme.DetailLabel.Render("Pri:"), PriorityStyle(t.Priority).Render(string(t.Priority)),
		theme.DetailLabel.Render("Scope:"), theme.TaskID.Render(t.Scope),
	)
	var metaParts []string
	if t.Assignee != "" {
		metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Assignee:"), theme.NoteHeader.Render(t.Assignee)))
	}
	if len(t.Tags) > 0 {
		metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Tags:"), theme.Tag.Render(strings.Join(t.Tags, ", "))))
	}
	fullMeta := meta1
	if len(metaParts) > 0 {
		fullMeta += "   " + strings.Join(metaParts, "   ")
	}
	if lipgloss.Width(fullMeta) <= detailWidth {
		contentLines = append(contentLines, fullMeta)
	} else {
		contentLines = append(contentLines, meta1)
		if len(metaParts) > 0 {
			for _, mp := range wrapText(strings.Join(metaParts, "   "), detailWidth-4, "  ") {
				contentLines = append(contentLines, mp)
			}
		}
	}
	contentLines = append(contentLines, strings.Repeat(" ", detailWidth))

	// Description
	if t.Description != "" {
		descLines := wrapText(t.Description, detailWidth-4, "  ")
		for _, dl := range descLines {
			contentLines = append(contentLines, theme.DetailDesc.Width(detailWidth).Render(dl))
		}
	}

	// Commit resolution & diff
	if t.CommitSHA != "" {
		contentLines = append(contentLines, strings.Repeat(" ", detailWidth))
		resText := fmt.Sprintf("✓ Resolved by commit %s: %s", t.CommitSHA, t.CommitMsg)
		for _, rl := range wrapText(resText, detailWidth-4, "  ") {
			contentLines = append(contentLines, theme.DetailResolved.Width(detailWidth).Render(rl))
		}
		if t.DiffStat != "" {
			for _, sl := range strings.Split(strings.TrimSpace(t.DiffStat), "\n") {
				contentLines = append(contentLines, theme.DiffStat.Width(detailWidth).Render("    "+strings.TrimSpace(sl)))
			}
		}
		if m.showDiff && t.DiffContent != "" {
			contentLines = append(contentLines, theme.Border.Width(detailWidth).Render("  ── Diff Content ──"))
			diffLines := strings.Split(t.DiffContent, "\n")
			for _, dl := range diffLines {
				if strings.HasPrefix(dl, "+") {
					contentLines = append(contentLines, theme.DiffAdd.Width(detailWidth).Render("    "+dl))
				} else if strings.HasPrefix(dl, "-") {
					contentLines = append(contentLines, theme.DiffSub.Width(detailWidth).Render("    "+dl))
				} else if strings.HasPrefix(dl, "@@") {
					contentLines = append(contentLines, theme.DiffHunk.Width(detailWidth).Render("    "+dl))
				} else {
					contentLines = append(contentLines, theme.DetailDesc.Width(detailWidth).Render("    "+dl))
				}
			}
		}
	}

	// Activity Notes
	if len(t.Notes) > 0 {
		contentLines = append(contentLines, strings.Repeat(" ", detailWidth))
		contentLines = append(contentLines, theme.NoteHeader.Width(detailWidth).Render(fmt.Sprintf("  • Notes & Messages (%d):", len(t.Notes))))
		for _, n := range t.Notes {
			timeStr := theme.NoteTime.Render(timeAgo(n.CreatedAt))
			authorStr := ""
			if n.Author != "" {
				availAuthor := detailWidth - 18
				author := n.Author
				if availAuthor > 6 && len(author) > availAuthor {
					author = author[:availAuthor-2] + ".."
				}
				authorStr = " by " + theme.NoteAuthor.Render(author)
			}
			contentLines = append(contentLines, fmt.Sprintf("    • %s%s:", timeStr, authorStr))
			for _, line := range wrapText(n.Content, detailWidth-6, "      ") {
				contentLines = append(contentLines, theme.NoteContent.Width(detailWidth).Render(line))
			}
		}
	}

	return contentLines
}

func (m Model) maxDetailScrollOffset() int {
	if len(m.tasks) == 0 || m.cursor >= len(m.tasks) {
		return 0
	}
	detailWidth, visibleBody := m.getDetailDimensions()
	contentLines := m.getDetailContentLines(detailWidth)
	maxOffset := len(contentLines) - visibleBody
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

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

func (m Model) buildUndoAction(dlg *ConfirmDialog) UndoAction {
	var snapshots []TaskUndoSnapshot
	idMap := make(map[string]bool, len(dlg.TargetIDs))
	for _, id := range dlg.TargetIDs {
		idMap[id] = true
	}

	for _, t := range m.tasks {
		if idMap[t.ID] {
			clone := *t
			if t.Tags != nil {
				clone.Tags = append([]string(nil), t.Tags...)
			}
			if t.Notes != nil {
				clone.Notes = append([]domain.TaskNote(nil), t.Notes...)
			}
			var prevClosed *time.Time
			if t.ClosedAt != nil {
				tCopy := *t.ClosedAt
				prevClosed = &tCopy
			}
			snapshots = append(snapshots, TaskUndoSnapshot{
				Task:             &clone,
				PreviousStatus:   t.Status,
				PreviousAssignee: t.Assignee,
				PreviousClosedAt: prevClosed,
			})
		}
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
	case ConfirmDone, ConfirmReopen, ConfirmStatus:
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
	}
	return m, nil
}

func (m Model) executeUndoCmd(undo UndoAction) tea.Cmd {
	return func() tea.Msg {
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
		}
		tasks, err := m.client.ListTasks(domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		})
		if err != nil {
			return errMsg(err)
		}
		return reloadMsg(tasks)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	maxRows := m.getMaxRows()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		maxOffset := m.maxDetailScrollOffset()
		if m.detailScrollOffset > maxOffset {
			m.detailScrollOffset = maxOffset
		}
		maxScrollOffset := len(m.tasks) - maxRows
		if maxScrollOffset < 0 {
			maxScrollOffset = 0
		}
		if m.scrollOffset > maxScrollOffset {
			m.scrollOffset = maxScrollOffset
		}
		return m, nil

	case reloadMsg:
		m.tasks = msg
		if m.cursor >= len(m.tasks) {
			if len(m.tasks) > 0 {
				m.cursor = len(m.tasks) - 1
			} else {
				m.cursor = 0
			}
		}
		if m.cursor < m.scrollOffset {
			m.scrollOffset = m.cursor
		}
		maxScrollOffset := len(m.tasks) - maxRows
		if maxScrollOffset < 0 {
			maxScrollOffset = 0
		}
		if m.scrollOffset > maxScrollOffset {
			m.scrollOffset = maxScrollOffset
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		maxDetailOffset := m.maxDetailScrollOffset()
		if m.detailScrollOffset > maxDetailOffset {
			m.detailScrollOffset = maxDetailOffset
		}
		// Prune stale selected IDs that no longer exist
		if len(m.selected) > 0 {
			taskSet := make(map[string]bool, len(m.tasks))
			for _, t := range m.tasks {
				taskSet[t.ID] = true
			}
			for id := range m.selected {
				if !taskSet[id] {
					delete(m.selected, id)
				}
			}
		}
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
			maxOffset := m.maxDetailScrollOffset()
			if m.detailScrollOffset > maxOffset {
				m.detailScrollOffset = maxOffset
			}
		}
		return m, nil

	case statusMsg:
		m.statusMsg = string(msg)
		return m, nil

	case errMsg:
		m.err = msg
		m.statusMsg = fmt.Sprintf("Error: %v", msg)
		return m, nil

	case tea.KeyMsg:
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
				m.statusMsg = "Focused Details pane (j/k to scroll content)"
			} else {
				m.activePane = PaneList
				m.statusMsg = "Focused Tasks list (j/k to select task)"
			}
			return m, nil

		case "shift+tab":
			if m.activePane == PaneDetail {
				m.activePane = PaneList
				m.statusMsg = "Focused Tasks list (j/k to select task)"
			} else {
				m.activePane = PaneDetail
				maxOffset := m.maxDetailScrollOffset()
				if m.detailScrollOffset > maxOffset {
					m.detailScrollOffset = maxOffset
				}
				m.statusMsg = "Focused Details pane (j/k to scroll content)"
			}
			return m, nil

		case "l", "right":
			if m.activePane == PaneList && len(m.tasks) > 0 {
				m.activePane = PaneDetail
				maxOffset := m.maxDetailScrollOffset()
				if m.detailScrollOffset > maxOffset {
					m.detailScrollOffset = maxOffset
				}
				m.statusMsg = "Focused Details pane (j/k to scroll content)"
				return m, nil
			}

		case "h", "left":
			if m.activePane == PaneDetail {
				m.activePane = PaneList
				m.statusMsg = "Focused Tasks list (j/k to select task)"
				return m, nil
			}

		case "up", "k":
			if m.activePane == PaneDetail {
				maxOffset := m.maxDetailScrollOffset()
				if m.detailScrollOffset > maxOffset {
					m.detailScrollOffset = maxOffset
				}
				if m.detailScrollOffset > 0 {
					m.detailScrollOffset--
				}
				return m, nil
			}
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scrollOffset {
					m.scrollOffset = m.cursor
				}
				m.detailScrollOffset = 0
			}

		case "down", "j":
			if m.activePane == PaneDetail {
				maxOffset := m.maxDetailScrollOffset()
				if m.detailScrollOffset < maxOffset {
					m.detailScrollOffset++
				}
				return m, nil
			}
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
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
				m.detailScrollOffset = m.maxDetailScrollOffset()
				return m, nil
			}
			if len(m.tasks) > 0 {
				m.cursor = len(m.tasks) - 1
				if m.cursor >= m.scrollOffset+maxRows {
					m.scrollOffset = m.cursor - maxRows + 1
				}
				m.detailScrollOffset = 0
			}

		case "J", "ctrl+d", "]", "pgdown":
			if m.activePane == PaneDetail {
				maxOffset := m.maxDetailScrollOffset()
				m.detailScrollOffset += 6
				if m.detailScrollOffset > maxOffset {
					m.detailScrollOffset = maxOffset
				}
				return m, nil
			}
			if len(m.tasks) > 0 {
				step := maxRows / 2
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
			if m.activePane == PaneDetail {
				if m.detailScrollOffset > 6 {
					m.detailScrollOffset -= 6
				} else {
					m.detailScrollOffset = 0
				}
				maxOffset := m.maxDetailScrollOffset()
				if m.detailScrollOffset > maxOffset {
					m.detailScrollOffset = maxOffset
				}
				return m, nil
			}
			if len(m.tasks) > 0 {
				step := maxRows / 2
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
				m.statusMsg = "Focused Tasks list"
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
				ids := m.getSelectedIDs()
				m.confirmDialog = &ConfirmDialog{
					Kind:      ConfirmDelete,
					Title:     "CONFIRM BATCH DELETION",
					Prompt:    fmt.Sprintf("Permanently delete %d selected tasks?", len(ids)),
					Details:   m.getTaskTitlesForIDs(ids),
					TargetIDs: ids,
					IsBatch:   true,
				}
				return m, nil
			}
			if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				m.confirmDialog = &ConfirmDialog{
					Kind:      ConfirmDelete,
					Title:     "CONFIRM DELETION",
					Prompt:    fmt.Sprintf("Permanently delete task %s?", t.ID),
					Details:   []string{fmt.Sprintf("%s: %s", t.ID, t.Title)},
					TargetIDs: []string{t.ID},
					IsBatch:   false,
				}
				return m, nil
			}

		case "a":
			actor := git.GetAuthor()
			if len(m.selected) > 0 {
				ids := m.getSelectedIDs()
				m.confirmDialog = &ConfirmDialog{
					Kind:           ConfirmClaim,
					Title:          "CONFIRM BATCH CLAIM",
					Prompt:         fmt.Sprintf("Claim %d selected tasks as %q?", len(ids), actor),
					Details:        m.getTaskTitlesForIDs(ids),
					TargetIDs:      ids,
					IsBatch:        true,
					TargetAssignee: actor,
				}
				return m, nil
			}
			if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				m.confirmDialog = &ConfirmDialog{
					Kind:           ConfirmClaim,
					Title:          "CONFIRM TASK CLAIM",
					Prompt:         fmt.Sprintf("Claim task %s as %q?", t.ID, actor),
					Details:        []string{fmt.Sprintf("%s: %s", t.ID, t.Title)},
					TargetIDs:      []string{t.ID},
					IsBatch:        false,
					TargetAssignee: actor,
				}
				return m, nil
			}

		case "d":
			// Batch Done if tasks selected, otherwise single-task reversible done
			if len(m.selected) > 0 {
				ids := m.getSelectedIDs()
				m.confirmDialog = &ConfirmDialog{
					Kind:         ConfirmDone,
					Title:        "CONFIRM BATCH DONE",
					Prompt:       fmt.Sprintf("Mark %d selected tasks as DONE?", len(ids)),
					Details:      m.getTaskTitlesForIDs(ids),
					TargetIDs:    ids,
					IsBatch:      true,
					TargetStatus: domain.StatusDone,
				}
				return m, nil
			}
			if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				if t.Status == domain.StatusDone || t.Status == domain.StatusClosed {
					m.confirmDialog = &ConfirmDialog{
						Kind:         ConfirmReopen,
						Title:        "CONFIRM REOPEN TASK",
						Prompt:       fmt.Sprintf("Reopen task %s to IN_PROGRESS?", t.ID),
						Details:      []string{fmt.Sprintf("%s: %s", t.ID, t.Title)},
						TargetIDs:    []string{t.ID},
						IsBatch:      false,
						TargetStatus: domain.StatusInProgress,
					}
					return m, nil
				}
				m.confirmDialog = &ConfirmDialog{
					Kind:         ConfirmDone,
					Title:        "CONFIRM TASK DONE",
					Prompt:       fmt.Sprintf("Mark task %s as DONE?", t.ID),
					Details:      []string{fmt.Sprintf("%s: %s", t.ID, t.Title)},
					TargetIDs:    []string{t.ID},
					IsBatch:      false,
					TargetStatus: domain.StatusDone,
				}
				return m, nil
			}

		case "s":
			// Batch status toggle if tasks selected, otherwise single-task toggle
			if len(m.selected) > 0 {
				ids := m.getSelectedIDs()
				m.confirmDialog = &ConfirmDialog{
					Kind:      ConfirmStatus,
					Title:     "CONFIRM BATCH STATUS CYCLE",
					Prompt:    fmt.Sprintf("Cycle status for %d selected tasks?", len(ids)),
					Details:   m.getTaskTitlesForIDs(ids),
					TargetIDs: ids,
					IsBatch:   true,
				}
				return m, nil
			}
			if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				newStatus := domain.StatusInProgress
				if t.Status == domain.StatusInProgress {
					newStatus = domain.StatusOpen
				} else if t.Status == domain.StatusDone || t.Status == domain.StatusClosed {
					newStatus = domain.StatusInProgress
				}
				m.confirmDialog = &ConfirmDialog{
					Kind:         ConfirmStatus,
					Title:        "CONFIRM STATUS CHANGE",
					Prompt:       fmt.Sprintf("Change status of %s to %s?", t.ID, strings.ToUpper(string(newStatus))),
					Details:      []string{fmt.Sprintf("%s: %s (current: %s)", t.ID, t.Title, t.Status)},
					TargetIDs:    []string{t.ID},
					IsBatch:      false,
					TargetStatus: newStatus,
				}
				return m, nil
			}

		case "u":
			if len(m.undoStack) == 0 {
				m.statusMsg = "Nothing to undo"
				return m, nil
			}
			lastUndo := m.undoStack[len(m.undoStack)-1]
			m.undoStack = m.undoStack[:len(m.undoStack)-1]
			m.statusMsg = fmt.Sprintf("Undid: %s", lastUndo.Description)
			return m, m.executeUndoCmd(lastUndo)

		case "n":
			// Create new task via editor
			tmpPath, err := editor.CreateNewTaskFile("")
			if err != nil {
				return m, func() tea.Msg { return errMsg(err) }
			}
			cmd := editor.BuildEditorCmd(m.editorCmd, tmpPath)
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				defer os.Remove(tmpPath)
				if err != nil {
					return errMsg(err)
				}
				data, err := os.ReadFile(tmpPath)
				if err != nil {
					return errMsg(err)
				}
				parsed, err := editor.ParseNewTaskContent(string(data))
				if err != nil {
					return errMsg(err)
				}
				author := git.GetAuthor()
				_, err = m.client.CreateTask(protocol.CreateTaskPayload{
					Title:       parsed.Title,
					Description: parsed.Description,
					Status:      domain.StatusOpen,
					Priority:    parsed.Priority,
					Tags:        parsed.Tags,
					Author:      author,
					Assignee:    parsed.Assignee,
				})
				if err != nil {
					return errMsg(err)
				}
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
					defer os.Remove(tmpPath)
					if err != nil {
						return errMsg(err)
					}
					data, err := os.ReadFile(tmpPath)
					if err != nil {
						return errMsg(err)
					}
					parsed, err := editor.ParseEditTaskContent(string(data), t)
					if err != nil {
						return errMsg(err)
					}
					author := git.GetAuthor()
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
						Author:      author,
					})
					if err != nil {
						return errMsg(err)
					}
					return m.fetchTasksCmd()()
				})
			}
		}
	}

	return m, nil
}

func (m Model) deleteTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.DeleteTask(taskID)
		if err != nil {
			return errMsg(err)
		}
		return m.fetchTasksCmd()()
	}
}

func (m Model) reopenTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		st := domain.StatusOpen
		note := "Reopened task"
		_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
			ID:      taskID,
			Status:  &st,
			AddNote: &note,
		})
		if err != nil {
			return errMsg(err)
		}
		return m.fetchTasksCmd()()
	}
}

func (m Model) updateStatusCmd(taskID string, newStatus domain.Status) tea.Cmd {
	return func() tea.Msg {
		_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
			ID:     taskID,
			Status: &newStatus,
		})
		if err != nil {
			return errMsg(err)
		}
		return m.fetchTasksCmd()()
	}
}

func (m Model) markDoneCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		// Attempt to grab latest git commit if local
		var sha, msg, stat, diff string
		if m.scope == "local" && m.repoPath != "" {
			if commit, err := git.GetLatestCommit(m.repoPath); err == nil {
				sha = commit.ShortSHA
				msg = commit.Subject
				stat = commit.DiffStat
				diff = commit.DiffContent
			}
		}

		_, err := m.client.AttachGit(taskID, sha, msg, stat, diff, true)
		if err != nil {
			return errMsg(err)
		}

		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) claimTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		actor := m.client.Actor()
		if actor == "" {
			actor = git.GetAuthor()
		}
		_, err := m.client.ClaimTask(taskID, actor, false)
		if err != nil {
			return errMsg(err)
		}
		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) getSelectedIDs() []string {
	var ids []string
	for _, t := range m.tasks {
		if m.selected[t.ID] {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

func (m Model) batchDoneCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		var sha, msg, stat, diff string
		if m.scope == "local" && m.repoPath != "" {
			if commit, err := git.GetLatestCommit(m.repoPath); err == nil {
				sha = commit.ShortSHA
				msg = commit.Subject
				stat = commit.DiffStat
				diff = commit.DiffContent
			}
		}
		for _, id := range ids {
			if sha != "" {
				_, _ = m.client.AttachGit(id, sha, msg, stat, diff, true)
			} else {
				st := domain.StatusDone
				note := "Completed task"
				_, _ = m.client.UpdateTask(protocol.UpdateTaskPayload{
					ID:      id,
					Status:  &st,
					AddNote: &note,
				})
			}
		}
		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) batchStatusCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		allInProg := true
		for _, id := range ids {
			for _, t := range m.tasks {
				if t.ID == id && t.Status != domain.StatusInProgress {
					allInProg = false
					break
				}
			}
			if !allInProg {
				break
			}
		}
		targetStatus := domain.StatusInProgress
		if allInProg {
			targetStatus = domain.StatusOpen
		}
		for _, id := range ids {
			st := targetStatus
			_, _ = m.client.UpdateTask(protocol.UpdateTaskPayload{
				ID:     id,
				Status: &st,
			})
		}
		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) batchClaimCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		actor := m.client.Actor()
		if actor == "" {
			actor = git.GetAuthor()
		}
		for _, id := range ids {
			_, _ = m.client.ClaimTask(id, actor, false)
		}
		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) batchDeleteCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		for _, id := range ids {
			_ = m.client.DeleteTask(id)
		}
		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func wrapText(text string, width int, indent string) []string {
	var result []string
	paragraphs := strings.Split(text, "\n")
	for _, p := range paragraphs {
		p = strings.TrimRight(p, " \t\r")
		if p == "" {
			result = append(result, "")
			continue
		}
		words := strings.Fields(p)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}
		var curLine strings.Builder
		curLine.WriteString(indent)
		curLen := len(indent)

		for _, w := range words {
			if curLen > len(indent) && curLen+1+len(w) > width {
				result = append(result, curLine.String())
				curLine.Reset()
				curLine.WriteString(indent)
				curLen = len(indent)
			}
			if curLen > len(indent) {
				curLine.WriteString(" ")
				curLen++
			}
			curLine.WriteString(w)
			curLen += len(w)
		}
		if curLine.Len() > len(indent) {
			result = append(result, curLine.String())
		}
	}
	return result
}

func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}

	theme := m.theme

	// 1. Header
	openCount, progCount, doneCount := 0, 0, 0
	for _, t := range m.tasks {
		switch t.Status {
		case domain.StatusOpen:
			openCount++
		case domain.StatusInProgress:
			progCount++
		case domain.StatusDone, domain.StatusClosed:
			doneCount++
		}
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(WaveAqua2))
	var scopeBadge string
	if m.scope == "global" {
		scopeBadge = theme.ScopeGlobal.Render("[global]")
	} else {
		scopeBadge = theme.ScopeLocal.Render("[local]")
	}

	countsLabel := fmt.Sprintf("%s │ %s │ %s",
		theme.CountOpen.Render(fmt.Sprintf("%d Open", openCount)),
		theme.CountProg.Render(fmt.Sprintf("%d Active", progCount)),
		theme.CountDone.Render(fmt.Sprintf("%d Done", doneCount)),
	)
	headerText := fmt.Sprintf("%s %s  ─  %s", titleStyle.Render("tk: task_cli"), scopeBadge, countsLabel)
	if m.isSearch {
		headerText += "  ─  " + theme.SearchPrompt.Render(fmt.Sprintf("[Search: %s_] (Enter apply, Esc cancel)", m.filterText))
	} else if m.filterText != "" {
		headerText += "  ─  " + theme.FilterIndicator.Render(fmt.Sprintf("[Filter: %q (Esc to clear)]", m.filterText))
	}

	var rawView string
	if m.isSplitView() {
		rawView = m.renderSplitView(width, height, headerText)
	} else {
		rawView = m.renderStackedView(width, height, headerText)
	}

	if dlg := m.activeConfirmDialog(); dlg != nil {
		return m.overlayModal(rawView, dlg)
	}
	return rawView
}

func (m Model) renderStackedView(width, height int, headerText string) string {
	theme := m.theme
	var sb strings.Builder
	sb.WriteString(theme.Header.Width(width).Render(headerText))
	sb.WriteString("\n")

	// Dynamic column widths calculation:
	idWidth := 6
	statusWidth := 11
	priWidth := 7
	ageWidth := 14
	var tagsWidth int
	var titleWidth int

	if width >= 120 {
		tagsWidth = 16
		titleWidth = width - 45 - tagsWidth
	} else if width >= 90 {
		tagsWidth = 12
		titleWidth = width - 45 - tagsWidth
	} else if width >= 72 {
		tagsWidth = 10
		titleWidth = width - 45 - tagsWidth
	} else if width >= 60 {
		tagsWidth = 0
		titleWidth = width - 44
	} else {
		// Ultra-narrow terminal (< 60 cols)
		tagsWidth = 0
		statusWidth = 11
		priWidth = 6
		ageWidth = 8
		titleWidth = width - 37
	}

	if titleWidth < 12 {
		titleWidth = 12
	}

	// Table columns header
	var colHeader string
	if tagsWidth > 0 {
		colHeader = fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s %-*s",
			idWidth, "ID",
			statusWidth, "STATUS",
			priWidth, "PRI",
			titleWidth, "TITLE",
			tagsWidth, "TAGS",
			ageWidth, "AGE",
		)
	} else {
		colHeader = fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
			idWidth, "ID",
			statusWidth, "STATUS",
			priWidth, "PRI",
			titleWidth, "TITLE",
			ageWidth, "AGE",
		)
	}
	sb.WriteString(theme.ColHeader.Render(colHeader))
	sb.WriteString("\n")
	sb.WriteString(theme.Border.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	// 2. Task rows (scrollable window)
	maxRows := m.getMaxRows()
	if len(m.tasks) == 0 {
		sb.WriteString("  No tasks found. Press 'n' to create a new task.\n")
	} else {
		endIdx := m.scrollOffset + maxRows
		if endIdx > len(m.tasks) {
			endIdx = len(m.tasks)
		}

		for i := m.scrollOffset; i < endIdx; i++ {
			t := m.tasks[i]
			isCursor := (i == m.cursor)
			prefix := "  "
			if len(m.selected) > 0 {
				isSel := m.selected[t.ID]
				if isCursor && isSel {
					prefix = theme.Cursor.Render("►") + theme.SelectedMark.Render("●")
				} else if isCursor && !isSel {
					prefix = theme.Cursor.Render("►") + theme.UnselectedMark.Render("○")
				} else if !isCursor && isSel {
					prefix = " " + theme.SelectedMark.Render("●")
				} else {
					prefix = " " + theme.UnselectedMark.Render("○")
				}
			} else {
				if isCursor {
					prefix = theme.Cursor.Render("► ")
				}
			}

			title := t.Title
			if len(title) > titleWidth {
				if titleWidth > 3 {
					title = title[:titleWidth-2] + ".."
				} else {
					title = title[:titleWidth]
				}
			}

			age := timeAgo(t.CreatedAt)
			if t.CommitSHA != "" {
				sha := t.CommitSHA
				if len(sha) > 6 {
					sha = sha[:6]
				}
				age = fmt.Sprintf("%s (%s)", age, sha)
			}
			if len(age) > ageWidth {
				age = age[:ageWidth]
			}

			status := string(t.Status)
			if len(status) > statusWidth {
				status = status[:statusWidth]
			}
			prio := string(t.Priority)
			if len(prio) > priWidth {
				prio = prio[:priWidth]
			}

			idStr := theme.TaskID.Render(fmt.Sprintf("%-*s", idWidth, t.ID))
			statusStr := StatusStyle(t.Status).Render(fmt.Sprintf("%-*s", statusWidth, status))
			priStr := PriorityStyle(t.Priority).Render(fmt.Sprintf("%-*s", priWidth, prio))
			titleStr := theme.TaskTitle.Render(fmt.Sprintf("%-*s", titleWidth, title))
			ageDisplay := theme.Age.Render(fmt.Sprintf("%-*s", ageWidth, age))

			var line string
			if tagsWidth > 0 {
				tagsStr := strings.Join(t.Tags, ",")
				if len(tagsStr) > tagsWidth {
					if tagsWidth > 3 {
						tagsStr = tagsStr[:tagsWidth-2] + ".."
					} else {
						tagsStr = tagsStr[:tagsWidth]
					}
				}
				tagsDisplay := theme.Tag.Render(fmt.Sprintf("%-*s", tagsWidth, tagsStr))
				line = fmt.Sprintf("%s%s %s %s %s %s %s",
					prefix, idStr, statusStr, priStr, titleStr, tagsDisplay, ageDisplay,
				)
			} else {
				line = fmt.Sprintf("%s%s %s %s %s %s",
					prefix, idStr, statusStr, priStr, titleStr, ageDisplay,
				)
			}

			if isCursor {
				sb.WriteString(theme.RowSelected.Width(width).Render(line))
			} else {
				sb.WriteString(line)
			}
			sb.WriteString("\n")
		}

		if len(m.tasks) > maxRows {
			pagination := fmt.Sprintf("  ── Showing %d-%d of %d tasks (▲/▼ to scroll) ──", m.scrollOffset+1, endIdx, len(m.tasks))
			sb.WriteString(theme.FooterDesc.Render(pagination))
			sb.WriteString("\n")
		}
	}

	sb.WriteString(theme.Border.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	// 3. Detail Pane
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		t := m.tasks[m.cursor]

		// Title with clean wrapping if title is long
		rawTitle := t.Title
		wrappedTitle := wrapText(rawTitle, width-len(t.ID)-6, "    ")
		if len(wrappedTitle) > 0 {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				theme.DetailID.Render(t.ID+":"),
				theme.DetailTitle.Render(strings.TrimPrefix(wrappedTitle[0], "    ")),
			))
			for _, tl := range wrappedTitle[1:] {
				sb.WriteString(theme.DetailTitle.Render(tl) + "\n")
			}
		} else {
			sb.WriteString(fmt.Sprintf("  %s %s\n",
				theme.DetailID.Render(t.ID+":"),
				theme.DetailTitle.Render(rawTitle),
			))
		}

		// Metadata line(s) - responsive to avoid ugly mid-tag wrapping
		metaLine1 := fmt.Sprintf("  %s %s   %s %s   %s %s",
			theme.DetailLabel.Render("Status:"),
			StatusStyle(t.Status).Render(string(t.Status)),
			theme.DetailLabel.Render("Priority:"),
			PriorityStyle(t.Priority).Render(string(t.Priority)),
			theme.DetailLabel.Render("Scope:"),
			theme.TaskID.Render(t.Scope),
		)

		var metaParts []string
		if t.Assignee != "" {
			metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Assignee:"), theme.NoteHeader.Render(t.Assignee)))
		}
		if len(t.Tags) > 0 {
			metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Tags:"), theme.Tag.Render(strings.Join(t.Tags, ", "))))
		}

		fullMeta := metaLine1
		if len(metaParts) > 0 {
			fullMeta += "   " + strings.Join(metaParts, "   ")
		}

		if lipgloss.Width(fullMeta) <= width {
			sb.WriteString(fullMeta + "\n\n")
		} else {
			sb.WriteString(metaLine1 + "\n")
			if len(metaParts) > 0 {
				line2 := "  " + strings.Join(metaParts, "   ")
				if lipgloss.Width(line2) <= width {
					sb.WriteString(line2 + "\n\n")
				} else {
					for _, part := range metaParts {
						for _, pw := range wrapText(part, width-4, "  ") {
							sb.WriteString(pw + "\n")
						}
					}
					sb.WriteString("\n")
				}
			} else {
				sb.WriteString("\n")
			}
		}

		// Description with clean indented word wrapping
		if t.Description != "" {
			descLines := wrapText(t.Description, width-6, "    ")
			maxDesc := 3
			for i, line := range descLines {
				if i >= maxDesc {
					sb.WriteString(theme.Age.Render("    ... (press 'e' to view full)") + "\n")
					break
				}
				sb.WriteString(theme.DetailDesc.Render(line) + "\n")
			}
		}

		if t.CommitSHA != "" {
			resText := fmt.Sprintf("✓ Resolved by commit %s: %s", t.CommitSHA, t.CommitMsg)
			resLines := wrapText(resText, width-4, "  ")
			for _, rl := range resLines {
				sb.WriteString(theme.DetailResolved.Render(rl) + "\n")
			}
			if t.DiffStat != "" {
				statLines := strings.Split(strings.TrimSpace(t.DiffStat), "\n")
				for i, sl := range statLines {
					if i >= 3 {
						break
					}
					sb.WriteString("    " + theme.DiffStat.Render(strings.TrimSpace(sl)) + "\n")
				}
			}
			if m.showDiff && t.DiffContent != "" {
				sb.WriteString("\n  " + theme.Border.Render("── Diff Content ──") + "\n")
				diffLines := strings.Split(t.DiffContent, "\n")
				maxDiff := 8
				if height <= 26 {
					maxDiff = 5
				}
				for i, dl := range diffLines {
					if i >= maxDiff {
						sb.WriteString(theme.Age.Render("    [diff truncated, press enter to collapse]") + "\n")
						break
					}
					if strings.HasPrefix(dl, "+") {
						sb.WriteString("    " + theme.DiffAdd.Render(dl) + "\n")
					} else if strings.HasPrefix(dl, "-") {
						sb.WriteString("    " + theme.DiffSub.Render(dl) + "\n")
					} else if strings.HasPrefix(dl, "@@") {
						sb.WriteString("    " + theme.DiffHunk.Render(dl) + "\n")
					} else {
						sb.WriteString("    " + theme.DetailDesc.Render(dl) + "\n")
					}
				}
			}
		}

		if len(t.Notes) > 0 {
			sb.WriteString(theme.NoteHeader.Render(
				fmt.Sprintf("  • %d note(s):", len(t.Notes)),
			) + "\n")
			maxNotes := 3
			if height <= 26 {
				maxNotes = 2
			}
			for i, n := range t.Notes {
				if i >= maxNotes {
					sb.WriteString(theme.Age.Render(fmt.Sprintf("    ... and %d more note(s)", len(t.Notes)-maxNotes)) + "\n")
					break
				}
				timeStr := theme.NoteTime.Render(timeAgo(n.CreatedAt))
				authorStr := ""
				if n.Author != "" {
					availAuthor := width - 18
					author := n.Author
					if availAuthor > 6 && len(author) > availAuthor {
						author = author[:availAuthor-2] + ".."
					}
					authorStr = " by " + theme.NoteAuthor.Render(author)
				}
				sb.WriteString(fmt.Sprintf("    • %s%s:\n", timeStr, authorStr))
				wrappedNotes := wrapText(n.Content, width-8, "      ")
				for _, line := range wrappedNotes {
					sb.WriteString(theme.NoteContent.Render(line) + "\n")
				}
			}
		}
	}

	sb.WriteString(theme.Border.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	sb.WriteString(m.renderFooter(width))

	return sb.String()
}

func (m Model) renderSplitView(width, height int, headerText string) string {
	theme := m.theme
	var sb strings.Builder

	// 1. Header
	sb.WriteString(theme.Header.Width(width).Render(headerText))
	sb.WriteString("\n")

	// Split proportions
	detailWidth, visibleBody := m.getDetailDimensions()
	bodyHeight := visibleBody + 1
	tableWidth := width - detailWidth - 1

	// ─── LEFT PANE: Tasks Table ───
	var leftLines []string
	idWidth := 6
	statusWidth := 11
	priWidth := 6
	ageWidth := 10
	var tagsWidth int
	if tableWidth >= 65 {
		tagsWidth = 10
	}
	fixed := 39
	if tagsWidth > 0 {
		fixed += tagsWidth + 1
	}
	titleWidth := tableWidth - fixed
	if titleWidth < 10 {
		titleWidth = 10
	}

	var colHeader string
	if tagsWidth > 0 {
		colHeader = fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s %-*s",
			idWidth, "ID", statusWidth, "STATUS", priWidth, "PRI", titleWidth, "TITLE", tagsWidth, "TAGS", ageWidth, "AGE")
	} else {
		colHeader = fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
			idWidth, "ID", statusWidth, "STATUS", priWidth, "PRI", titleWidth, "TITLE", ageWidth, "AGE")
	}
	var tableHeaderStyled string
	if m.activePane == PaneList {
		tableHeaderStyled = theme.ActiveHeader.Width(tableWidth).Render(colHeader)
	} else {
		tableHeaderStyled = theme.ColHeader.Width(tableWidth).Render(colHeader)
	}
	leftLines = append(leftLines, tableHeaderStyled)
	leftLines = append(leftLines, theme.Border.Render(strings.Repeat("─", tableWidth)))

	maxRows := bodyHeight - 2
	if len(m.tasks) > maxRows {
		maxRows-- // reserve 1 line for pagination
	}
	if maxRows < 4 {
		maxRows = 4
	}

	endIdx := m.scrollOffset + maxRows
	if endIdx > len(m.tasks) {
		endIdx = len(m.tasks)
	}

	if len(m.tasks) == 0 {
		leftLines = append(leftLines, theme.RowNormal.Width(tableWidth).Render("  No tasks found. Press 'n' to create."))
	} else {
		for i := m.scrollOffset; i < endIdx; i++ {
			t := m.tasks[i]
			isCursor := (i == m.cursor)
			prefix := "  "
			if len(m.selected) > 0 {
				isSel := m.selected[t.ID]
				if isCursor && isSel {
					prefix = theme.Cursor.Render("►") + theme.SelectedMark.Render("●")
				} else if isCursor && !isSel {
					prefix = theme.Cursor.Render("►") + theme.UnselectedMark.Render("○")
				} else if !isCursor && isSel {
					prefix = " " + theme.SelectedMark.Render("●")
				} else {
					prefix = " " + theme.UnselectedMark.Render("○")
				}
			} else {
				if isCursor {
					prefix = theme.Cursor.Render("► ")
				}
			}

			title := t.Title
			if len(title) > titleWidth {
				if titleWidth > 3 {
					title = title[:titleWidth-2] + ".."
				} else {
					title = title[:titleWidth]
				}
			}

			age := timeAgo(t.CreatedAt)
			if t.CommitSHA != "" {
				sha := t.CommitSHA
				if len(sha) > 6 {
					sha = sha[:6]
				}
				age = fmt.Sprintf("%s (%s)", age, sha)
			}
			if len(age) > ageWidth {
				age = age[:ageWidth]
			}

			status := string(t.Status)
			if len(status) > statusWidth {
				status = status[:statusWidth]
			}
			prio := string(t.Priority)
			if len(prio) > priWidth {
				prio = prio[:priWidth]
			}

			idStr := theme.TaskID.Render(fmt.Sprintf("%-*s", idWidth, t.ID))
			statusStr := StatusStyle(t.Status).Render(fmt.Sprintf("%-*s", statusWidth, status))
			priStr := PriorityStyle(t.Priority).Render(fmt.Sprintf("%-*s", priWidth, prio))
			titleStr := theme.TaskTitle.Render(fmt.Sprintf("%-*s", titleWidth, title))
			ageDisplay := theme.Age.Render(fmt.Sprintf("%-*s", ageWidth, age))

			var line string
			if tagsWidth > 0 {
				tagsStr := strings.Join(t.Tags, ",")
				if len(tagsStr) > tagsWidth {
					if tagsWidth > 3 {
						tagsStr = tagsStr[:tagsWidth-2] + ".."
					} else {
						tagsStr = tagsStr[:tagsWidth]
					}
				}
				tagsDisplay := theme.Tag.Render(fmt.Sprintf("%-*s", tagsWidth, tagsStr))
				line = fmt.Sprintf("%s%s %s %s %s %s %s", prefix, idStr, statusStr, priStr, titleStr, tagsDisplay, ageDisplay)
			} else {
				line = fmt.Sprintf("%s%s %s %s %s %s", prefix, idStr, statusStr, priStr, titleStr, ageDisplay)
			}

			if isCursor {
				leftLines = append(leftLines, theme.RowSelected.Width(tableWidth).Render(line))
			} else {
				leftLines = append(leftLines, theme.RowNormal.Width(tableWidth).Render(line))
			}
		}

		if len(m.tasks) > maxRows {
			pagination := fmt.Sprintf("  ── Showing %d-%d of %d (▲/▼) ──", m.scrollOffset+1, endIdx, len(m.tasks))
			leftLines = append(leftLines, theme.FooterDesc.Width(tableWidth).Render(pagination))
		}
	}

	for len(leftLines) < bodyHeight {
		leftLines = append(leftLines, strings.Repeat(" ", tableWidth))
	}
	if len(leftLines) > bodyHeight {
		leftLines = leftLines[:bodyHeight]
	}

	// ─── DIVIDER COLUMN ───
	var divLines []string
	divStyle := theme.Border
	if m.activePane == PaneDetail {
		divStyle = theme.ActiveBorder
	}
	for i := 0; i < bodyHeight; i++ {
		divLines = append(divLines, divStyle.Render("│"))
	}

	// ─── RIGHT PANE: Live Detail & Latest Activity ───
	var rightLines []string
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		contentLines := m.getDetailContentLines(detailWidth)

		// Pinned Header and viewport calculation
		totalContent := len(contentLines)
		maxOffset := totalContent - visibleBody
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.detailScrollOffset > maxOffset {
			m.detailScrollOffset = maxOffset
		}
		if m.detailScrollOffset < 0 {
			m.detailScrollOffset = 0
		}

		start := m.detailScrollOffset
		end := start + visibleBody
		if end > totalContent {
			end = totalContent
		}

		var panelTitle string
		if m.activePane == PaneDetail {
			if totalContent > visibleBody {
				panelTitle = fmt.Sprintf(" ── Details & Messages (L%d-%d/%d) [Active • j/k scroll • Tab list] ──", start+1, end, totalContent)
			} else {
				panelTitle = " ── Details & Messages [Active • Tab to return to list] ──"
			}
			rightLines = append(rightLines, theme.ActiveHeader.Width(detailWidth).Render(panelTitle))
		} else {
			if totalContent > visibleBody {
				panelTitle = fmt.Sprintf(" ── Details & Messages (L%d-%d/%d) [Tab/l to focus & scroll] ──", start+1, end, totalContent)
			} else {
				panelTitle = " ── Details & Messages [Tab/l to focus] ──"
			}
			rightLines = append(rightLines, theme.InactiveHeader.Width(detailWidth).Render(panelTitle))
		}

		if totalContent > 0 {
			rightLines = append(rightLines, contentLines[start:end]...)
		}
	} else {
		panelTitle := "  ── Task Details & Messages ──"
		if m.activePane == PaneDetail {
			rightLines = append(rightLines, theme.ActiveHeader.Width(detailWidth).Render(panelTitle))
		} else {
			rightLines = append(rightLines, theme.InactiveHeader.Width(detailWidth).Render(panelTitle))
		}
		rightLines = append(rightLines, theme.RowNormal.Width(detailWidth).Render("  No task selected."))
	}

	for len(rightLines) < bodyHeight {
		rightLines = append(rightLines, strings.Repeat(" ", detailWidth))
	}
	if len(rightLines) > bodyHeight {
		rightLines = rightLines[:bodyHeight]
	}

	leftJoined := strings.Join(leftLines, "\n")
	divJoined := strings.Join(divLines, "\n")
	rightJoined := strings.Join(rightLines, "\n")
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftJoined, divJoined, rightJoined)
	sb.WriteString(body)
	sb.WriteString("\n")

	sb.WriteString(theme.Border.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")
	sb.WriteString(m.renderFooter(width))

	return sb.String()
}

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
	lines = append(lines, titleStr)
	lines = append(lines, "")

	// Prompt
	promptWrapped := wrapText(dlg.Prompt, contentWidth, "")
	for _, pw := range promptWrapped {
		lines = append(lines, theme.ModalPrompt.Render(pw))
	}

	// Details
	if len(dlg.Details) > 0 {
		lines = append(lines, "")
		for _, det := range dlg.Details {
			wrapped := wrapText("• "+det, contentWidth, "  ")
			for _, w := range wrapped {
				lines = append(lines, theme.ModalDesc.Render(w))
			}
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
			case ConfirmDone:
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

	undoKey := ""
	if len(m.undoStack) > 0 {
		undoKey = theme.ModalKey.Render("[u] Undo")
	}

	if len(m.selected) > 0 {
		selCount := len(m.selected)
		selBadge := theme.BatchBadge.Render(fmt.Sprintf("%d selected", selCount))
		var keys []string
		if width >= 115 {
			keys = []string{
				selBadge,
				theme.CountDone.Render(fmt.Sprintf("[d] Done (%d)", selCount)),
				theme.CountProg.Render(fmt.Sprintf("[s] Status (%d)", selCount)),
				theme.CountOpen.Render(fmt.Sprintf("[a] Claim (%d)", selCount)),
				theme.DiffSub.Render(fmt.Sprintf("[x] Del (%d)", selCount)),
			}
			if undoKey != "" {
				keys = append(keys, undoKey)
			}
			keys = append(keys,
				key("Space", "Toggle"),
				key("V", "All"),
				theme.FilterIndicator.Render("[Esc] Clear"),
				theme.Age.Render("[q] Quit"),
			)
		} else if width >= 80 {
			keys = []string{
				selBadge,
				theme.CountDone.Render(fmt.Sprintf("[d] Done(%d)", selCount)),
				theme.CountProg.Render(fmt.Sprintf("[s] Status(%d)", selCount)),
				theme.DiffSub.Render(fmt.Sprintf("[x] Del(%d)", selCount)),
			}
			if undoKey != "" {
				keys = append(keys, undoKey)
			}
			keys = append(keys,
				key("Space", "Tgl"),
				theme.FilterIndicator.Render("[Esc]"),
				theme.Age.Render("[q]"),
			)
		} else {
			keys = []string{
				selBadge,
				theme.CountDone.Render(fmt.Sprintf("[d](%d)", selCount)),
				theme.DiffSub.Render(fmt.Sprintf("[x](%d)", selCount)),
			}
			if undoKey != "" {
				keys = append(keys, undoKey)
			}
			keys = append(keys,
				theme.FilterIndicator.Render("[Esc]"),
				theme.Age.Render("[q]"),
			)
		}
		footerText := strings.Join(keys, "  ")
		if m.statusMsg != "" {
			footerText = theme.FooterStatus.Render("["+m.statusMsg+"]") + "  " + footerText
		}
		return theme.FooterBg.Width(width).Render(footerText)
	}

	doneLabel := theme.CountProg.Render("[d] Done")
	statusLabel := theme.CountOpen.Render("[s] In Prog")
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		switch m.tasks[m.cursor].Status {
		case domain.StatusDone, domain.StatusClosed:
			doneLabel = theme.CountDone.Render("[d] Reopen")
			statusLabel = theme.CountProg.Render("[s] In Prog")
		case domain.StatusInProgress:
			doneLabel = theme.CountProg.Render("[d] Done")
			statusLabel = theme.CountOpen.Render("[s] Open")
		default: // open
			doneLabel = theme.CountDone.Render("[d] Done")
			statusLabel = theme.CountProg.Render("[s] In Prog")
		}
	}

	if m.activePane == PaneDetail {
		var keys []string
		if width >= 115 {
			keys = []string{
				theme.BatchBadge.Render("DETAILS PANE"),
				key("j/k", "Scroll"),
				key("Ctrl+D/U", "Page"),
				key("g/G", "Top/Bottom"),
				key("Tab/h", "Tasks List"),
				key("Enter", "Diff"),
				doneLabel,
				statusLabel,
				theme.DiffSub.Render("[x] Del"),
			}
			if undoKey != "" {
				keys = append(keys, undoKey)
			}
			keys = append(keys, theme.Age.Render("[q] Quit"))
		} else if width >= 80 {
			keys = []string{
				theme.BatchBadge.Render("DETAILS"),
				key("j/k", "Scroll"),
				key("Tab", "Tasks"),
				key("Enter", "Diff"),
				doneLabel,
				statusLabel,
			}
			if undoKey != "" {
				keys = append(keys, undoKey)
			}
			keys = append(keys, theme.Age.Render("[q]"))
		} else {
			keys = []string{
				theme.BatchBadge.Render("DET"),
				key("j/k", "Scroll"),
				key("Tab", "Back"),
			}
			if undoKey != "" {
				keys = append(keys, undoKey)
			}
			keys = append(keys, theme.Age.Render("[q]"))
		}
		footerText := strings.Join(keys, "  ")
		if m.statusMsg != "" {
			footerText = theme.FooterStatus.Render("["+m.statusMsg+"]") + "  " + footerText
		}
		return theme.FooterBg.Width(width).Render(footerText)
	}

	var keys []string
	if width >= 120 {
		keys = []string{
			key("j/k", "Move"),
			key("Tab/l", "Details"),
			key("Space", "Sel"),
			key("n", "New"),
			key("e", "Edit"),
			key("a", "Claim"),
			doneLabel,
			statusLabel,
			theme.DiffSub.Render("[x]") + " " + theme.FooterDesc.Render("Del"),
		}
		if undoKey != "" {
			keys = append(keys, undoKey)
		}
		keys = append(keys,
			key("Enter", "Diff"),
			key("/", "Search"),
			theme.FilterIndicator.Render("[Esc]") + " " + theme.FooterDesc.Render("Clear"),
			key("S", "Scope"),
			key("r", "Refresh"),
			theme.Age.Render("[q] Quit"),
		)
	} else if width >= 90 {
		keys = []string{
			key("j/k", "Move"),
			key("Tab/l", "Details"),
			key("Space", "Sel"),
			key("n", "New"),
			key("e", "Edit"),
			key("a", "Claim"),
			doneLabel,
			statusLabel,
			theme.DiffSub.Render("[x] Del"),
		}
		if undoKey != "" {
			keys = append(keys, undoKey)
		}
		keys = append(keys,
			key("Enter", "Diff"),
			key("/", "Search"),
			key("S", "Scope"),
			theme.Age.Render("[q] Quit"),
		)
	} else if width >= 70 {
		keys = []string{
			key("j/k", "Nav"),
			key("n", "New"),
			key("e", "Edit"),
			key("a", "Claim"),
			doneLabel,
			key("x", "Del"),
		}
		if undoKey != "" {
			keys = append(keys, undoKey)
		}
		keys = append(keys,
			key("/", "Find"),
			theme.Age.Render("[q] Quit"),
		)
	} else {
		// Ultra-compact footer for narrow/split terminal panes (< 70 cols)
		keys = []string{
			key("j/k", "Nav"),
			key("a", "Claim"),
			doneLabel,
			key("x", "Del"),
		}
		if undoKey != "" {
			keys = append(keys, undoKey)
		}
		keys = append(keys,
			key("/", "Find"),
			theme.Age.Render("[q]"),
		)
	}

	footerText := strings.Join(keys, "  ")
	if m.statusMsg != "" {
		footerText = theme.FooterStatus.Render("["+m.statusMsg+"]") + "  " + footerText
	}
	return theme.FooterBg.Width(width).Render(footerText)
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
