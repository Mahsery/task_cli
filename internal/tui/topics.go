package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

// topicsMsg delivers the topic tree of one scope. Results for a scope other than the
// model's current one are stale (the user toggled scope meanwhile) and ignored.
type topicsMsg struct {
	scope  string
	topics []*domain.Topic
}

// topicOpMsg reports a successful topic mutation. When selectPath is set the
// sidebar switches to that topic after the topic list is refreshed.
type topicOpMsg struct {
	status     string
	selectPath *string
}

// sidebarEntry is one row of the topic sidebar: a virtual entry (All, Tasks, Posts, Unfiled) or a topic.
type sidebarEntry struct {
	Label string
	Path  string // "" = all topics, domain.UnfiledTopic, or a topic path
	Kind  domain.ItemKind
	Topic *domain.Topic
	Depth int
}

// orderedTopics returns topics in tree order (parents before children, siblings in
// server order) with their depth, independent of the order they were delivered in.
func orderedTopics(topics []*domain.Topic) []sidebarEntry {
	byID := make(map[int64]bool, len(topics))
	for _, t := range topics {
		byID[t.ID] = true
	}
	children := make(map[int64][]*domain.Topic)
	var roots []*domain.Topic
	for _, t := range topics {
		if t.ParentID == nil || !byID[*t.ParentID] {
			roots = append(roots, t)
			continue
		}
		children[*t.ParentID] = append(children[*t.ParentID], t)
	}
	out := make([]sidebarEntry, 0, len(topics))
	seen := make(map[int64]bool, len(topics))
	var walk func(t *domain.Topic, depth int)
	walk = func(t *domain.Topic, depth int) {
		if seen[t.ID] {
			return
		}
		seen[t.ID] = true
		out = append(out, sidebarEntry{Label: t.Slug, Path: topicPathOf(t), Topic: t, Depth: depth})
		for _, c := range children[t.ID] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}

func topicPathOf(t *domain.Topic) string {
	if t.Path != "" {
		return t.Path
	}
	return t.Slug
}

// allSidebarEntries lists the virtual entries followed by every topic in tree order.
func (m Model) allSidebarEntries() []sidebarEntry {
	entries := []sidebarEntry{
		{Label: "All", Path: ""},
		{Label: "All Tasks", Path: "", Kind: domain.KindTask},
		{Label: "All Posts", Path: "", Kind: domain.KindPost},
		{Label: "Unfiled", Path: domain.UnfiledTopic},
	}
	return append(entries, orderedTopics(m.topics)...)
}

// sidebarEntries is allSidebarEntries minus the descendants of collapsed groups.
func (m Model) sidebarEntries() []sidebarEntry {
	all := m.allSidebarEntries()
	if len(m.collapsed) == 0 {
		return all
	}
	out := make([]sidebarEntry, 0, len(all))
	for _, e := range all {
		hidden := false
		for p, c := range m.collapsed {
			if c && strings.HasPrefix(e.Path, p+"/") {
				hidden = true
				break
			}
		}
		if !hidden {
			out = append(out, e)
		}
	}
	return out
}

func (m Model) topicByPath(path string) *domain.Topic {
	if path == "" || path == domain.UnfiledTopic {
		return nil
	}
	for _, t := range m.topics {
		if topicPathOf(t) == path {
			return t
		}
	}
	return nil
}

// currentTopic returns the selected real topic, or nil for All/Unfiled.
func (m Model) currentTopic() *domain.Topic {
	return m.topicByPath(m.topicPath)
}

// isBoardView reports whether the list shows a board topic (flat posts, no tree).
func (m Model) isBoardView() bool {
	if m.kindFilter == domain.KindPost {
		return true
	}
	if m.kindFilter == domain.KindTask {
		return false
	}
	t := m.currentTopic()
	return t != nil && t.Kind == domain.TopicBoard
}

// topicLabel renders a topic filter for headers and pickers.
func topicLabel(path string) string {
	switch path {
	case "":
		return "all topics"
	case domain.UnfiledTopic:
		return "unfiled"
	}
	return "@" + path
}

func topicGlyph(e sidebarEntry, collapsed bool) string {
	ascii := isASCIIOnly()
	if e.Topic == nil {
		if e.Path == "" {
			switch e.Kind {
			case domain.KindTask:
				if ascii {
					return "-"
				}
				return "•"
			case domain.KindPost:
				if ascii {
					return "#"
				}
				return "▣"
			default:
				return "*"
			}
		}
		if ascii {
			return "0"
		}
		return "∅"
	}
	switch e.Topic.Kind {
	case domain.TopicGroup:
		switch {
		case ascii && collapsed:
			return ">"
		case ascii:
			return "v"
		case collapsed:
			return "▸"
		}
		return "▾"
	case domain.TopicBoard:
		if ascii {
			return "#"
		}
		return "▣"
	}
	if ascii {
		return "-"
	}
	return "•"
}

// syncSidebarCursor puts the sidebar cursor on the currently selected topic.
func (m *Model) syncSidebarCursor() {
	for i, e := range m.sidebarEntries() {
		if e.Path == m.topicPath && (e.Path != "" || e.Kind == m.kindFilter) {
			m.sidebarCursor = i
			return
		}
	}
	m.sidebarCursor = min(m.sidebarCursor, max(len(m.sidebarEntries())-1, 0))
}

// selectTopic switches the list filter to path and reloads the list.
func (m *Model) selectTopic(path string) tea.Cmd {
	if t := m.topicByPath(path); t != nil {
		// Make sure the selected topic is not hidden inside a collapsed group.
		for p := range m.collapsed {
			if strings.HasPrefix(path, p+"/") {
				delete(m.collapsed, p)
			}
		}
	}
	m.topicPath = path
	m.cursor = 0
	m.scrollOffset = 0
	m.detailScrollOffset = 0
	m.selected = make(map[string]bool)
	m.related = nil
	m.missing = nil
	m.syncSidebarCursor()
	if path == "" {
		switch m.kindFilter {
		case domain.KindTask:
			m.statusMsg = "Filter: all tasks"
		case domain.KindPost:
			m.statusMsg = "Filter: all posts"
		default:
			m.statusMsg = "Filter: all items"
		}
	} else {
		m.statusMsg = "Topic: " + topicLabel(path)
	}
	return m.fetchTasksCmd()
}

// cycleTopic moves the topic filter delta steps through All, Unfiled and the topic tree.
func (m *Model) cycleTopic(delta int) tea.Cmd {
	entries := m.allSidebarEntries()
	idx := 0
	for i, e := range entries {
		if e.Path == m.topicPath && (e.Path != "" || e.Kind == m.kindFilter) {
			idx = i
			break
		}
	}
	n := len(entries)
	next := ((idx+delta)%n + n) % n
	m.kindFilter = entries[next].Kind
	return m.selectTopic(entries[next].Path)
}

// cycleKindFilter cycles the item kind filter: "" -> task -> post -> "".
func (m *Model) cycleKindFilter() tea.Cmd {
	switch m.kindFilter {
	case "":
		m.kindFilter = domain.KindTask
		m.statusMsg = "Filter: tasks only"
	case domain.KindTask:
		m.kindFilter = domain.KindPost
		m.statusMsg = "Filter: posts only"
	case domain.KindPost:
		m.kindFilter = ""
		m.statusMsg = "Filter: all items"
	default:
		m.kindFilter = ""
		m.statusMsg = "Filter: all items"
	}
	m.cursor = 0
	m.scrollOffset = 0
	m.detailScrollOffset = 0
	m.selected = make(map[string]bool)
	m.syncSidebarCursor()
	return m.fetchTasksCmd()
}

// setScope switches between the repository and the global database. The topic
// filter and everything derived from the old scope is reset.
func (m *Model) setScope(scope string) {
	m.scope = scope
	if m.client != nil {
		m.client.Scope = scope
	}
	m.topicPath = ""
	m.kindFilter = ""
	m.topics = nil
	m.sidebarCursor = 0
	m.cursor = 0
	m.scrollOffset = 0
	m.detailScrollOffset = 0
	m.selected = make(map[string]bool)
	m.related = nil
	m.missing = nil
	m.anchor = nil
	m.linkCursor = 0
}

func otherScope(scope string) string {
	if scope == domain.ScopeGlobal {
		return domain.ScopeLocal
	}
	return domain.ScopeGlobal
}

func (m Model) fetchTopicsCmd() tea.Cmd {
	scope := m.scope
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		topics, err := m.client.ListTopics()
		if err != nil {
			return errMsg(fmt.Errorf("failed to load topics: %w", err))
		}
		return topicsMsg{scope: scope, topics: topics}
	}
}

