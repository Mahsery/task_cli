package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"task_cli/internal/client"
	"task_cli/internal/domain"
	"task_cli/internal/editor"
	"task_cli/internal/git"
	"task_cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
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
	UndoTag
	UndoArchive
)

type TaskUndoSnapshot struct {
	Task               *domain.Task
	PreviousStatus     domain.Status
	PreviousAssignee   string
	PreviousClosedAt   *time.Time
	PreviousArchivedAt *time.Time
	PreviousTags       []string
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
	treeViewMode       bool
	splitOffset        int
	isDraggingSplit    bool
	isLinking          bool
	linkInput          string
	linkTaskID         string
	chordBuffer        string
	isTagging          bool
	tagInput           string
	tagTaskID          string
	showArchived       bool
}

type TreeRow struct {
	Task   *domain.Task
	Prefix string
	Depth  int
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
		if m.client == nil {
			return nil
		}
		filter := domain.TaskFilter{
			Scope:           m.scope,
			RepoPath:        m.repoPath,
			Search:          m.filterText,
			IncludeArchived: m.showArchived,
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
	basePercent := 48
	if m.treeViewMode {
		basePercent = 56
	}

	tableWidth := (width * basePercent / 100) + m.splitOffset

	minTableWidth := 38
	minDetailWidth := 25

	if width < minTableWidth+minDetailWidth+dividerWidth {
		tableWidth = width / 2
		detailWidth = width - tableWidth - dividerWidth
	} else {
		maxTableWidth := width - minDetailWidth - dividerWidth
		if tableWidth < minTableWidth {
			tableWidth = minTableWidth
		}
		if tableWidth > maxTableWidth {
			tableWidth = maxTableWidth
		}
		detailWidth = width - tableWidth - dividerWidth
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

func (m Model) currentTask() *domain.Task {
	if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
		return nil
	}
	if m.treeViewMode {
		rows := m.getTreeRows()
		if m.cursor < len(rows) {
			return rows[m.cursor].Task
		}
	}
	return m.tasks[m.cursor]
}

func (m Model) getTreeRows() []TreeRow {
	if len(m.tasks) == 0 {
		return nil
	}
	taskMap := make(map[string]*domain.Task, len(m.tasks))
	for _, t := range m.tasks {
		taskMap[t.ID] = t
	}

	var roots []*domain.Task
	visited := make(map[string]bool)

	for _, t := range m.tasks {
		hasPrereqInSet := false
		for _, depID := range t.DependsOn {
			if _, ok := taskMap[depID]; ok {
				hasPrereqInSet = true
				break
			}
		}
		if !hasPrereqInSet {
			roots = append(roots, t)
		}
	}

	var rows []TreeRow
	var traverse func(t *domain.Task, prefix string, isLast bool, isRoot bool, branchVisited map[string]bool)
	traverse = func(t *domain.Task, prefix string, isLast bool, isRoot bool, branchVisited map[string]bool) {
		branchGlyph := "├── "
		lastGlyph := "└── "
		contGlyph := "│   "
		emptyGlyph := "    "
		if isASCIIOnly() {
			branchGlyph = "|-- "
			lastGlyph = "\\-- "
			contGlyph = "|   "
			emptyGlyph = "    "
		}

		rowPrefix := prefix
		if !isRoot {
			if isLast {
				rowPrefix += lastGlyph
			} else {
				rowPrefix += branchGlyph
			}
		}

		rows = append(rows, TreeRow{
			Task:   t,
			Prefix: rowPrefix,
			Depth:  len(branchVisited),
		})
		visited[t.ID] = true
		branchVisited[t.ID] = true

		newPrefix := prefix
		if !isRoot {
			if isLast {
				newPrefix += emptyGlyph
			} else {
				newPrefix += contGlyph
			}
		}

		var children []*domain.Task
		for _, blkID := range t.Blocks {
			if child, ok := taskMap[blkID]; ok && !visited[blkID] && !branchVisited[blkID] {
				children = append(children, child)
			}
		}

		for i, child := range children {
			childLast := (i == len(children)-1)
			childBranchVisited := make(map[string]bool, len(branchVisited)+1)
			for k, v := range branchVisited {
				childBranchVisited[k] = v
			}
			traverse(child, newPrefix, childLast, false, childBranchVisited)
		}
	}

	for _, root := range roots {
		if !visited[root.ID] {
			traverse(root, "", true, true, make(map[string]bool))
		}
	}

	for _, t := range m.tasks {
		if !visited[t.ID] {
			traverse(t, "", true, true, make(map[string]bool))
		}
	}

	return rows
}

func (m Model) getDetailContentLines(detailWidth int) []string {
	if len(m.tasks) == 0 || m.cursor >= len(m.tasks) {
		return nil
	}
	t := m.currentTask()
	if t == nil {
		return nil
	}
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
	latestBadge = truncateToWidth(latestBadge, detailWidth)
	contentLines = append(contentLines, theme.NoteTime.Width(detailWidth).Render(latestBadge))
	contentLines = append(contentLines, strings.Repeat(" ", detailWidth))

	// Task Title
	for _, tl := range wrapText(t.ID+": "+t.Title, detailWidth-4, "  ") {
		contentLines = append(contentLines, theme.DetailTitle.Width(detailWidth).Render(tl))
	}

	// Metadata badges
	statusDisplay := string(t.Status)
	statusStyle := StatusStyle(t.Status)
	if t.IsOrphaned() {
		statusDisplay = fmt.Sprintf("%s 👻 (ORPHANED)", string(t.Status))
		statusStyle = theme.CountOrphaned
	}
	meta1 := fmt.Sprintf("  %s %s   %s %s   %s %s",
		theme.DetailLabel.Render("Status:"), statusStyle.Render(statusDisplay),
		theme.DetailLabel.Render("Pri:"), PriorityStyle(t.Priority).Render(string(t.Priority)),
		theme.DetailLabel.Render("Scope:"), theme.TaskID.Render(t.Scope),
	)
	var metaParts []string
	if t.Assignee != "" {
		assigneeDisplay := t.Assignee
		if t.IsOrphaned() {
			assigneeDisplay += " [STALE / TIMED OUT]"
		}
		metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Assignee:"), theme.NoteHeader.Render(assigneeDisplay)))
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

	// Dependencies & Blocked Status
	if t.IsBlocked {
		contentLines = append(contentLines, theme.DiffSub.Render("  🔒 BLOCKED: Must complete unfinished prerequisites first:"))
		for _, b := range t.BlockedBy {
			contentLines = append(contentLines, theme.DiffSub.Render(fmt.Sprintf("    • %s", b)))
		}
		contentLines = append(contentLines, strings.Repeat(" ", detailWidth))
	}
	if len(t.DependsOn) > 0 {
		depText := "Depends on: " + strings.Join(t.DependsOn, ", ")
		if t.IsBlocked {
			depText += " 🔒 [BLOCKED]"
		} else {
			depText += " ⚡ [READY]"
		}
		contentLines = append(contentLines, theme.DetailLabel.Render("  "+depText))
	}
	if len(t.Blocks) > 0 {
		blkText := "Blocks: " + strings.Join(t.Blocks, ", ")
		contentLines = append(contentLines, theme.DetailLabel.Render("  "+blkText))
	}
	if len(t.DependsOn) > 0 || len(t.Blocks) > 0 {
		contentLines = append(contentLines, strings.Repeat(" ", detailWidth))
	}

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
				author := truncateToWidth(n.Author, availAuthor)
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
			var fullTask *domain.Task
			if dlg.Kind == ConfirmDelete && m.client != nil {
				if fetched, err := m.client.GetTask(t.ID); err == nil && fetched != nil {
					fullTask = fetched
				}
			}
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
			snapshots = append(snapshots, TaskUndoSnapshot{
				Task:               fullTask,
				PreviousStatus:     t.Status,
				PreviousAssignee:   t.Assignee,
				PreviousClosedAt:   prevClosed,
				PreviousArchivedAt: prevArchived,
				PreviousTags:       append([]string(nil), t.Tags...),
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
		case UndoTag:
			for _, snap := range undo.Snapshots {
				note := "Undo: reverted tags"
				prevTags := snap.PreviousTags
				_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
					ID:      snap.Task.ID,
					Tags:    &prevTags,
					AddNote: &note,
				})
				if err != nil {
					return errMsg(fmt.Errorf("failed to restore tags on %s: %w", snap.Task.ID, err))
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
		tasks, err := m.client.ListTasks(domain.TaskFilter{
			Scope:           m.scope,
			RepoPath:        m.repoPath,
			Search:          m.filterText,
			IncludeArchived: m.showArchived,
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

	case tea.MouseMsg:
		if !m.isSplitView() {
			if msg.Button == tea.MouseButtonWheelUp {
				if m.cursor > 0 {
					m.cursor--
					if m.cursor < m.scrollOffset {
						m.scrollOffset = m.cursor
					}
				}
			} else if msg.Button == tea.MouseButtonWheelDown {
				maxRows := m.getMaxRows()
				displayCount := len(m.tasks)
				if m.treeViewMode {
					displayCount = len(m.getTreeRows())
				}
				if m.cursor < displayCount-1 {
					m.cursor++
					if m.cursor >= m.scrollOffset+maxRows {
						m.scrollOffset = m.cursor - maxRows + 1
					}
				}
			}
			return m, nil
		}

		detailWidth, _ := m.getDetailDimensions()
		tableWidth := m.width - detailWidth - 1

		switch msg.Action {
		case tea.MouseActionPress:
			if msg.Button == tea.MouseButtonLeft {
				// Click on or near the central divider bar to start dragging
				if msg.X >= tableWidth-1 && msg.X <= tableWidth+1 {
					m.isDraggingSplit = true
					return m, nil
				}
				// Click left of divider: focus list pane and select clicked task if on task row
				if msg.X < tableWidth {
					m.activePane = PaneList
					row := msg.Y - 3
					if row >= 0 {
						targetIdx := m.scrollOffset + row
						displayCount := len(m.tasks)
						if m.treeViewMode {
							displayCount = len(m.getTreeRows())
						}
						if targetIdx < displayCount {
							m.cursor = targetIdx
						}
					}
					return m, nil
				}
				// Click right of divider: focus details pane
				if msg.X > tableWidth {
					m.activePane = PaneDetail
					return m, nil
				}
			}

		case tea.MouseActionMotion:
			if m.isDraggingSplit {
				basePercent := 48
				if m.treeViewMode {
					basePercent = 56
				}
				baseTableWidth := m.width * basePercent / 100
				targetTableWidth := msg.X
				minTableWidth := 38
				maxTableWidth := m.width - 25 - 1
				if targetTableWidth < minTableWidth {
					targetTableWidth = minTableWidth
				}
				if targetTableWidth > maxTableWidth {
					targetTableWidth = maxTableWidth
				}
				m.splitOffset = targetTableWidth - baseTableWidth
				return m, nil
			}

		case tea.MouseActionRelease:
			m.isDraggingSplit = false
			return m, nil
		}

		// Mouse wheel scrolling in split view
		if msg.Button == tea.MouseButtonWheelUp {
			if msg.X <= tableWidth {
				if m.cursor > 0 {
					m.cursor--
					if m.cursor < m.scrollOffset {
						m.scrollOffset = m.cursor
					}
				}
			} else {
				if m.detailScrollOffset > 0 {
					m.detailScrollOffset--
				}
			}
			return m, nil
		} else if msg.Button == tea.MouseButtonWheelDown {
			if msg.X <= tableWidth {
				maxRows := m.getMaxRows()
				displayCount := len(m.tasks)
				if m.treeViewMode {
					displayCount = len(m.getTreeRows())
				}
				if m.cursor < displayCount-1 {
					m.cursor++
					if m.cursor >= m.scrollOffset+maxRows {
						m.scrollOffset = m.cursor - maxRows + 1
					}
				}
			} else {
				maxOffset := m.maxDetailScrollOffset()
				if m.detailScrollOffset < maxOffset {
					m.detailScrollOffset++
				}
			}
			return m, nil
		}
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
								newTags = append(cur.Tags, tag)
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
								if t.ID == id {
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
			fmt.Sscanf(m.chordBuffer, "%d", &chordCount)
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
						if t.ID == id {
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
				} else {
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
			cur := m.currentTask()
			if cur != nil {
				if cur.Status == domain.StatusDone || cur.Status == domain.StatusClosed {
					undo := UndoAction{
						Kind:        UndoStatusChange,
						Description: fmt.Sprintf("Reopened %s", cur.ID),
						Snapshots: []TaskUndoSnapshot{
							{
								Task:             cur,
								PreviousStatus:   cur.Status,
								PreviousAssignee: cur.Assignee,
								PreviousClosedAt: cur.ClosedAt,
								PreviousTags:     append([]string(nil), cur.Tags...),
							},
						},
					}
					m.undoStack = append(m.undoStack, undo)
					if len(m.undoStack) > 50 {
						m.undoStack = m.undoStack[len(m.undoStack)-50:]
					}
					cur.Status = domain.StatusOpen
					m.statusMsg = fmt.Sprintf("Reopened %s (press 'u' to undo)", cur.ID)
					return m, m.reopenTaskCmd(cur.ID)
				}
				if cur.IsBlocked {
					m.statusMsg = fmt.Sprintf("⚠️ Cannot mark %s DONE: blocked by unfinished prerequisite(s): %s", cur.ID, strings.Join(cur.BlockedBy, ", "))
					return m, nil
				}
				undo := UndoAction{
					Kind:        UndoStatusChange,
					Description: fmt.Sprintf("Marked %s as DONE", cur.ID),
					Snapshots: []TaskUndoSnapshot{
						{
							Task:             cur,
							PreviousStatus:   cur.Status,
							PreviousAssignee: cur.Assignee,
							PreviousClosedAt: cur.ClosedAt,
							PreviousTags:     append([]string(nil), cur.Tags...),
						},
					},
				}
				m.undoStack = append(m.undoStack, undo)
				if len(m.undoStack) > 50 {
					m.undoStack = m.undoStack[len(m.undoStack)-50:]
				}
				cur.Status = domain.StatusDone
				m.statusMsg = fmt.Sprintf("Marked %s as DONE (press 'u' to undo)", cur.ID)
				return m, m.markDoneCmd(cur.ID)
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
			cur := m.currentTask()
			if cur != nil {
				newStatus := domain.StatusInProgress
				if cur.Status == domain.StatusInProgress {
					newStatus = domain.StatusOpen
				} else if cur.Status == domain.StatusDone || cur.Status == domain.StatusClosed {
					newStatus = domain.StatusInProgress
				}
				undo := UndoAction{
					Kind:        UndoStatusChange,
					Description: fmt.Sprintf("Status change on %s", cur.ID),
					Snapshots: []TaskUndoSnapshot{
						{
							Task:             cur,
							PreviousStatus:   cur.Status,
							PreviousAssignee: cur.Assignee,
							PreviousClosedAt: cur.ClosedAt,
							PreviousTags:     append([]string(nil), cur.Tags...),
						},
					},
				}
				m.undoStack = append(m.undoStack, undo)
				if len(m.undoStack) > 50 {
					m.undoStack = m.undoStack[len(m.undoStack)-50:]
				}
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

func (m Model) linkTaskCmd(taskID, prereqID string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.AddDependency(taskID, prereqID)
		if err != nil {
			return errMsg(fmt.Errorf("failed to link %s -> %s: %w", taskID, prereqID, err))
		}
		return m.fetchTasksCmd()()
	}
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
		if m.client == nil {
			return nil
		}
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
		if m.client == nil {
			return nil
		}
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
		if m.client == nil {
			return nil
		}
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
		if m.client == nil {
			return nil
		}
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

func (m Model) updateTaskTagsCmd(taskID string, tags []string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
			ID:   taskID,
			Tags: &tags,
		})
		if err != nil {
			return errMsg(err)
		}
		return m.fetchTasksCmd()()
	}
}

func (m Model) batchTagCmd(ids []string, tag string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		for _, id := range ids {
			_, _ = m.client.UpdateTask(protocol.UpdateTaskPayload{
				ID:      id,
				AddTags: []string{tag},
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
		if m.client == nil {
			return nil
		}
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
		if m.client == nil {
			return nil
		}
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
		if m.client == nil {
			return nil
		}
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
		if m.client == nil {
			return nil
		}
		for _, id := range ids {
			_ = m.client.DeleteTask(id)
		}
		filter := domain.TaskFilter{
			Scope:           m.scope,
			RepoPath:        m.repoPath,
			Search:          m.filterText,
			IncludeArchived: m.showArchived,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) archiveTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		actor := m.client.Actor()
		if actor == "" {
			actor = git.GetAuthor()
		}
		_, err := m.client.ArchiveTask(taskID, actor, "Archived via TUI")
		if err != nil {
			return errMsg(err)
		}
		filter := domain.TaskFilter{
			Scope:           m.scope,
			RepoPath:        m.repoPath,
			Search:          m.filterText,
			IncludeArchived: m.showArchived,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) unarchiveTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		actor := m.client.Actor()
		if actor == "" {
			actor = git.GetAuthor()
		}
		_, err := m.client.UnarchiveTask(taskID, actor, "Unarchived via TUI")
		if err != nil {
			return errMsg(err)
		}
		filter := domain.TaskFilter{
			Scope:           m.scope,
			RepoPath:        m.repoPath,
			Search:          m.filterText,
			IncludeArchived: m.showArchived,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) batchArchiveCmd(ids []string, archive bool) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		actor := m.client.Actor()
		if actor == "" {
			actor = git.GetAuthor()
		}
		for _, id := range ids {
			if archive {
				_, _ = m.client.ArchiveTask(id, actor, "Archived via TUI batch")
			} else {
				_, _ = m.client.UnarchiveTask(id, actor, "Unarchived via TUI batch")
			}
		}
		filter := domain.TaskFilter{
			Scope:           m.scope,
			RepoPath:        m.repoPath,
			Search:          m.filterText,
			IncludeArchived: m.showArchived,
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
	openCount, progCount, doneCount, orphanCount, archivedCount := 0, 0, 0, 0, 0
	for _, t := range m.tasks {
		if t.IsArchived() {
			archivedCount++
			continue
		}

		isOrphan := t.IsOrphaned()
		if isOrphan {
			orphanCount++
		}

		switch t.Status {
		case domain.StatusOpen:
			openCount++
		case domain.StatusInProgress:
			if !isOrphan {
				progCount++
			}
		case domain.StatusDone, domain.StatusClosed:
			doneCount++
		default:
			openCount++
		}
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(WaveAqua2))
	var scopeBadge string
	if m.scope == "global" {
		scopeBadge = theme.ScopeGlobal.Render("[global]")
	} else {
		scopeBadge = theme.ScopeLocal.Render("[local]")
	}

	var orphanLabel string
	if orphanCount > 0 {
		orphanLabel = theme.CountOrphaned.Render(fmt.Sprintf("%d Orphaned", orphanCount))
	} else {
		orphanLabel = theme.CountOrphanedZero.Render("0 Orphaned")
	}

	countsLabel := fmt.Sprintf("%s │ %s │ %s │ %s",
		theme.CountOpen.Render(fmt.Sprintf("%d Open", openCount)),
		theme.CountProg.Render(fmt.Sprintf("%d Active", progCount)),
		theme.CountDone.Render(fmt.Sprintf("%d Done", doneCount)),
		orphanLabel,
	)
	if m.showArchived || archivedCount > 0 {
		countsLabel += fmt.Sprintf(" │ %s", theme.CountArchived.Render(fmt.Sprintf("%d Archived", archivedCount)))
	}
	modeBadge := ""
	if m.treeViewMode {
		modeBadge = "  ─  " + theme.ModalTitle.Render("[TREE MODE (t)]")
	}
	if m.showArchived {
		modeBadge += "  ─  " + theme.ArchiveBadge.Render("[ARCHIVE VIEW]")
	}
	appName := "task_cli"
	if m.scope == "local" && m.repoPath != "" {
		appName = filepath.Base(m.repoPath)
	} else if m.scope == "global" {
		appName = "global"
	}
	headerText := fmt.Sprintf("%s %s%s  ─  %s", titleStyle.Render("tk: "+appName), scopeBadge, modeBadge, countsLabel)
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
	if m.isLinking {
		return m.overlayModalString(rawView, m.renderLinkModal(width, height))
	}
	if m.isTagging {
		return m.overlayModalString(rawView, m.renderTagModal(width, height))
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
	colTitle := "TITLE"
	if m.treeViewMode {
		colTitle = "TREE / TITLE"
	}
	var colHeader string
	if tagsWidth > 0 {
		colHeader = fmt.Sprintf("  %s %s %s %s %s %s",
			padRight("ID", idWidth),
			padRight("STATUS", statusWidth),
			padRight("PRI", priWidth),
			padRight(colTitle, titleWidth),
			padRight("TAGS", tagsWidth),
			padRight("AGE", ageWidth),
		)
	} else {
		colHeader = fmt.Sprintf("  %s %s %s %s %s",
			padRight("ID", idWidth),
			padRight("STATUS", statusWidth),
			padRight("PRI", priWidth),
			padRight(colTitle, titleWidth),
			padRight("AGE", ageWidth),
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
		var displayTasks []*domain.Task
		var displayPrefixes []string
		if m.treeViewMode {
			treeRows := m.getTreeRows()
			for _, r := range treeRows {
				displayTasks = append(displayTasks, r.Task)
				displayPrefixes = append(displayPrefixes, r.Prefix)
			}
		} else {
			displayTasks = m.tasks
			for range m.tasks {
				displayPrefixes = append(displayPrefixes, "")
			}
		}

		endIdx := m.scrollOffset + maxRows
		if endIdx > len(displayTasks) {
			endIdx = len(displayTasks)
		}

		for i := m.scrollOffset; i < endIdx; i++ {
			t := displayTasks[i]
			treeBranch := displayPrefixes[i]
			if m.treeViewMode && treeBranch != "" {
				maxBranch := titleWidth - 10
				if maxBranch < 6 {
					maxBranch = titleWidth / 2
				}
				if maxBranch > 0 && runewidth.StringWidth(treeBranch) > maxBranch {
					treeBranch = compactTreeBranch(treeBranch, maxBranch)
				}
			}
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

			rawTitle := treeBranch + t.Title
			if t.IsBlocked {
				rawTitle += " 🔒"
			} else if t.Status != domain.StatusDone && t.Status != domain.StatusClosed && len(t.DependsOn) > 0 {
				rawTitle += " ⚡"
			}
			if t.IsOrphaned() {
				rawTitle += " 👻"
			}
			title := truncateToWidth(rawTitle, titleWidth)

			age := timeAgo(t.CreatedAt)
			if t.CommitSHA != "" {
				sha := t.CommitSHA
				if len(sha) > 6 {
					sha = sha[:6]
				}
				age = fmt.Sprintf("%s (%s)", age, sha)
			}
			age = truncateToWidth(age, ageWidth)
			statusText := string(t.Status)
			if t.IsOrphaned() {
				statusText = "orphan"
			}
			status := truncateToWidth(statusText, statusWidth)
			prio := truncateToWidth(string(t.Priority), priWidth)

			idStr := theme.TaskID.Render(padRight(t.ID, idWidth))
			var statusStyle lipgloss.Style
			if t.IsOrphaned() {
				statusStyle = theme.CountOrphaned
			} else {
				statusStyle = StatusStyle(t.Status)
			}
			statusStr := statusStyle.Render(padRight(status, statusWidth))
			priStr := PriorityStyle(t.Priority).Render(padRight(prio, priWidth))
			titleStr := theme.TaskTitle.Render(padRight(title, titleWidth))
			ageDisplay := theme.Age.Render(padRight(age, ageWidth))

			var line string
			if tagsWidth > 0 {
				tagsStr := strings.Join(t.Tags, ",")
				tagsStr = truncateToWidth(tagsStr, tagsWidth)
				tagsDisplay := theme.Tag.Render(padRight(tagsStr, tagsWidth))
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
		statusDisplay := string(t.Status)
		statusStyle := StatusStyle(t.Status)
		if t.IsOrphaned() {
			statusDisplay = fmt.Sprintf("%s 👻 (ORPHANED)", string(t.Status))
			statusStyle = theme.CountOrphaned
		}
		metaLine1 := fmt.Sprintf("  %s %s   %s %s   %s %s",
			theme.DetailLabel.Render("Status:"),
			statusStyle.Render(statusDisplay),
			theme.DetailLabel.Render("Priority:"),
			PriorityStyle(t.Priority).Render(string(t.Priority)),
			theme.DetailLabel.Render("Scope:"),
			theme.TaskID.Render(t.Scope),
		)

		var metaParts []string
		if t.Assignee != "" {
			assigneeDisplay := t.Assignee
			if t.IsOrphaned() {
				assigneeDisplay += " [STALE / TIMED OUT]"
			}
			metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Assignee:"), theme.NoteHeader.Render(assigneeDisplay)))
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
					author := truncateToWidth(n.Author, availAuthor)
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
	if m.treeViewMode {
		if tableWidth >= 80 {
			tagsWidth = 10
		}
	} else {
		if tableWidth >= 65 {
			tagsWidth = 10
		}
	}
	fixed := 39
	if tagsWidth > 0 {
		fixed += tagsWidth + 1
	}
	titleWidth := tableWidth - fixed
	if titleWidth < 12 {
		titleWidth = 12
	}

	colTitle := "TITLE"
	if m.treeViewMode {
		colTitle = "TREE / TITLE"
	}
	var colHeader string
	if tagsWidth > 0 {
		colHeader = fmt.Sprintf("  %s %s %s %s %s %s",
			padRight("ID", idWidth),
			padRight("STATUS", statusWidth),
			padRight("PRI", priWidth),
			padRight(colTitle, titleWidth),
			padRight("TAGS", tagsWidth),
			padRight("AGE", ageWidth),
		)
	} else {
		colHeader = fmt.Sprintf("  %s %s %s %s %s",
			padRight("ID", idWidth),
			padRight("STATUS", statusWidth),
			padRight("PRI", priWidth),
			padRight(colTitle, titleWidth),
			padRight("AGE", ageWidth),
		)
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

	if len(m.tasks) == 0 {
		leftLines = append(leftLines, theme.RowNormal.Width(tableWidth).Render("  No tasks found. Press 'n' to create."))
	} else {
		var displayTasks []*domain.Task
		var displayPrefixes []string
		if m.treeViewMode {
			treeRows := m.getTreeRows()
			for _, r := range treeRows {
				displayTasks = append(displayTasks, r.Task)
				displayPrefixes = append(displayPrefixes, r.Prefix)
			}
		} else {
			displayTasks = m.tasks
			for range m.tasks {
				displayPrefixes = append(displayPrefixes, "")
			}
		}

		endIdx := m.scrollOffset + maxRows
		if endIdx > len(displayTasks) {
			endIdx = len(displayTasks)
		}

		for i := m.scrollOffset; i < endIdx; i++ {
			t := displayTasks[i]
			treeBranch := displayPrefixes[i]
			if m.treeViewMode && treeBranch != "" {
				maxBranch := titleWidth - 10
				if maxBranch < 6 {
					maxBranch = titleWidth / 2
				}
				if maxBranch > 0 && runewidth.StringWidth(treeBranch) > maxBranch {
					treeBranch = compactTreeBranch(treeBranch, maxBranch)
				}
			}
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

			rawTitle := treeBranch + t.Title
			if t.IsBlocked {
				rawTitle += " 🔒"
			} else if t.Status != domain.StatusDone && t.Status != domain.StatusClosed && len(t.DependsOn) > 0 {
				rawTitle += " ⚡"
			}
			if t.IsOrphaned() {
				rawTitle += " 👻"
			}
			title := truncateToWidth(rawTitle, titleWidth)

			age := timeAgo(t.CreatedAt)
			if t.CommitSHA != "" {
				sha := t.CommitSHA
				if len(sha) > 6 {
					sha = sha[:6]
				}
				age = fmt.Sprintf("%s (%s)", age, sha)
			}
			age = truncateToWidth(age, ageWidth)
			statusText := string(t.Status)
			if t.IsOrphaned() {
				statusText = "orphan"
			}
			status := truncateToWidth(statusText, statusWidth)
			prio := truncateToWidth(string(t.Priority), priWidth)

			idStr := theme.TaskID.Render(padRight(t.ID, idWidth))
			var statusStyle lipgloss.Style
			if t.IsOrphaned() {
				statusStyle = theme.CountOrphaned
			} else {
				statusStyle = StatusStyle(t.Status)
			}
			statusStr := statusStyle.Render(padRight(status, statusWidth))
			priStr := PriorityStyle(t.Priority).Render(padRight(prio, priWidth))
			titleStr := theme.TaskTitle.Render(padRight(title, titleWidth))
			ageDisplay := theme.Age.Render(padRight(age, ageWidth))

			var line string
			if tagsWidth > 0 {
				tagsStr := strings.Join(t.Tags, ",")
				tagsStr = truncateToWidth(tagsStr, tagsWidth)
				tagsDisplay := theme.Tag.Render(padRight(tagsStr, tagsWidth))
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

func (m Model) renderLinkModal(termWidth, termHeight int) string {
	theme := m.theme
	boxWidth := 56
	if termWidth > 0 && termWidth-6 < boxWidth {
		boxWidth = termWidth - 6
	}
	if boxWidth < 36 {
		boxWidth = 36
	}

	contentWidth := boxWidth - 4
	var lines []string

	titleStr := theme.ModalTitle.Render(" LINK PREREQUISITE DEPENDENCY ")
	lines = append(lines, titleStr)
	lines = append(lines, "")

	promptStr := fmt.Sprintf("Make task %s depend on prerequisite:", m.linkTaskID)
	for _, pw := range wrapText(promptStr, contentWidth, "") {
		lines = append(lines, theme.ModalPrompt.Render(pw))
	}
	lines = append(lines, "")

	inputStr := fmt.Sprintf("  Prereq Task ID: %s_", m.linkInput)
	lines = append(lines, theme.SearchPrompt.Render(inputStr))
	lines = append(lines, "")

	hint := fmt.Sprintf("E.g. enter T-1 to require T-1 before %s can be closed.", m.linkTaskID)
	for _, hw := range wrapText(hint, contentWidth, "") {
		lines = append(lines, theme.ModalDesc.Render(hw))
	}
	lines = append(lines, "")

	confirmKey := theme.ModalKey.Render("[Enter]") + " " + theme.ModalPrompt.Render("Link")
	cancelKey := theme.ModalCancel.Render("[Esc]") + " " + theme.ModalPrompt.Render("Cancel")
	lines = append(lines, fmt.Sprintf("%s    %s", confirmKey, cancelKey))

	inner := strings.Join(lines, "\n")
	return theme.ModalBox.Width(boxWidth).Render(inner)
}

func (m Model) renderTagModal(termWidth, termHeight int) string {
	theme := m.theme
	boxWidth := 56
	if termWidth > 0 && termWidth-6 < boxWidth {
		boxWidth = termWidth - 6
	}
	if boxWidth < 36 {
		boxWidth = 36
	}

	contentWidth := boxWidth - 4
	var lines []string

	titleStr := theme.ModalTitle.Render(" QUICK TAG TASK ")
	lines = append(lines, titleStr)
	lines = append(lines, "")

	var promptStr string
	if m.tagTaskID != "" {
		promptStr = fmt.Sprintf("Add or toggle tag for task %s:", m.tagTaskID)
	} else {
		promptStr = fmt.Sprintf("Add tag for %d selected tasks:", len(m.selected))
	}
	for _, pw := range wrapText(promptStr, contentWidth, "") {
		lines = append(lines, theme.ModalPrompt.Render(pw))
	}
	lines = append(lines, "")

	inputStr := fmt.Sprintf("  Tag: %s_", m.tagInput)
	lines = append(lines, theme.SearchPrompt.Render(inputStr))
	lines = append(lines, "")

	hint := "Type tag name to add (or toggle off if already present)."
	for _, hw := range wrapText(hint, contentWidth, "") {
		lines = append(lines, theme.ModalDesc.Render(hw))
	}
	lines = append(lines, "")

	confirmKey := theme.ModalKey.Render("[Enter]") + " " + theme.ModalPrompt.Render("Apply")
	cancelKey := theme.ModalCancel.Render("[Esc]") + " " + theme.ModalPrompt.Render("Cancel")
	lines = append(lines, fmt.Sprintf("%s    %s", confirmKey, cancelKey))

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
	return m.overlayModalString(bg, modalStr)
}

func (m Model) overlayModalString(bg string, modalStr string) string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}

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
		maxStatusLen := 20
		if width < 80 {
			maxStatusLen = 12
		}
		prefixParts = append(prefixParts, theme.FooterStatus.Render("["+truncateToWidth(m.statusMsg, maxStatusLen)+"]"))
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
			key("m", fmt.Sprintf("Tag(%d)", selCount)),
			key("Space", "Sel"),
			theme.FilterIndicator.Render("[Esc] Clear"),
			theme.Age.Render("[q] Quit"),
		)
	} else if m.activePane == PaneDetail {
		curTask := m.currentTask()
		doneLabel := theme.CountProg.Render("[d] Done")
		statusLabel := theme.CountOpen.Render("[s] Prog")
		archiveLabel := theme.CountArchived.Render("[A] Arc")
		if curTask != nil {
			if curTask.Status == domain.StatusDone || curTask.Status == domain.StatusClosed {
				doneLabel = theme.CountDone.Render("[d] Reopen")
			}
			if curTask.Status == domain.StatusInProgress {
				statusLabel = theme.CountOpen.Render("[s] Open")
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
			key("Enter", "Diff"),
			doneLabel,
			statusLabel,
			archiveLabel,
			theme.DiffSub.Render("[x] Del"),
			key("L", "Link"),
			key("m", "Tag"),
			theme.Age.Render("[q] Quit"),
		)
	} else {
		// List Pane (Normal mode)
		curTask := m.currentTask()
		doneLabel := theme.CountProg.Render("[d] Done")
		statusLabel := theme.CountOpen.Render("[s] Prog")
		archiveLabel := theme.CountArchived.Render("[A] Arc")
		if curTask != nil {
			if curTask.Status == domain.StatusDone || curTask.Status == domain.StatusClosed {
				doneLabel = theme.CountDone.Render("[d] Reopen")
			}
			if curTask.Status == domain.StatusInProgress {
				statusLabel = theme.CountOpen.Render("[s] Open")
			}
			if curTask.IsArchived() {
				archiveLabel = theme.CountArchived.Render("[A] Unarc")
			}
		}

		treeLabel := key("t", "Tree")
		if m.treeViewMode {
			treeLabel = theme.ModalKey.Render("[t] List")
		}

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
				key("L", "Link"),
				key("Space", "Sel"),
				key("n", "New"),
				key("e", "Edit"),
				key("a", "Claim"),
				doneLabel,
				statusLabel,
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
				key("Space", "Sel"),
				key("n", "New"),
				key("e", "Edit"),
				key("a", "Claim"),
				doneLabel,
				statusLabel,
				archiveLabel,
				theme.DiffSub.Render("[x] Del"),
				key("/", "Find"),
				theme.Age.Render("[q] Quit"),
			)
		} else {
			// Ultra-compact footer for narrow screens (< 80 cols)
			candidateKeys = []string{
				key("j/k", "Nav"),
				key("a", "Claim"),
			}
			if undoKey != "" {
				candidateKeys = append(candidateKeys, undoKey)
			}
			candidateKeys = append(candidateKeys,
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
		if currentWidth+sepW+kW <= maxContentWidth {
			activeKeys = append(activeKeys, k)
			currentWidth += sepW + kW
		} else {
			break
		}
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

func isASCIIOnly() bool {
	if strings.ToLower(os.Getenv("TERM")) == "dumb" {
		return true
	}
	lc := os.Getenv("LC_ALL")
	if lc == "" {
		lc = os.Getenv("LANG")
	}
	return lc == "C" || lc == "POSIX"
}

// truncateToWidth truncates s so its terminal visual display width does not exceed maxWidth.
// If truncated, it appends ".." within the maxWidth budget. It never slices multi-byte UTF-8 runes.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	w := runewidth.StringWidth(s)
	if w <= maxWidth {
		return s
	}
	if maxWidth <= 2 {
		return strings.Repeat(".", maxWidth)
	}

	target := maxWidth - 2
	var sb strings.Builder
	curW := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if curW+rw > target {
			break
		}
		sb.WriteRune(r)
		curW += rw
	}
	sb.WriteString("..")
	return sb.String()
}

// padRight pads s with spaces so its visual terminal display width equals targetWidth.
func padRight(s string, targetWidth int) string {
	w := runewidth.StringWidth(s)
	if w >= targetWidth {
		return s
	}
	return s + strings.Repeat(" ", targetWidth-w)
}

// compactTreeBranch compacts deep tree branch indentation so it does not exceed maxW visual cells,
// ensuring the connector and task title remain clearly visible without getting squeezed out.
func compactTreeBranch(branch string, maxW int) string {
	w := runewidth.StringWidth(branch)
	if w <= maxW || maxW <= 0 {
		return branch
	}

	ascii := isASCIIOnly()
	connectorW := 4
	if maxW < connectorW+2 {
		return truncateToWidth(branch, maxW)
	}

	var connector string
	switch {
	case strings.HasSuffix(branch, "├── "):
		connector = "├── "
	case strings.HasSuffix(branch, "└── "):
		connector = "└── "
	case strings.HasSuffix(branch, "|-- "):
		connector = "|-- "
	case strings.HasSuffix(branch, "\\-- "):
		connector = "\\-- "
	default:
		runes := []rune(branch)
		if len(runes) >= 4 {
			connector = string(runes[len(runes)-4:])
		} else {
			return truncateToWidth(branch, maxW)
		}
	}

	ellipsis := "..│ "
	if ascii {
		ellipsis = "..| "
	}
	if maxW < 8 {
		ellipsis = ".."
	}
	elW := runewidth.StringWidth(ellipsis)
	avail := maxW - connectorW - elW
	if avail <= 0 {
		return ellipsis + connector
	}

	prefixWithoutConnector := branch[:len(branch)-len(connector)]
	runes := []rune(prefixWithoutConnector)
	var middle string
	for len(runes) >= 4 {
		lastChunk := string(runes[len(runes)-4:])
		cw := runewidth.StringWidth(lastChunk)
		if avail-cw < 0 {
			break
		}
		middle = lastChunk + middle
		avail -= cw
		runes = runes[:len(runes)-4]
	}

	return ellipsis + middle + connector
}

