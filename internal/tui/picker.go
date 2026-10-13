package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

// pickerKind selects what a picker lists and what choosing an entry does.
type pickerKind int

const (
	pickGoTopic      pickerKind = iota // gt: jump to a topic (tab toggles repo/global)
	pickMoveItems                      // M in the list: move selected items to a topic
	pickMoveTopic                      // M in the sidebar: re-parent a topic
	pickPromote                        // p: promote a post into a tasks topic
	pickInsertLink                     // gl: append a [[link]] to the current item
	pickNewItemTopic                   // n in a group: pick sub-topic for new item
	pickUnlinkDep                      // U: remove prerequisite dependency
)

type pickerItem struct {
	Label  string // primary text (topic path or item ID)
	Detail string // secondary text (title)
	Value  string // topic path ("" = all/root) or item ID / "@topic"
}

// picker is the fuzzy-filter modal shared by topic jumps, moves, promotion and link insertion.
// The query lives in m.input.
type picker struct {
	kind      pickerKind
	title     string
	items     []pickerItem
	cursor    int
	targetIDs []string // items being moved / promoted / linked from
	ref       string   // topic being moved (pickMoveTopic)
	origScope string   // scope when the picker opened (pickGoTopic can toggle it)
}

// fuzzyScore ranks how well query matches s: contiguous substring matches beat
// scattered subsequence matches, earlier matches beat later ones. ok=false means no match.
func fuzzyScore(query, s string) (score int, ok bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return 0, true
	}
	low := strings.ToLower(s)
	if i := strings.Index(low, q); i >= 0 {
		return i, true
	}
	pos, gaps := 0, 0
	for _, r := range q {
		i := strings.IndexRune(low[pos:], r)
		if i < 0 {
			return 0, false
		}
		gaps += i
		pos += i + len(string(r))
	}
	return 1000 + gaps, true
}

// splitLinkQuery separates the item filter from an optional anchor in a gl query
// ("t12#L4" -> "t12", "#L4").
func splitLinkQuery(q string) (filter, anchor string) {
	if i := strings.IndexAny(q, "#:"); i >= 0 {
		a := q[i+1:]
		if q[i] == ':' {
			a = "L" + a
		}
		return q[:i], "#" + a
	}
	return q, ""
}

func (p *picker) query(m Model) string {
	q := m.input.Value()
	if p.kind == pickInsertLink {
		q, _ = splitLinkQuery(q)
	}
	return q
}

// filtered returns the items matching the current query, best match first.
func (p *picker) filtered(m Model) []pickerItem {
	q := p.query(m)
	type scored struct {
		item  pickerItem
		score int
		idx   int
	}
	var hits []scored
	for i, it := range p.items {
		if s, ok := fuzzyScore(q, it.Label+" "+it.Detail); ok {
			hits = append(hits, scored{it, s, i})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score < hits[b].score
		}
		return hits[a].idx < hits[b].idx
	})
	out := make([]pickerItem, len(hits))
	for i, h := range hits {
		out[i] = h.item
	}
	return out
}

// topicPickerItems lists destinations for the given picker kind. accept, when
// non-nil, restricts topics to those that may hold the given item kinds.
func (m Model) topicPickerItems(kind pickerKind, accept []domain.ItemKind) []pickerItem {
	var items []pickerItem
	switch kind {
	case pickGoTopic:
		items = append(items, pickerItem{Label: "* All", Detail: "every topic", Value: ""},
			pickerItem{Label: "∅ Unfiled", Detail: "items without a topic", Value: domain.UnfiledTopic})
	case pickMoveItems, pickPromote:
		items = append(items, pickerItem{Label: "∅ Unfiled", Detail: "no topic", Value: domain.UnfiledTopic})
	case pickMoveTopic:
		items = append(items, pickerItem{Label: "(root)", Detail: "top level", Value: ""})
	}
	for _, e := range orderedTopics(m.topics) {
		t := e.Topic
		if kind == pickMoveTopic && (e.Path == m.picker.ref || strings.HasPrefix(e.Path, m.picker.ref+"/")) {
			continue
		}
		if kind == pickNewItemTopic && !strings.HasPrefix(e.Path, m.picker.ref+"/") {
			continue
		}
		if len(accept) > 0 {
			ok := true
			for _, k := range accept {
				ok = ok && t.Kind.Accepts(k)
			}
			if !ok {
				continue
			}
		}
		items = append(items, pickerItem{Label: "@" + e.Path, Detail: fmt.Sprintf("%s · %s", t.Title, t.Kind), Value: e.Path})
	}
	return items
}

func (m Model) linkPickerItems(sourceID string) []pickerItem {
	var items []pickerItem
	seen := map[string]bool{sourceID: true}
	add := func(t *domain.Task) {
		if t == nil || seen[t.ID] {
			return
		}
		seen[t.ID] = true
		items = append(items, pickerItem{Label: t.ID, Detail: t.Title, Value: t.ID})
	}
	for _, t := range m.tasks {
		add(t)
	}
	ids := make([]string, 0, len(m.related))
	for id := range m.related {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		add(m.related[id])
	}
	for _, e := range orderedTopics(m.topics) {
		items = append(items, pickerItem{Label: "@" + e.Path, Detail: e.Topic.Title, Value: "@" + e.Path})
	}
	return items
}

