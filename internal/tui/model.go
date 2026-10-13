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
	PaneSidebar
)

type ConfirmKind int

const (
	ConfirmDelete ConfirmKind = iota
	ConfirmDone
	ConfirmForceDone
	ConfirmReopen
	ConfirmStatus
	ConfirmClaim
	ConfirmDeleteTopic
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
	// TopicRef and MoveTo are only used by ConfirmDeleteTopic.
	TopicRef string
	MoveTo   string
}

type UndoKind int

const (
	UndoStatusChange UndoKind = iota
	UndoClaim
	UndoDelete
	UndoArchive
)

type TaskUndoSnapshot struct {
	Task               *domain.Task
	PreviousStatus     domain.Status
	PreviousAssignee   string
	PreviousClosedAt   *time.Time
	PreviousArchivedAt *time.Time
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
	showArchived       bool
	input              textinput.Model
	viewport           viewport.Model

	// Topics (see topics.go). topicPath is the list filter: "" = all topics,
	// domain.UnfiledTopic, or a topic path such as "game/hero-system".
	topics        []*domain.Topic
	topicsLoaded  bool
	topicPath     string
	kindFilter    domain.ItemKind // "" = all kinds, domain.KindTask, or domain.KindPost
	sidebarCursor int
	sidebarHidden bool
	collapsed     map[string]bool // collapsed group topic paths

	// Two-key commands (g…, ]…, […): the pending prefix and the count typed before it.
	pendingPrefix string
	prefixCount   int

	// Items outside the visible list: cross-topic dependencies (ghost rows) and
	// link targets, plus link targets confirmed not to exist (see links.go).
	related map[string]*domain.Task
	missing map[string]bool

	// Link navigation in the detail pane (see links.go and history.go).
	linkCursor    int    // 1-based focused link, 0 = none
	linkCursorFor string // item the link cursor belongs to
	anchor        *detailAnchor
	pendingSelect *navTarget
	backStack     []navEntry
	fwdStack      []navEntry

	picker    *picker
	topicForm *topicForm
}

type TreeRow struct {
	Task   *domain.Task
	Prefix string
	Depth  int
	// Pointer marks a task already rendered in full above (multi-parent DAG node).
	Pointer bool
	// Ghost marks a task that lives outside the current topic (cross-topic dependency).
	Ghost bool
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

// Init loads topics before tasks: the client connection is not safe for concurrent use.
func (m Model) Init() tea.Cmd {
	return tea.Sequence(m.fetchTopicsCmd(), m.fetchTasksCmd())
}
