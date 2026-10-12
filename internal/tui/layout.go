package tui

// Layout model
//
// The TUI always renders exactly m.height lines so the terminal never scrolls and the
// header/legend can never be pushed off screen:
//
//	header (1) │ body (list pane + detail pane) │ separator (1) │ footer legend (1)
//
// Wide terminals place the panes side by side; narrow terminals stack them top/bottom.
// Every pane has a FIXED size derived only from the terminal size and the number of
// tasks — never from the selected task's content — so navigating never moves anything.
// The detail pane is a scrollable viewport in both modes (Tab / l to focus, j/k to scroll).

const (
	splitMinWidth = 110 // below this, panes are stacked vertically

	defaultWidth  = 80
	defaultHeight = 24

	chromeHeight = 3 // header + separator + footer

	listChromeHeight = 2 // column header + rule
	minListRows      = 3
	minDetailHeight  = 4 // title bar + 3 content lines

	// stackedListPercent is the share of the stacked body given to the task list
	// when there are more tasks than fit.
	stackedListPercent = 45

	splitDividerWidth = 1
	minSplitListWidth = 38
	minSplitDetail    = 25
)

// paneLayout describes the geometry of one frame. All heights are in terminal lines.
type paneLayout struct {
	split bool

	width, height int

	sidebarWidth int // 0 when hidden / stacked

	listWidth  int
	listHeight int // column header + rule + task rows + optional pagination line
	rows       int // visible task rows
	paginated  bool

	detailWidth  int
	detailHeight int // title bar + body
	detailBody   int // scrollable content lines
}

func (m Model) termSize() (width, height int) {
	width, height = m.width, m.height
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	return width, height
}

func (m Model) isSplitView() bool {
	w, _ := m.termSize()
	return w >= splitMinWidth
}

// splitListWidth returns the list pane width for side-by-side mode, honoring the user's drag offset.
func (m Model) splitListWidth(width int) int {
	basePercent := 48
	if m.treeViewMode {
		basePercent = 56
	}
	tableWidth := (width * basePercent / 100) + m.splitOffset
	if width < minSplitListWidth+minSplitDetail+splitDividerWidth {
		return width / 2
	}
	maxTableWidth := width - minSplitDetail - splitDividerWidth
	if tableWidth < minSplitListWidth {
		tableWidth = minSplitListWidth
	}
	if tableWidth > maxTableWidth {
		tableWidth = maxTableWidth
	}
	return tableWidth
}

// layout computes the geometry for the current terminal size and task count.
func (m Model) layout() paneLayout {
	width, height := m.termSize()
	l := paneLayout{split: width >= splitMinWidth, width: width, height: height}

	body := height - chromeHeight
	if body < listChromeHeight+minListRows+minDetailHeight {
		body = listChromeHeight + minListRows + minDetailHeight
	}
	taskCount := len(m.tasks)

	if l.split {
		if !m.sidebarHidden && width >= splitMinWidth+15 {
			l.sidebarWidth = min(max(width*18/100, 18), 26)
		}
		availWidth := width
		if l.sidebarWidth > 0 {
			availWidth -= l.sidebarWidth + splitDividerWidth
		}
		l.listWidth = m.splitListWidth(availWidth)
		l.detailWidth = availWidth - l.listWidth - splitDividerWidth
		l.listHeight = body
		l.detailHeight = body
	} else {
		l.listWidth = width
		l.detailWidth = width
		// The list takes a fixed share of the body, shrinking only when there are
		// few enough tasks to show them all; the detail pane gets the rest.
		listHeight := body * stackedListPercent / 100
		if needed := listChromeHeight + max(taskCount, 1); needed < listHeight {
			listHeight = needed
		}
		listHeight = max(listHeight, listChromeHeight+minListRows)
		listHeight = min(listHeight, body-minDetailHeight)
		l.listHeight = listHeight
		l.detailHeight = body - listHeight
	}

	l.rows = l.listHeight - listChromeHeight
	if taskCount > l.rows {
		l.paginated = true
		l.rows-- // reserve a line for the "Showing x-y of n" indicator
	}
	l.rows = max(l.rows, 1)
	l.detailBody = max(l.detailHeight-1, 1)
	return l
}

func (m Model) getMaxRows() int {
	return m.layout().rows
}

// getDetailDimensions returns the detail pane width and number of scrollable body lines.
func (m Model) getDetailDimensions() (detailWidth, visibleBody int) {
	l := m.layout()
	return l.detailWidth, l.detailBody
}

func (m Model) maxDetailScrollOffset() int {
	if len(m.tasks) == 0 || m.cursor >= len(m.tasks) {
		return 0
	}
	detailWidth, visibleBody := m.getDetailDimensions()
	contentLines := m.getDetailContentLines(detailWidth)
	return max(len(contentLines)-visibleBody, 0)
}

// clampScroll keeps list and detail scroll offsets valid after size or data changes.
func (m *Model) clampScroll() {
	rows := m.getMaxRows()
	if m.cursor >= len(m.tasks) {
		m.cursor = max(len(m.tasks)-1, 0)
	}
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+rows {
		m.scrollOffset = m.cursor - rows + 1
	}
	m.scrollOffset = min(m.scrollOffset, max(len(m.tasks)-rows, 0))
	m.scrollOffset = max(m.scrollOffset, 0)
	m.detailScrollOffset = min(m.detailScrollOffset, m.maxDetailScrollOffset())
	m.detailScrollOffset = max(m.detailScrollOffset, 0)
}