// openPicker opens a picker of the given kind for targetIDs (items) or ref (topic).
func (m *Model) openPicker(kind pickerKind, targetIDs []string, ref string) {
	p := &picker{kind: kind, targetIDs: targetIDs, ref: ref, origScope: m.scope}
	m.picker = p
	m.focusInput()
	switch kind {
	case pickGoTopic:
		p.title = "GO TO TOPIC"
		p.items = m.topicPickerItems(kind, nil)
	case pickMoveItems:
		p.title = fmt.Sprintf("MOVE %d ITEM(S) TO TOPIC", len(targetIDs))
		p.items = m.topicPickerItems(kind, m.itemKinds(targetIDs))
	case pickMoveTopic:
		p.title = fmt.Sprintf("MOVE @%s UNDER", ref)
		p.items = m.topicPickerItems(kind, nil)
	case pickPromote:
		p.title = fmt.Sprintf("PROMOTE %s TO A TASK IN", strings.Join(targetIDs, ", "))
		p.items = m.topicPickerItems(kind, []domain.ItemKind{domain.KindTask})
	case pickInsertLink:
		p.title = "INSERT LINK INTO " + strings.Join(targetIDs, ", ")
		p.items = m.linkPickerItems(strings.Join(targetIDs, ""))
	case pickNewItemTopic:
		p.title = "NEW ITEM IN @" + ref + ": CHOOSE SUB-TOPIC"
		accept := []domain.ItemKind{domain.KindTask}
		if m.isBoardView() {
			accept = []domain.ItemKind{domain.KindPost}
		}
		p.items = m.topicPickerItems(kind, accept)
	case pickUnlinkDep:
		taskID := strings.Join(targetIDs, "")
		p.title = "UNLINK PREREQUISITE FROM " + taskID
		p.items = m.unlinkPickerItems(taskID)
	}
}

func (m Model) unlinkPickerItems(taskID string) []pickerItem {
	src := m.lookupItem(taskID)
	if src == nil {
		return nil
	}
	var items []pickerItem
	for _, depID := range src.DependsOn {
		detail := ""
		if dep := m.lookupItem(depID); dep != nil {
			detail = dep.Title
		}
		items = append(items, pickerItem{
			Label:  depID,
			Detail: detail,
			Value:  depID,
		})
	}
	return items
}

func (m Model) itemKinds(ids []string) []domain.ItemKind {
	set := map[domain.ItemKind]bool{}
	for _, id := range ids {
		if t := m.lookupItem(id); t != nil {
			k := t.Kind
			if k == "" {
				k = domain.KindTask
			}
			set[k] = true
		}
	}
	var out []domain.ItemKind
	for _, k := range []domain.ItemKind{domain.KindTask, domain.KindPost} {
		if set[k] {
			out = append(out, k)
		}
	}
	return out
}

func (m Model) closePicker(status string) Model {
	m.picker = nil
	m.input.Blur()
	m.input.Reset()
	m.statusMsg = status
	return m
}

func (m Model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	items := p.filtered(m)
	switch msg.String() {
	case "esc":
		restore := p.kind == pickGoTopic && m.scope != p.origScope
		m = m.closePicker("Cancelled")
		if restore {
			m.setScope(p.origScope)
			return m, tea.Sequence(m.fetchTopicsCmd(), m.fetchTasksCmd())
		}
		return m, nil
	case "down":
		p.cursor = min(p.cursor+1, max(len(items)-1, 0))
		return m, nil
	case "up":
		p.cursor = max(p.cursor-1, 0)
		return m, nil
	case "tab":
		if p.kind != pickGoTopic {
			return m, nil
		}
		next := otherScope(m.scope)
		if next == domain.ScopeLocal && m.repoPath == "" {
			m.statusMsg = "Not inside a repository: no repo topics"
			return m, nil
		}
		m.setScope(next)
		p.cursor = 0
		p.items = m.topicPickerItems(p.kind, nil)
		return m, m.fetchTopicsCmd()
	case "enter":
		var choice *pickerItem
		if p.cursor < len(items) {
			choice = &items[p.cursor]
		}
		return m.applyPicker(choice)
	}
	cmd := m.updateInput(msg)
	p.cursor = 0
	return m, cmd
}

