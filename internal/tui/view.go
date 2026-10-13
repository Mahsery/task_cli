package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"task_cli/internal/domain"
)

// View renders exactly one terminal screen. See layout.go for the geometry contract.
func (m Model) View() string {
	l := m.layout()

	lines := make([]string, 0, l.height)
	lines = append(lines, m.renderHeader(l.width))

	listLines := m.renderListPane(l)
	detailLines := m.renderDetailPane(l)
	if l.split {
		div1 := m.theme.Border.Render("│")
		div2 := m.theme.Border.Render("│")
		switch m.activePane {
		case PaneSidebar:
			div1 = m.theme.ActiveBorder.Render("│")
		case PaneDetail:
			div2 = m.theme.ActiveBorder.Render("│")
		}
		var sidebarLines []string
		if l.sidebarWidth > 0 {
			sidebarLines = m.renderSidebar(l)
		}
		for i := range listLines {
			line := ""
			if l.sidebarWidth > 0 && i < len(sidebarLines) {
				line = sidebarLines[i] + div1
			}
			line += listLines[i] + div2 + detailLines[i]
			lines = append(lines, line)
		}
	} else {
		lines = append(lines, listLines...)
		lines = append(lines, detailLines...)
	}

	lines = append(lines, m.theme.Border.Render(strings.Repeat("─", l.width)), m.renderFooter(l.width))
	rawView := strings.Join(lines, "\n")

	if dlg := m.activeConfirmDialog(); dlg != nil {
		return m.overlayModal(rawView, dlg)
	}
	if m.isLinking {
		return m.overlayModalString(rawView, m.renderLinkModal(l.width, l.height))
	}
	if m.topicForm != nil {
		return m.overlayModalString(rawView, m.renderTopicForm(l.width))
	}
	if m.picker != nil {
		return m.overlayModalString(rawView, m.renderPicker(l.width, l.height))
	}
	return rawView
}

// fitLine truncates or pads an (ANSI-styled) single line to exactly width cells.
func fitLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if w := ansi.StringWidth(s); w > width {
		s = ansi.Truncate(s, width, "")
	}
	if w := ansi.StringWidth(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// fitLines flattens multi-line entries and fits every resulting line to exactly width cells.
func fitLines(entries []string, width int) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		for _, line := range strings.Split(e, "\n") {
			out = append(out, fitLine(line, width))
		}
	}
	return out
}

// fitBlock is fitLines forced to exactly height lines (truncated or blank-padded).
func fitBlock(entries []string, width, height int) []string {
	out := fitLines(entries, width)
	if len(out) > height {
		return out[:height]
	}
	blank := strings.Repeat(" ", max(width, 0))
	for len(out) < height {
		out = append(out, blank)
	}
	return out
}

type taskCounts struct {
	open, active, done, orphaned, archived int
}

func (m Model) countTasks() taskCounts {
	var c taskCounts
	for _, t := range m.tasks {
		if t.IsArchived() {
			c.archived++
			continue
		}
		isOrphan := t.IsOrphaned()
		if isOrphan {
			c.orphaned++
		}
		switch t.Status {
		case domain.StatusInProgress:
			if !isOrphan {
				c.active++
			}
		case domain.StatusDone, domain.StatusClosed:
			c.done++
		default:
			c.open++
		}
	}
	return c
}