func (m *Model) applyTopics(msg topicsMsg) {
	if msg.scope != m.scope {
		return
	}
	m.topics = msg.topics
	m.topicsLoaded = true
	if m.topicPath != "" && m.topicPath != domain.UnfiledTopic && m.currentTopic() == nil {
		// The selected topic vanished (deleted elsewhere): fall back to All on the next reload.
		m.topicPath = ""
	}
	m.syncSidebarCursor()
	if m.picker != nil && m.picker.kind == pickGoTopic {
		m.picker.items = m.topicPickerItems(m.picker.kind, nil)
		m.picker.cursor = 0
	}
}

// topicFormMode distinguishes creating a topic from renaming one.
type topicFormMode int

const (
	topicFormCreate topicFormMode = iota
	topicFormRename
)

// topicForm is the small modal behind N (new topic) and R (rename topic).
// The title is typed into m.input; tab cycles the kind and shift+tab toggles the parent.
type topicForm struct {
	mode          topicFormMode
	ref           string // topic being renamed
	kind          domain.TopicKind
	parent        string // parent path for new topics ("" = root)
	defaultParent string
}

var topicKinds = []domain.TopicKind{domain.TopicTasks, domain.TopicBoard, domain.TopicGroup}

func (m *Model) openTopicForm(mode topicFormMode) {
	sel := m.sidebarSelectedTopic()
	form := &topicForm{mode: mode, kind: domain.TopicTasks}
	m.focusInput()
	switch mode {
	case topicFormCreate:
		if sel != nil {
			form.defaultParent = topicPathOf(sel)
			form.parent = form.defaultParent
		}
	case topicFormRename:
		if sel == nil {
			m.statusMsg = "Select a topic to rename"
			return
		}
		form.ref = topicPathOf(sel)
		form.kind = sel.Kind
		m.input.SetValue(sel.Title)
		m.input.CursorEnd()
	}
	m.topicForm = form
}

