package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"task_cli/internal/client"
	"task_cli/internal/domain"
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
	input              textinput.Model
	viewport           viewport.Model
}

type TreeRow struct {
	Task   *domain.Task
	Prefix string
	Depth  int
}

func NewModel(c *client.Client, editorCmd, scope, repoPath string) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 200

	vp := viewport.New(0, 0)

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
		input:              ti,
		viewport:           vp,
	}
}

func (m *Model) focusInput() {
	m.input = textinput.New()
	m.input.Prompt = ""
	m.input.CharLimit = 200
	_ = m.input.Focus()
}

func (m *Model) updateInput(msg tea.Msg) tea.Cmd {
	if m.input.CharLimit == 0 {
		m.input = textinput.New()
		m.input.Prompt = ""
		m.input.CharLimit = 200
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

type reloadMsg []*domain.Task

type taskDetailMsg *domain.Task

type statusMsg string

type errMsg error

func (m Model) Init() tea.Cmd {
	return m.fetchTasksCmd()
}