// applyPicker performs the picker's action for the chosen entry.
func (m Model) applyPicker(choice *pickerItem) (tea.Model, tea.Cmd) {
	p := m.picker
	query := m.input.Value()
	m = m.closePicker("")
	switch p.kind {
	case pickGoTopic:
		if choice == nil {
			m.statusMsg = "No matching topic"
			return m, nil
		}
		m.pushHistory()
		cmd := m.selectTopic(choice.Value)
		return m, cmd
	case pickMoveItems:
		if choice == nil {
			m.statusMsg = "No matching topic"
			return m, nil
		}
		m.selected = make(map[string]bool)
		m.statusMsg = fmt.Sprintf("Moving %d item(s) to %s", len(p.targetIDs), topicLabel(choice.Value))
		return m, m.moveItemsCmd(p.targetIDs, choice.Value)
	case pickMoveTopic:
		if choice == nil {
			m.statusMsg = "No matching topic"
			return m, nil
		}
		wasSelected := m.topicPath == p.ref || strings.HasPrefix(m.topicPath, p.ref+"/")
		return m, m.moveTopicCmd(p.ref, choice.Value, wasSelected)
	case pickPromote:
		if choice == nil {
			m.statusMsg = "No matching topic"
			return m, nil
		}
		dest := choice.Value
		if dest == domain.UnfiledTopic {
			dest = ""
		}
		return m, m.promotePostCmd(p.targetIDs[0], dest)
	case pickInsertLink:
		return m.insertLink(p.targetIDs[0], choice, query)
	case pickNewItemTopic:
		if choice == nil {
			m.statusMsg = "No matching topic"
			return m, nil
		}
		return m.openNewTaskEditor(choice.Value)
	case pickUnlinkDep:
		if choice == nil {
			m.statusMsg = "No prerequisite selected"
			return m, nil
		}
		taskID := p.targetIDs[0]
		depID := choice.Value
		m.statusMsg = fmt.Sprintf("Unlinked dependency %s -> %s", taskID, depID)
		return m, m.unlinkTaskCmd(taskID, depID)
	}
	return m, nil
}

// insertLink appends the canonical [[...]] for the chosen target (with the optional
// anchor typed after '#' or ':') to the item's description.
func (m Model) insertLink(sourceID string, choice *pickerItem, query string) (tea.Model, tea.Cmd) {
	filter, anchor := splitLinkQuery(query)
	target := ""
	if choice != nil {
		target = choice.Value
	} else if id, err := domain.NormalizeID(filter, m.scope); err == nil {
		target = id
	}
	if target == "" {
		m.statusMsg = "No matching item"
		return m, nil
	}
	if strings.HasPrefix(target, "@") {
		anchor = "" // topics have no anchors
	}
	link, err := domain.ParseWikiLink(target+anchor, m.scope)
	if err != nil {
		m.err = err
		m.statusMsg = fmt.Sprintf("Error: %v", err)
		return m, nil
	}
	src := m.lookupItem(sourceID)
	if src == nil {
		m.statusMsg = "Item not loaded: " + sourceID
		return m, nil
	}
	desc := strings.TrimRight(src.Description, "\n")
	if desc != "" {
		desc += "\n"
	}
	desc += link.Canonical()
	src.Description = desc
	m.statusMsg = fmt.Sprintf("Linked %s → %s", sourceID, link.Display())
	return m, m.updateDescriptionCmd(sourceID, desc)
}

func (m Model) updateDescriptionCmd(id, desc string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{ID: id, Description: &desc})
		return m.reloadAfter(err)
	}
}

func (m Model) moveItemsCmd(ids []string, topic string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		_, err := m.client.MoveItems(ids, topic, m.resolveActor())
		return m.reloadAfter(err)
	}
}

func (m Model) promotePostCmd(id, topic string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		_, err := m.client.PromotePost(id, topic, m.resolveActor())
		if err != nil {
			err = fmt.Errorf("promote %s: %w", id, err)
		}
		return m.reloadAfter(err)
	}
}

func (m Model) renderPicker(termWidth, termHeight int) string {
	theme := m.theme
	p := m.picker
	boxWidth := min(max(termWidth-6, 36), 72)
	contentWidth := boxWidth - 4
	title := p.title
	if p.kind == pickGoTopic {
		if m.scope == domain.ScopeGlobal {
			title += " · GLOBAL"
		} else {
			title += " · REPO"
		}
	}
	lines := []string{theme.ModalTitle.Render(" " + truncateToWidth(title, contentWidth-2) + " "), ""}
	lines = append(lines, theme.SearchPrompt.Render(fmt.Sprintf("  > %s_", m.input.Value())), "")

	items := p.filtered(m)
	maxRows := max(min(10, termHeight-14), 3)
	start := 0
	if p.cursor >= maxRows {
		start = p.cursor - maxRows + 1
	}
	if len(items) == 0 {
		lines = append(lines, theme.ModalDesc.Render("  (no matches)"))
	}
	for i := start; i < min(start+maxRows, len(items)); i++ {
		it := items[i]
		row := truncateToWidth(fmt.Sprintf("%s  %s", it.Label, it.Detail), contentWidth-2)
		if i == p.cursor {
			lines = append(lines, theme.RowSelected.Render("► "+row))
		} else {
			lines = append(lines, theme.RowNormal.Render("  "+row))
		}
	}
	lines = append(lines, "")
	hint := "type to filter · ↑/↓ move · enter select · esc cancel"
	switch p.kind {
	case pickGoTopic:
		hint = "type to filter · ↑/↓ · enter · tab repo/global · esc"
	case pickInsertLink:
		hint = "type an ID, add #L12, #L3-L5 or #n2 for an anchor · enter insert · esc"
	}
	for _, h := range wrapText(hint, contentWidth, "") {
		lines = append(lines, theme.ModalDesc.Render(h))
	}
	return theme.ModalBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
}