// sidebarSelectedTopic is the real topic under the sidebar cursor (nil for All/Unfiled).
func (m Model) sidebarSelectedTopic() *domain.Topic {
	entries := m.sidebarEntries()
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(entries) {
		return nil
	}
	return entries[m.sidebarCursor].Topic
}

func (m Model) handleTopicFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	form := m.topicForm
	switch msg.String() {
	case "esc":
		m.topicForm = nil
		m.input.Blur()
		m.statusMsg = "Cancelled"
		return m, nil
	case "tab":
		if form.mode == topicFormCreate {
			for i, k := range topicKinds {
				if k == form.kind {
					form.kind = topicKinds[(i+1)%len(topicKinds)]
					break
				}
			}
		}
		return m, nil
	case "shift+tab":
		if form.mode == topicFormCreate {
			if form.parent == "" {
				form.parent = form.defaultParent
			} else {
				form.parent = ""
			}
		}
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.input.Value())
		if title == "" {
			m.statusMsg = "Topic title cannot be empty"
			return m, nil
		}
		m.topicForm = nil
		m.input.Blur()
		if form.mode == topicFormRename {
			return m, m.renameTopicCmd(form.ref, title)
		}
		return m, m.createTopicCmd(protocol.CreateTopicPayload{Title: title, Kind: form.kind, Parent: form.parent})
	}
	cmd := m.updateInput(msg)
	return m, cmd
}