// renderHeader renders the single-line title bar. On narrow terminals it degrades to
// compact counts rather than wrapping; an active search prompt always stays visible.
func (m Model) renderHeader(width int) string {
	theme := m.theme
	c := m.countTasks()

	appName := "task_cli"
	if m.scope == domain.ScopeLocal && m.repoPath != "" {
		appName = filepath.Base(m.repoPath)
	} else if m.scope == domain.ScopeGlobal {
		appName = "global"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(WaveAqua2)).Render("tk: " + appName)

	scopeBadge := theme.ScopeLocal.Render("[local]")
	if m.scope == domain.ScopeGlobal {
		scopeBadge = theme.ScopeGlobal.Render("[global]")
	}

	topicBadge := theme.ActiveHeader.Render("[" + topicLabel(m.topicPath) + "]")

	orphanStyle := theme.CountOrphanedZero
	if c.orphaned > 0 {
		orphanStyle = theme.CountOrphaned
	}
	showArchived := m.showArchived || c.archived > 0

	fullCounts := []string{
		theme.CountOpen.Render(fmt.Sprintf("%d Open", c.open)),
		theme.CountProg.Render(fmt.Sprintf("%d Active", c.active)),
		theme.CountDone.Render(fmt.Sprintf("%d Done", c.done)),
		orphanStyle.Render(fmt.Sprintf("%d Orphaned", c.orphaned)),
	}
	compactCounts := []string{
		theme.CountOpen.Render(fmt.Sprintf("%do", c.open)),
		theme.CountProg.Render(fmt.Sprintf("%da", c.active)),
		theme.CountDone.Render(fmt.Sprintf("%dd", c.done)),
		orphanStyle.Render(fmt.Sprintf("%d👻", c.orphaned)),
	}
	if showArchived {
		fullCounts = append(fullCounts, theme.CountArchived.Render(fmt.Sprintf("%d Archived", c.archived)))
		compactCounts = append(compactCounts, theme.CountArchived.Render(fmt.Sprintf("%darc", c.archived)))
	}

	var fullModes, compactModes []string
	if m.treeViewMode && !m.isBoardView() {
		fullModes = append(fullModes, theme.ModalTitle.Render("[TREE MODE (t)]"))
		compactModes = append(compactModes, theme.ModalTitle.Render("[TREE MODE]"))
	}
	if m.kindFilter == domain.KindTask {
		fullModes = append(fullModes, theme.CountProg.Render("[TASKS ONLY (O)]"))
		compactModes = append(compactModes, theme.CountProg.Render("[TASKS]"))
	} else if m.kindFilter == domain.KindPost {
		fullModes = append(fullModes, theme.ModalKey.Render("[POSTS ONLY (O)]"))
		compactModes = append(compactModes, theme.ModalKey.Render("[POSTS]"))
	} else if m.isBoardView() {
		fullModes = append(fullModes, theme.ModalKey.Render("[BOARD]"))
		compactModes = append(compactModes, theme.ModalKey.Render("[BOARD]"))
	}
	if m.showArchived {
		fullModes = append(fullModes, theme.ArchiveBadge.Render("[ARCHIVE VIEW]"))
		compactModes = append(compactModes, theme.ArchiveBadge.Render("[arc]"))
	}

	var fullQuery, compactQuery string
	if m.isSearch {
		searchInput := m.inputViewOrText(m.filterText)
		fullQuery = theme.SearchPrompt.Render(fmt.Sprintf("[Search: %s] (Enter apply, Esc cancel)", searchInput))
		compactQuery = theme.SearchPrompt.Render(fmt.Sprintf("/%s", searchInput))
	} else if m.filterText != "" {
		fullQuery = theme.FilterIndicator.Render(fmt.Sprintf("[Filter: %q (Esc to clear)]", m.filterText))
		compactQuery = theme.FilterIndicator.Render(fmt.Sprintf("[%q]", m.filterText))
	}

	sep := "  ─  "
	full := title + " " + scopeBadge + " " + topicBadge
	for _, md := range fullModes {
		full += sep + md
	}
	full += sep + strings.Join(fullCounts, " │ ")
	if fullQuery != "" {
		full += sep + fullQuery
	}

	inner := width - theme.Header.GetHorizontalFrameSize()
	text := full
	if ansi.StringWidth(full) > inner {
		// Compact form: the query comes right after the title so typed input is never cut off.
		text = title + " " + topicBadge
		if compactQuery != "" {
			text += " " + compactQuery
		}
		for _, md := range compactModes {
			text += " " + md
		}
		text += "  " + strings.Join(compactCounts, " ")
	}
	return theme.Header.Width(width).Render(fitLine(text, inner))
}

