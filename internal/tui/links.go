package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"task_cli/internal/domain"
)

// relatedMsg delivers items referenced from the list but not part of it (ghost
// dependencies and link targets) and the link targets that do not exist.
type relatedMsg struct {
	scope   string
	found   map[string]*domain.Task
	missing map[string]bool
}

// maxRelatedFetch bounds the per-reload lookups of out-of-list items.
const maxRelatedFetch = 200

// relatedIDs lists item IDs referenced by the listed tasks (dependencies and
// [[links]], plus backlinks) that are not themselves listed.
func (m Model) relatedIDs() []string {
	listed := make(map[string]bool, len(m.tasks))
	for _, t := range m.tasks {
		listed[t.ID] = true
	}
	seen := make(map[string]bool)
	var ids []string
	add := func(id string) {
		if id == "" || listed[id] || seen[id] || strings.HasPrefix(id, "@") {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, t := range m.tasks {
		for _, id := range t.DependsOn {
			add(id)
		}
		for _, id := range t.Blocks {
			add(id)
		}
		for _, l := range domain.ParseWikiLinks(t.LinkText(), m.scope) {
			if !l.IsTopic {
				add(l.Target)
			}
		}
		for _, id := range t.Backlinks {
			add(id)
		}
	}
	if len(ids) > maxRelatedFetch {
		ids = ids[:maxRelatedFetch]
	}
	return ids
}

func (m Model) fetchRelatedCmd() tea.Cmd {
	ids := m.relatedIDs()
	if len(ids) == 0 || m.client == nil {
		return nil
	}
	scope := m.scope
	return func() tea.Msg {
		msg := relatedMsg{scope: scope, found: make(map[string]*domain.Task), missing: make(map[string]bool)}
		for _, id := range ids {
			t, err := m.client.GetTask(id)
			if err != nil || t == nil {
				msg.missing[id] = true
				continue
			}
			msg.found[id] = t
		}
		return msg
	}
}

// linkState classifies how a link renders.
type linkState int

const (
	linkOK      linkState = iota
	linkBroken            // the target item or topic does not exist
	linkStale             // the target exists but the #L / #n anchor is out of range
	linkUnknown           // not resolved yet
)

func (m Model) linkState(l domain.WikiLink) linkState {
	if l.IsTopic {
		if !m.topicsLoaded {
			return linkUnknown
		}
		if m.topicByPath(l.Target) == nil {
			return linkBroken
		}
		return linkOK
	}
	t := m.lookupItem(l.Target)
	if t == nil {
		if m.missing[l.Target] {
			return linkBroken
		}
		return linkUnknown
	}
	if _, ok := l.AnchorText(t); !ok {
		return linkStale
	}
	return linkOK
}

// superscript renders a link index as ¹²³ (or [n] on ASCII-only terminals).
func superscript(n int) string {
	if isASCIIOnly() {
		return fmt.Sprintf("[%d]", n)
	}
	digits := []rune("⁰¹²³⁴⁵⁶⁷⁸⁹")
	var b strings.Builder
	for _, r := range fmt.Sprint(n) {
		b.WriteRune(digits[r-'0'])
	}
	return b.String()
}

// detailLink is one navigable link of the detail pane: a [[link]] from the
// description or notes, or a backlink to an item that links here.
type detailLink struct {
	Index    int
	Link     domain.WikiLink
	Backlink bool
	Line     int // content line where the link is rendered (-1 if not rendered)
}

// detailDoc is the rendered detail pane plus the positions needed for navigation.
type detailDoc struct {
	lines    []string
	links    []detailLink
	descLine map[int]int // 1-based description line -> content line
	noteLine map[int]int // 1-based note number -> content line
}

// detailLinks lists the navigable links of t in display order: text links
// (description, then notes) followed by backlinks.
func (m Model) detailLinks(t *domain.Task) []detailLink {
	if t == nil {
		return nil
	}
	var out []detailLink
	for _, l := range domain.ParseWikiLinks(t.LinkText(), m.scope) {
		out = append(out, detailLink{Index: len(out) + 1, Link: l, Line: -1})
	}
	for _, id := range t.Backlinks {
		out = append(out, detailLink{Index: len(out) + 1, Link: domain.WikiLink{Target: id}, Backlink: true, Line: -1})
	}
	return out
}

// span is a run of styled text for wrapSpans; link > 0 makes it an unbreakable link token.
type span struct {
	text  string
	style lipgloss.Style
	link  int
}

// wrapSpans word-wraps styled spans to width (including indent). Link spans are
// never split. linkLines receives the wrapped line offset of every link index.
func wrapSpans(spans []span, width int, indent string, base lipgloss.Style, linkLines map[int]int) []string {
	avail := max(width-ansi.StringWidth(indent), 1)
	var tokens []span
	for i := range spans {
		s := &spans[i]
		if s.link > 0 {
			tokens = append(tokens, *s)
			continue
		}
		for _, w := range strings.Fields(s.text) {
			for ansi.StringWidth(w) > avail {
				head := ansi.Truncate(w, avail, "")
				tokens = append(tokens, span{head, s.style, 0})
				w = w[len(head):]
			}
			if w != "" {
				tokens = append(tokens, span{w, s.style, 0})
			}
		}
	}
	var lines []string
	var cur strings.Builder
	curLen := 0
	flush := func() {
		lines = append(lines, base.Render(indent)+cur.String())
		cur.Reset()
		curLen = 0
	}
	for i := range tokens {
		tk := &tokens[i]
		w := ansi.StringWidth(tk.text)
		if curLen > 0 && curLen+1+w > avail {
			flush()
		}
		if curLen > 0 {
			cur.WriteString(base.Render(" "))
			curLen++
		}
		if tk.link > 0 && linkLines != nil {
			if _, ok := linkLines[tk.link]; !ok {
				linkLines[tk.link] = len(lines)
			}
		}
		cur.WriteString(tk.style.Render(tk.text))
		curLen += w
	}
	if curLen > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

func (m Model) linkStyle(st linkState, focused bool) lipgloss.Style {
	theme := m.theme
	switch {
	case focused:
		return theme.LinkFocused
	case st == linkBroken:
		return theme.LinkBroken
	case st == linkStale:
		return theme.LinkStale
	}
	return theme.Link
}

// linkToken renders a link as [[display]] followed by its superscript index.
func linkToken(l domain.WikiLink, idx int) string {
	return "[[" + l.Display() + "]]" + superscript(idx)
}

// renderLinkedText renders text containing [[links]] (numbered from firstIdx in
// order of appearance) as wrapped lines. lineStarts receives, for each 1-based
// source line, the offset of its first wrapped line; linkLines the offset of each link.
func (m Model) renderLinkedText(text string, links []domain.WikiLink, firstIdx, width int, indent string,
	base func(srcLine int) lipgloss.Style, lineStarts map[int]int, linkLines map[int]int) []string {
	focused := m.focusedLinkIndex()
	var out []string
	offset := 0
	li := 0
	for n, line := range strings.Split(text, "\n") {
		lineStart, lineEnd := offset, offset+len(line)
		offset = lineEnd + 1
		st := base(n + 1)
		var spans []span
		pos := lineStart
		for li < len(links) && links[li].Start < lineEnd {
			l := links[li]
			idx := firstIdx + li
			if l.Start > pos {
				spans = append(spans, span{text: text[pos:l.Start], style: st})
			}
			spans = append(spans, span{text: linkToken(l, idx), style: m.linkStyle(m.linkState(l), idx == focused), link: idx})
			pos = l.End
			li++
		}
		if pos < lineEnd {
			spans = append(spans, span{text: text[pos:lineEnd], style: st})
		}
		if lineStarts != nil {
			lineStarts[n+1] = len(out)
		}
		local := make(map[int]int)
		wrapped := wrapSpans(spans, width, indent, st, local)
		for idx, off := range local {
			if linkLines != nil {
				linkLines[idx] = len(out) + off
			}
		}
		out = append(out, wrapped...)
	}
	return out
}

// focusedLinkIndex returns the focused link index for the current task, or 0.
func (m Model) focusedLinkIndex() int {
	t := m.currentTask()
	if t == nil || m.linkCursorFor != t.ID {
		return 0
	}
	return m.linkCursor
}

// linkSummary describes where a link points for the Links section.
func (m Model) linkSummary(dl detailLink) (string, linkState) {
	l := dl.Link
	if l.IsTopic {
		st := m.linkState(l)
		if tp := m.topicByPath(l.Target); tp != nil {
			return fmt.Sprintf("topic %s (%s)", tp.Title, tp.Kind), st
		}
		if st == linkBroken {
			return "topic not found", st
		}
		return "topic", st
	}
	st := m.linkState(l)
	t := m.lookupItem(l.Target)
	switch {
	case t == nil && st == linkBroken:
		return "not found", st
	case t == nil:
		return "…", st
	case st == linkStale:
		return t.Title + " — anchor out of range", st
	}
	where := ""
	if t.TopicPath != "" {
		where = "  @" + t.TopicPath
	}
	return fmt.Sprintf("%s [%s]%s", t.Title, t.StatusLabel(), where), st
}

// scrollToLine scrolls the detail pane so content line ln is visible, keeping a little context.
func (m *Model) scrollToLine(ln int) {
	if ln < 0 {
		return
	}
	_, body := m.getDetailDimensions()
	if ln < m.detailScrollOffset || ln >= m.detailScrollOffset+body {
		m.detailScrollOffset = max(ln-2, 0)
	}
	m.detailScrollOffset = min(m.detailScrollOffset, m.maxDetailScrollOffset())
}

// moveLinkCursor implements ]l / [l: focus the next/previous link of the current item.
// moveLinkCursor implements ]l / [l: focus the next/previous link of the current item.
func (m *Model) moveLinkCursor(delta, count int) {
	t := m.currentTask()
	if t == nil {
		return
	}
	w, _ := m.getDetailDimensions()
	doc := m.detailDoc(w)
	if len(doc.links) == 0 {
		m.statusMsg = "No links in " + t.ID
		return
	}
	cur := m.focusedLinkIndex()
	step := max(count, 1) * delta
	n := len(doc.links)
	next := cur + step
	if cur == 0 && delta < 0 {
		next = n + 1 + step
	}
	next = ((next-1)%n+n)%n + 1
	m.linkCursor = next
	m.linkCursorFor = t.ID
	m.activePane = PaneDetail
	m.scrollToLine(doc.links[next-1].Line)
	dl := doc.links[next-1]
	label := dl.Link.Display()
	if dl.Backlink {
		label = "← " + label
	}
	m.statusMsg = fmt.Sprintf("Link %d/%d: %s", next, n, label)
}

// followLink implements gf / enter / {n}gf. n = 0 follows the focused link.
func (m *Model) followLink(n int) tea.Cmd {
	t := m.currentTask()
	if t == nil {
		return nil
	}
	links := m.detailLinks(t)
	if len(links) == 0 {
		m.statusMsg = "No links in " + t.ID
		return nil
	}
	if n == 0 {
		n = m.focusedLinkIndex()
		if n == 0 {
			n = 1
		}
	}
	if n < 1 || n > len(links) {
		m.statusMsg = fmt.Sprintf("No link %d (%s has %d)", n, t.ID, len(links))
		return nil
	}
	return m.navigateTo(links[n-1].Link)
}

// navigateTo jumps to a link target, recording the current position in the history.
func (m *Model) navigateTo(l domain.WikiLink) tea.Cmd {
	m.pushHistory()
	if l.IsTopic {
		return m.selectTopic(l.Target)
	}
	var anchor *detailAnchor
	if l.LineStart > 0 || l.Note > 0 {
		anchor = &detailAnchor{TaskID: l.Target, LineStart: l.LineStart, LineEnd: max(l.LineEnd, l.LineStart), Note: l.Note}
	}
	target := &navTarget{ItemID: l.Target, Anchor: anchor}
	if m.selectItem(target) {
		m.statusMsg = "→ " + l.Display()
		return nil
	}

	// Not in the current list: switch to the item's scope/topic and select it after reloading.
	var cmds []tea.Cmd
	if s := scopeOfID(l.Target); s != "" && s != m.scope {
		m.setScope(s)
		cmds = append(cmds, m.fetchTopicsCmd())
	}
	dest := ""
	if it := m.lookupItem(l.Target); it != nil {
		dest = it.TopicPath
		if dest == "" {
			dest = domain.UnfiledTopic
		}
	}
	cmds = append(cmds, m.selectTopic(dest))
	m.pendingSelect = target
	m.statusMsg = "→ " + l.Display()
	return tea.Sequence(cmds...)
}

// scopeOfID infers the database of an item from its ID prefix (G-/GP- are global).
func scopeOfID(id string) string {
	prefix, _, ok := domain.ParseItemID(id)
	if !ok {
		return ""
	}
	if prefix == domain.PrefixGlobalTask || prefix == domain.PrefixGlobalPost {
		return domain.ScopeGlobal
	}
	return domain.ScopeLocal
}

// scrollToAnchor scrolls the detail pane to the active anchor (description line or note).
func (m *Model) scrollToAnchor() {
	a := m.anchor
	t := m.currentTask()
	if a == nil || t == nil || a.TaskID != t.ID {
		return
	}
	w, _ := m.getDetailDimensions()
	doc := m.detailDoc(w)
	if a.Note > 0 {
		if ln, ok := doc.noteLine[a.Note]; ok {
			m.scrollToLine(ln)
			m.detailScrollOffset = min(ln, m.maxDetailScrollOffset())
		}
		return
	}
	if ln, ok := doc.descLine[a.LineStart]; ok {
		m.detailScrollOffset = min(max(ln-1, 0), m.maxDetailScrollOffset())
	}
}

// anchorCovers reports whether the active anchor highlights description line n of t.
func (m Model) anchorCovers(t *domain.Task, n int) bool {
	a := m.anchor
	return a != nil && a.TaskID == t.ID && a.Note == 0 && n >= a.LineStart && n <= a.LineEnd
}

// sortedBacklinks returns backlink IDs in stable display order.
func sortedBacklinks(t *domain.Task) []string {
	out := append([]string(nil), t.Backlinks...)
	sort.Strings(out)
	return out
}

func (m *Model) applyRelated(msg relatedMsg) {
	if msg.scope != m.scope {
		return
	}
	if m.related == nil {
		m.related = make(map[string]*domain.Task)
	}
	for k, v := range msg.found {
		m.related[k] = v
	}
	if m.missing == nil {
		m.missing = make(map[string]bool)
	}
	for k, v := range msg.missing {
		m.missing[k] = v
	}
}

// detailDoc constructs the rendered lines and navigation metadata for the detail pane.
func (m Model) detailDoc(detailWidth int) detailDoc {
	t := m.currentTask()
	if t == nil {
		return detailDoc{}
	}
	theme := m.theme
	var doc detailDoc
	doc.descLine = make(map[int]int)
	doc.noteLine = make(map[int]int)

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
	doc.lines = append(doc.lines, theme.NoteTime.Width(detailWidth).Render(latestBadge), strings.Repeat(" ", detailWidth))

	// Task Title
	titlePrefix := t.ID + ": "
	if t.IsPost() {
		titlePrefix = t.ID + " [BOARD POST]: "
	}
	for _, tl := range wrapText(titlePrefix+t.Title, detailWidth-4, "  ") {
		doc.lines = append(doc.lines, theme.DetailTitle.Width(detailWidth).Render(tl))
	}

	// Metadata badges
	statusDisplay := t.StatusLabel()
	statusStyle := StatusStyle(t.Status)
	if t.IsOrphaned() {
		statusDisplay = fmt.Sprintf("%s 👻 (ORPHANED)", t.Status)
		statusStyle = theme.CountOrphaned
	}
	meta1 := fmt.Sprintf("  %s %s   %s %s   %s %s",
		theme.DetailLabel.Render("Status:"), statusStyle.Render(statusDisplay),
		theme.DetailLabel.Render("Pri:"), PriorityStyle(t.Priority).Render(string(t.Priority)),
		theme.DetailLabel.Render("Scope:"), theme.TaskID.Render(t.Scope),
	)
	var metaParts []string
	if t.IsPost() {
		metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Kind:"), theme.ModalKey.Render("board post")))
	} else if t.Kind != "" && t.Kind != domain.KindTask {
		metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Kind:"), theme.ModalKey.Render(string(t.Kind))))
	}
	topicStr := t.TopicPath
	if topicStr == "" {
		topicStr = domain.UnfiledTopic
	}
	metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Topic:"), theme.TaskID.Render("@"+topicStr)))
	if t.Assignee != "" {
		assigneeDisplay := t.Assignee
		if t.IsOrphaned() {
			assigneeDisplay += " [STALE / TIMED OUT]"
		}
		metaParts = append(metaParts, fmt.Sprintf("%s %s", theme.DetailLabel.Render("Assignee:"), theme.NoteHeader.Render(assigneeDisplay)))
	}
	fullMeta := meta1 + "   " + strings.Join(metaParts, "   ")
	if lipgloss.Width(fullMeta) <= detailWidth {
		doc.lines = append(doc.lines, fullMeta)
	} else {
		doc.lines = append(doc.lines, meta1)
		doc.lines = append(doc.lines, wrapText(strings.Join(metaParts, "   "), detailWidth-4, "  ")...)
	}
	doc.lines = append(doc.lines, strings.Repeat(" ", detailWidth))

	// Dependencies
	if t.IsBlocked {
		doc.lines = append(doc.lines, theme.DiffSub.Render("  🔒 BLOCKED: Must complete unfinished prerequisites first:"))
		for _, b := range t.BlockedBy {
			doc.lines = append(doc.lines, theme.DiffSub.Render(fmt.Sprintf("    • %s", b)))
		}
		doc.lines = append(doc.lines, strings.Repeat(" ", detailWidth))
	}
	if len(t.DependsOn) > 0 {
		depText := "Depends on: " + strings.Join(t.DependsOn, ", ")
		if t.IsBlocked {
			depText += " 🔒 [BLOCKED]"
		} else {
			depText += " ⚡ [READY]"
		}
		doc.lines = append(doc.lines, theme.DetailLabel.Render("  "+depText))
	}
	if len(t.Blocks) > 0 {
		blkText := "Blocks: " + strings.Join(t.Blocks, ", ")
		doc.lines = append(doc.lines, theme.DetailLabel.Render("  "+blkText))
	}
	if len(t.DependsOn) > 0 || len(t.Blocks) > 0 {
		doc.lines = append(doc.lines, strings.Repeat(" ", detailWidth))
	}

	linkLines := make(map[int]int)
	currLinkIdx := 1

	// Description with links
	if t.Description != "" {
		descLinks := domain.ParseWikiLinks(t.Description, m.scope)
		descLinesMap := make(map[int]int)
		startLine := len(doc.lines)
		baseStyle := func(n int) lipgloss.Style {
			if m.anchorCovers(t, n) {
				return theme.RowSelected
			}
			return theme.DetailDesc
		}
		renderedDesc := m.renderLinkedText(t.Description, descLinks, currLinkIdx, detailWidth-4, "  ", baseStyle, descLinesMap, linkLines)
		for srcLine, offset := range descLinesMap {
			doc.descLine[srcLine] = startLine + offset
		}
		for _, l := range descLinks {
			doc.links = append(doc.links, detailLink{Index: currLinkIdx, Link: l})
			currLinkIdx++
		}
		doc.lines = append(doc.lines, renderedDesc...)
	}

	// Commit resolution & diff
	if t.CommitSHA != "" {
		doc.lines = append(doc.lines, strings.Repeat(" ", detailWidth))
		resText := fmt.Sprintf("✓ Resolved by commit %s: %s", t.CommitSHA, t.CommitMsg)
		for _, rl := range wrapText(resText, detailWidth-4, "  ") {
			doc.lines = append(doc.lines, theme.DetailResolved.Width(detailWidth).Render(rl))
		}
		if t.DiffStat != "" {
			for _, sl := range strings.Split(strings.TrimSpace(t.DiffStat), "\n") {
				doc.lines = append(doc.lines, theme.DiffStat.Width(detailWidth).Render("    "+strings.TrimSpace(sl)))
			}
		}
		if m.showDiff && t.DiffContent != "" {
			doc.lines = append(doc.lines, theme.Border.Width(detailWidth).Render("  ── Diff Content ──"))
			diffLines := strings.Split(t.DiffContent, "\n")
			for _, dl := range diffLines {
				if strings.HasPrefix(dl, "+") {
					doc.lines = append(doc.lines, theme.DiffAdd.Width(detailWidth).Render("    "+dl))
				} else if strings.HasPrefix(dl, "-") {
					doc.lines = append(doc.lines, theme.DiffSub.Width(detailWidth).Render("    "+dl))
				} else if strings.HasPrefix(dl, "@@") {
					doc.lines = append(doc.lines, theme.DiffHunk.Width(detailWidth).Render("    "+dl))
				} else {
					doc.lines = append(doc.lines, theme.DetailDesc.Width(detailWidth).Render("    "+dl))
				}
			}
		}
	}

	// Activity Notes with links
	if len(t.Notes) > 0 {
		doc.lines = append(doc.lines, strings.Repeat(" ", detailWidth), theme.NoteHeader.Width(detailWidth).Render(fmt.Sprintf("  • Notes & Messages (%d):", len(t.Notes))))
		for i, n := range t.Notes {
			noteNum := i + 1
			doc.noteLine[noteNum] = len(doc.lines)
			timeStr := theme.NoteTime.Render(timeAgo(n.CreatedAt))
			authorStr := ""
			if n.Author != "" {
				availAuthor := detailWidth - 18
				author := truncateToWidth(n.Author, availAuthor)
				authorStr = " by " + theme.NoteAuthor.Render(author)
			}
			var anchorTag string
			if m.anchor != nil && m.anchor.TaskID == t.ID && m.anchor.Note == noteNum {
				anchorTag = " " + theme.RowSelected.Render(fmt.Sprintf("[#n%d]", noteNum))
			} else {
				anchorTag = " " + theme.DetailLabel.Render(fmt.Sprintf("#n%d", noteNum))
			}
			doc.lines = append(doc.lines, fmt.Sprintf("    • %s%s%s:", timeStr, authorStr, anchorTag))
			nLinks := domain.ParseWikiLinks(n.Content, m.scope)
			renderedNote := m.renderLinkedText(n.Content, nLinks, currLinkIdx, detailWidth-6, "      ", func(_ int) lipgloss.Style {
				return theme.NoteContent
			}, nil, linkLines)
			for _, l := range nLinks {
				doc.links = append(doc.links, detailLink{Index: currLinkIdx, Link: l})
				currLinkIdx++
			}
			doc.lines = append(doc.lines, renderedNote...)
		}
	}

	// Outgoing Links section
	if len(doc.links) > 0 {
		doc.lines = append(doc.lines, strings.Repeat(" ", detailWidth), theme.DetailLabel.Render(fmt.Sprintf("  ── Links (%d) ──", len(doc.links))))
		for _, dl := range doc.links {
			summary, st := m.linkSummary(dl)
			lineIdx := len(doc.lines)
			linkLines[dl.Index] = lineIdx
			stStyle := m.linkStyle(st, dl.Index == m.focusedLinkIndex())
			doc.lines = append(doc.lines, fmt.Sprintf("    %s %s: %s",
				superscript(dl.Index),
				stStyle.Render("[["+dl.Link.Display()+"]]"),
				theme.DetailDesc.Render(summary),
			))
		}
	}

	// Backlinks section
	backlinks := sortedBacklinks(t)
	if len(backlinks) > 0 {
		doc.lines = append(doc.lines, strings.Repeat(" ", detailWidth), theme.DetailLabel.Render(fmt.Sprintf("  ── Backlinks (%d) ──", len(backlinks))))
		for _, bid := range backlinks {
			idx := currLinkIdx
			currLinkIdx++
			dl := detailLink{Index: idx, Link: domain.WikiLink{Target: bid}, Backlink: true}
			lineIdx := len(doc.lines)
			linkLines[idx] = lineIdx
			doc.links = append(doc.links, dl)
			summary, st := m.linkSummary(dl)
			stStyle := m.linkStyle(st, idx == m.focusedLinkIndex())
			doc.lines = append(doc.lines, fmt.Sprintf("    %s %s: %s",
				superscript(idx),
				stStyle.Render("← [["+bid+"]]"),
				theme.DetailDesc.Render(summary),
			))
		}
	}

	// Assign Line to doc.links
	for i := range doc.links {
		if ln, ok := linkLines[doc.links[i].Index]; ok {
			doc.links[i].Line = ln
		} else {
			doc.links[i].Line = -1
		}
	}

	doc.lines = fitLines(doc.lines, detailWidth)
	return doc
}