func (m Model) createTopicCmd(p protocol.CreateTopicPayload) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		tp, err := m.client.CreateTopic(p)
		if err != nil {
			return errMsg(fmt.Errorf("create topic: %w", err))
		}
		path := topicPathOf(tp)
		return topicOpMsg{status: "Created topic @" + path, selectPath: &path}
	}
}

func (m Model) renameTopicCmd(ref, title string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		if _, err := m.client.UpdateTopic(protocol.UpdateTopicPayload{Ref: ref, Title: &title}); err != nil {
			return errMsg(fmt.Errorf("rename topic: %w", err))
		}
		return topicOpMsg{status: fmt.Sprintf("Renamed @%s to %q", ref, title)}
	}
}

func (m Model) deleteTopicCmd(ref, moveTo string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		if err := m.client.DeleteTopic(ref, moveTo); err != nil {
			return errMsg(fmt.Errorf("delete topic: %w", err))
		}
		all := ""
		return topicOpMsg{status: "Deleted topic @" + ref, selectPath: &all}
	}
}

func (m Model) moveTopicCmd(ref, parent string, wasSelected bool) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		tp, err := m.client.MoveTopic(ref, parent)
		if err != nil {
			return errMsg(fmt.Errorf("move topic: %w", err))
		}
		path := topicPathOf(tp)
		msg := topicOpMsg{status: fmt.Sprintf("Moved @%s to @%s", ref, path)}
		if wasSelected {
			msg.selectPath = &path
		}
		return msg
	}
}

// confirmDeleteTopic opens the shared confirmation dialog for deleting the topic
// under the sidebar cursor; a non-empty topic's items are moved to Unfiled.
func (m *Model) confirmDeleteTopic() {
	t := m.sidebarSelectedTopic()
	if t == nil {
		m.statusMsg = "Select a topic to delete"
		return
	}
	path := topicPathOf(t)
	dlg := &ConfirmDialog{
		Kind:     ConfirmDeleteTopic,
		Title:    "CONFIRM TOPIC DELETION",
		Prompt:   fmt.Sprintf("Delete topic @%s?", path),
		TopicRef: path,
	}
	if t.ItemCount > 0 {
		dlg.MoveTo = domain.UnfiledTopic
		dlg.Details = append(dlg.Details, fmt.Sprintf("%d item(s) will be moved to Unfiled", t.ItemCount))
	}
	if t.ChildCount > 0 {
		dlg.Details = append(dlg.Details, fmt.Sprintf("%d sub-topic(s) are affected", t.ChildCount))
	}
	m.confirmDialog = dlg
}

// handleSidebarKey handles keys while the topic sidebar is focused. It reports
// whether the key was consumed.
func (m Model) handleSidebarKey(k string, count int) (Model, tea.Cmd, bool) {
	entries := m.sidebarEntries()
	step := max(count, 1)
	switch k {
	case "j", "down":
		m.sidebarCursor = min(m.sidebarCursor+step, max(len(entries)-1, 0))
	case "k", "up":
		m.sidebarCursor = max(m.sidebarCursor-step, 0)
	case "G", "end":
		m.sidebarCursor = max(len(entries)-1, 0)
	case "home":
		m.sidebarCursor = 0
	case "enter", "l", "right":
		if m.sidebarCursor >= len(entries) {
			return m, nil, true
		}
		e := entries[m.sidebarCursor]
		m.activePane = PaneList
		if e.Path == m.topicPath && e.Kind == m.kindFilter {
			m.statusMsg = "Tasks focused"
			return m, nil, true
		}
		m.kindFilter = e.Kind
		cmd := m.selectTopic(e.Path)
		return m, cmd, true
	case " ", "space":
		if t := m.sidebarSelectedTopic(); t != nil && t.Kind == domain.TopicGroup {
			if m.collapsed == nil {
				m.collapsed = make(map[string]bool)
			}
			p := topicPathOf(t)
			m.collapsed[p] = !m.collapsed[p]
		}
	case "h", "left":
		// Already the leftmost pane.
	case "esc", "c":
		m.activePane = PaneList
		m.statusMsg = "Tasks focused"
	case "N":
		m.openTopicForm(topicFormCreate)
	case "R":
		m.openTopicForm(topicFormRename)
	case "D", "x":
		m.confirmDeleteTopic()
	case "M":
		t := m.sidebarSelectedTopic()
		if t == nil {
			m.statusMsg = "Select a topic to move"
			return m, nil, true
		}
		m.openPicker(pickMoveTopic, nil, topicPathOf(t))
	default:
		return m, nil, false
	}
	return m, nil, true
}