// renderListPane renders the task table: exactly l.listHeight lines of l.listWidth cells.
func (m Model) renderListPane(l paneLayout) []string {
	theme := m.theme
	width := l.listWidth

	idWidth, statusWidth, priWidth, ageWidth := 6, 11, 6, 10
	if width < 60 {
		ageWidth = 7
	}
	topicWidth := 0
	topicThreshold := 60
	if m.treeActive() {
		topicThreshold = 75
	}
	if m.topicPath == "" || m.topicPath == domain.UnfiledTopic || m.isTopicRecursive() {
		switch {
		case width >= 105:
			topicWidth = 16
		case width >= topicThreshold:
			topicWidth = 10
		}
	}
	fixed := 2 + idWidth + 1 + statusWidth + 1 + priWidth + 1 + 1 + ageWidth
	if topicWidth > 0 {
		fixed += topicWidth + 1
	}
	titleWidth := max(width-fixed, 12)

	colTitle := "TITLE"
	if m.treeActive() {
		colTitle = "TREE / TITLE"
	}
	cols := []string{padRight("ID", idWidth), padRight("STATUS", statusWidth), padRight("PRI", priWidth), padRight(colTitle, titleWidth)}
	if topicWidth > 0 {
		cols = append(cols, padRight("TOPIC", topicWidth))
	}
	cols = append(cols, padRight("AGE", ageWidth))
	colHeader := "  " + strings.Join(cols, " ")

	headerStyle := theme.ColHeader
	if m.activePane == PaneList {
		headerStyle = theme.ActiveHeader
	}
	lines := []string{
		headerStyle.Render(fitLine(colHeader, width)),
		theme.Border.Render(strings.Repeat("─", width)),
	}

	rows := m.displayRows()
	if len(rows) == 0 {
		emptyMsg := "  No items found. Press 'n' to create."
		if t := m.currentTopic(); t != nil && t.Kind == domain.TopicGroup && t.ChildCount == 0 {
			emptyMsg = "  Empty group. Press 'N' in sidebar to create a sub-topic."
		}
		lines = append(lines, theme.RowNormal.Render(emptyMsg))
		return fitBlock(lines, width, l.listHeight)
	}

	endIdx := min(m.scrollOffset+l.rows, len(rows))
	for i := m.scrollOffset; i < endIdx; i++ {
		isCursor := i == m.cursor
		lines = append(lines, m.renderTaskRow(rows[i], isCursor, width, idWidth, statusWidth, priWidth, titleWidth, topicWidth, ageWidth))
	}

	if l.paginated {
		pagination := fmt.Sprintf("  ── Showing %d-%d of %d (▲/▼) ──", m.scrollOffset+1, endIdx, len(rows))
		// Pin the indicator to the last line of the pane so it never moves.
		lines = fitBlock(lines, width, l.listHeight-1)
		lines = append(lines, theme.FooterDesc.Render(fitLine(pagination, width)))
		return lines
	}
	return fitBlock(lines, width, l.listHeight)
}