// renderSidebar renders the topic tree pane: exactly l.listHeight lines of l.sidebarWidth cells.
func (m Model) renderSidebar(l paneLayout) []string {
	theme := m.theme
	width := l.sidebarWidth
	focused := m.activePane == PaneSidebar

	headerStyle := theme.ColHeader
	if focused {
		headerStyle = theme.ActiveHeader
	}
	title := " TOPICS"
	if m.scope == domain.ScopeGlobal {
		title += " · global"
	} else {
		title += " · repo"
	}
	lines := []string{
		headerStyle.Render(fitLine(title, width)),
		theme.Border.Render(strings.Repeat("─", width)),
	}

	entries := m.sidebarEntries()
	rows := max(l.listHeight-len(lines), 1)
	start := 0
	if m.sidebarCursor >= rows {
		start = m.sidebarCursor - rows + 1
	}
	end := min(start+rows, len(entries))
	for i := start; i < end; i++ {
		e := entries[i]
		collapsed := e.Topic != nil && m.collapsed[e.Path]
		text := strings.Repeat("  ", e.Depth) + topicGlyph(e, collapsed) + " " + e.Label
		if e.Topic != nil && e.Topic.OpenCount > 0 {
			text += fmt.Sprintf(" (%d)", e.Topic.OpenCount)
		}
		mark := "  "
		if i == m.sidebarCursor && focused {
			mark = "► "
		}
		line := fitLine(mark+text, width)
		switch {
		case i == m.sidebarCursor && focused:
			line = theme.RowSelected.Render(line)
		case e.Path == m.topicPath && (e.Path != "" || e.Kind == m.kindFilter):
			line = theme.ActiveHeader.Render(line)
		case e.Topic == nil:
			line = theme.DetailLabel.Render(line)
		default:
			line = theme.RowNormal.Render(line)
		}
		lines = append(lines, line)
	}
	return fitBlock(lines, width, l.listHeight)
}

func (m Model) renderTopicForm(termWidth int) string {
	theme := m.theme
	form := m.topicForm
	boxWidth := min(max(termWidth-6, 36), 60)
	lines := []string{}
	if form.mode == topicFormRename {
		lines = append(lines, theme.ModalTitle.Render(" RENAME TOPIC "), "",
			theme.ModalPrompt.Render("Topic: @"+form.ref), "")
	} else {
		parent := "(root)"
		if form.parent != "" {
			parent = "@" + form.parent
		}
		lines = append(lines, theme.ModalTitle.Render(" NEW TOPIC "), "",
			theme.DetailLabel.Render("Kind:   ")+theme.ModalKey.Render(string(form.kind))+theme.ModalDesc.Render("  (tab: tasks/board/group)"),
			theme.DetailLabel.Render("Parent: ")+theme.ModalKey.Render(parent)+theme.ModalDesc.Render("  (shift+tab: toggle)"),
			"")
	}
	lines = append(lines,
		theme.SearchPrompt.Render(fmt.Sprintf("  Title: %s_", m.input.Value())), "",
		theme.ModalKey.Render("[Enter]")+" "+theme.ModalPrompt.Render("Save")+"    "+theme.ModalCancel.Render("[Esc]")+" "+theme.ModalPrompt.Render("Cancel"))
	return theme.ModalBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
}