func (m Model) renderTaskRow(row TreeRow, isCursor bool, width, idWidth, statusWidth, priWidth, titleWidth, topicWidth, ageWidth int) string {
	theme := m.theme
	t := row.Task
	prefix := "  "
	if len(m.selected) > 0 {
		isSel := m.selected[t.ID]
		cursorMark := " "
		if isCursor {
			cursorMark = theme.Cursor.Render("►")
		}
		selMark := theme.UnselectedMark.Render("○")
		if isSel {
			selMark = theme.SelectedMark.Render("●")
		}
		prefix = cursorMark + selMark
	} else if isCursor {
		prefix = theme.Cursor.Render("► ")
	}

	pointerGlyph := "↪ "
	ghostGlyph := "⇢ "
	postGlyph := "▣ "
	if isASCIIOnly() {
		pointerGlyph = "-> "
		ghostGlyph = "~> "
		postGlyph = "# "
	}

	treeBranch := row.Prefix
	if treeBranch != "" {
		maxBranch := titleWidth - 10
		if maxBranch < 6 {
			maxBranch = titleWidth / 2
		}
		if maxBranch > 0 && runewidth.StringWidth(treeBranch) > maxBranch {
			treeBranch = compactTreeBranch(treeBranch, maxBranch)
		}
	}

	rawTitle := treeBranch + t.Title
	if row.Pointer {
		rawTitle = treeBranch + pointerGlyph + t.ID + " (shown above)"
	} else if row.Ghost {
		ghostTopic := ""
		if t.TopicPath != "" {
			ghostTopic = " @" + t.TopicPath
		}
		rawTitle = treeBranch + ghostGlyph + t.Title + ghostTopic
	} else {
		if t.IsPost() {
			rawTitle = treeBranch + postGlyph + t.Title
		}
		if t.IsBlocked {
			rawTitle += " 🔒"
		} else if t.Status != domain.StatusDone && t.Status != domain.StatusClosed && len(t.DependsOn) > 0 {
			rawTitle += " ⚡"
		}
		if t.IsOrphaned() {
			rawTitle += " 👻"
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
	statusText := t.StatusLabel()
	statusStyle := StatusStyle(t.Status)
	if t.IsOrphaned() {
		statusText = "orphan"
		statusStyle = theme.CountOrphaned
	}

	idStyle := theme.TaskID
	titleStyle := theme.TaskTitle
	if t.IsPost() {
		idStyle = theme.ModalKey
	}
	if row.Ghost {
		idStyle = theme.Age
		titleStyle = theme.Age
		statusStyle = theme.Age
	}

	cells := []string{
		idStyle.Render(padRight(truncateToWidth(t.ID, idWidth), idWidth)),
		statusStyle.Render(padRight(truncateToWidth(statusText, statusWidth), statusWidth)),
		PriorityStyle(t.Priority).Render(padRight(truncateToWidth(string(t.Priority), priWidth), priWidth)),
		titleStyle.Render(padRight(truncateToWidth(rawTitle, titleWidth), titleWidth)),
	}
	if topicWidth > 0 {
		topStr := t.TopicPath
		if topStr == "" {
			topStr = domain.UnfiledTopic
		}
		cells = append(cells, theme.Tag.Render(padRight(truncateToWidth("@"+topStr, topicWidth), topicWidth)))
	}
	cells = append(cells, theme.Age.Render(padRight(truncateToWidth(age, ageWidth), ageWidth)))

	line := fitLine(prefix+strings.Join(cells, " "), width)
	if isCursor {
		return theme.RowSelected.Render(line)
	}
	return theme.RowNormal.Render(line)
}

// renderDetailPane renders the selected task as a scrollable viewport with a pinned title bar:
// exactly l.detailHeight lines of l.detailWidth cells.
func (m Model) renderDetailPane(l paneLayout) []string {
	theme := m.theme
	width := l.detailWidth

	titleStyle := theme.InactiveHeader
	if m.activePane == PaneDetail {
		titleStyle = theme.ActiveHeader
	}

	if m.currentTask() == nil {
		return fitBlock([]string{
			titleStyle.Render(fitLine(" ── Task Details & Messages ──", width)),
			theme.RowNormal.Render("  No task selected."),
		}, width, l.detailHeight)
	}

	contentLines := m.getDetailContentLines(width)
	total := len(contentLines)
	start := min(max(m.detailScrollOffset, 0), max(total-l.detailBody, 0))
	end := min(start+l.detailBody, total)

	// Most important information first; on narrow panes the tail (line range) is truncated.
	var hint string
	switch {
	case m.activePane == PaneDetail && total > l.detailBody:
		hint = fmt.Sprintf("[Active • j/k scroll • Tab] %d-%d/%d", start+1, end, total)
	case m.activePane == PaneDetail:
		hint = "[Active • Tab to return to list]"
	case total > l.detailBody:
		hint = fmt.Sprintf("[Tab/l to focus & scroll] %d-%d/%d", start+1, end, total)
	default:
		hint = "[Tab/l to focus]"
	}
	panelTitle := " ── Details & Messages " + hint

	lines := make([]string, 0, l.detailHeight)
	lines = append(lines, titleStyle.Render(fitLine(panelTitle, width)))
	lines = append(lines, contentLines[start:end]...)
	return fitBlock(lines, width, l.detailHeight)
}
