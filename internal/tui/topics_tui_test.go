package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"task_cli/internal/domain"
)

func TestTUISidebarTreeOrderAndSelection(t *testing.T) {
	pID1 := int64(1)
	topics := []*domain.Topic{
		{ID: 1, Slug: "backend", Title: "Backend Engineering", Kind: domain.TopicTasks, Path: "backend", OpenCount: 3},
		{ID: 2, Slug: "db", Title: "Database Layer", Kind: domain.TopicTasks, ParentID: &pID1, Path: "backend/db", OpenCount: 1},
		{ID: 3, Slug: "bugs", Title: "Bug Reports", Kind: domain.TopicBoard, Path: "bugs", OpenCount: 2},
	}
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Root task", Status: domain.StatusOpen, TopicPath: "backend"},
	}

	m := Model{
		topics:        topics,
		topicsLoaded:  true,
		tasks:         tasks,
		width:         140,
		height:        30,
		theme:         NewKanagawaTheme(),
		activePane:    PaneSidebar,
		sidebarCursor: 0,
	}

	entries := m.sidebarEntries()
	if len(entries) < 7 {
		t.Fatalf("expected at least 7 sidebar entries (All, All Tasks, All Posts, Unfiled, backend, backend/db, bugs), got %d", len(entries))
	}
	if entries[0].Label != "All" || entries[1].Label != "All Tasks" || entries[2].Label != "All Posts" || entries[3].Label != "Unfiled" {
		t.Fatalf("expected All, All Tasks, All Posts, Unfiled first, got %s, %s, %s, %s",
			entries[0].Label, entries[1].Label, entries[2].Label, entries[3].Label)
	}
	if entries[4].Path != "backend" || entries[5].Path != "backend/db" {
		t.Fatalf("expected tree order backend -> backend/db, got %s -> %s", entries[4].Path, entries[5].Path)
	}
	if entries[5].Depth != 1 {
		t.Fatalf("expected backend/db depth 1, got %d", entries[5].Depth)
	}

	// Press 'j' 4 times to navigate down to backend
	for i := 0; i < 4; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = mustModel(t, updated)
	}

	// Press Enter to select backend topic
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, updated)

	if m.topicPath != "backend" {
		t.Fatalf("expected topicPath 'backend' after selecting topic, got %q", m.topicPath)
	}
	if m.activePane != PaneList {
		t.Fatalf("expected activePane to switch to PaneList after selection, got %d", m.activePane)
	}
}

func TestTUIKindFilterCycleAndSelection(t *testing.T) {
	m := Model{
		tasks: []*domain.Task{
			{ID: "T-1", Title: "Task One", Kind: domain.KindTask},
			{ID: "P-1", Title: "Post One", Kind: domain.KindPost},
		},
		width:      140,
		height:     30,
		theme:      NewKanagawaTheme(),
		activePane: PaneList,
	}

	// Initial state: all kinds
	if m.kindFilter != "" {
		t.Fatalf("expected empty kindFilter initially, got %q", m.kindFilter)
	}

	// Press 'O' to cycle to Tasks Only
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	m = mustModel(t, updated)
	if m.kindFilter != domain.KindTask {
		t.Fatalf("expected kindFilter 'task' after 1st O, got %q", m.kindFilter)
	}
	if !strings.Contains(m.View(), "TASKS ONLY") {
		t.Fatalf("expected TASKS ONLY indicator in header view")
	}

	// Press 'O' to cycle to Posts Only
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	m = mustModel(t, updated)
	if m.kindFilter != domain.KindPost {
		t.Fatalf("expected kindFilter 'post' after 2nd O, got %q", m.kindFilter)
	}
	if !strings.Contains(m.View(), "POSTS ONLY") {
		t.Fatalf("expected POSTS ONLY indicator in header view")
	}

	// Press 'O' to cycle back to All
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	m = mustModel(t, updated)
	if m.kindFilter != "" {
		t.Fatalf("expected empty kindFilter after 3rd O, got %q", m.kindFilter)
	}

	// Test sidebar navigation to All Posts (entry index 2)
	m.activePane = PaneSidebar
	m.sidebarCursor = 0 // on "All"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mustModel(t, updated) // cursor on "All Tasks"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mustModel(t, updated) // cursor on "All Posts"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, updated)

	if m.kindFilter != domain.KindPost {
		t.Fatalf("expected kindFilter 'post' after selecting All Posts in sidebar, got %q", m.kindFilter)
	}
	if m.topicPath != "" {
		t.Fatalf("expected topicPath '' after selecting All Posts in sidebar, got %q", m.topicPath)
	}
}

func TestTUIPostRowAndDetailRendering(t *testing.T) {
	post := &domain.Task{
		ID:        "P-1",
		Title:     "Crash on startup",
		Kind:      domain.KindPost,
		Status:    domain.StatusOpen,
		TopicPath: "bugs",
	}

	m := Model{
		tasks:      []*domain.Task{post},
		width:      140,
		height:     30,
		theme:      NewKanagawaTheme(),
		activePane: PaneList,
		cursor:     0,
	}

	view := m.View()
	// Check row glyph
	if !strings.Contains(view, "▣") && !strings.Contains(view, "#") {
		t.Fatalf("expected post glyph ▣ or # in task row view:\n%s", view)
	}

	// Focus detail pane
	m.activePane = PaneDetail
	detView := m.View()
	if !strings.Contains(detView, "[BOARD POST]") {
		t.Fatalf("expected [BOARD POST] in detail pane title:\n%s", detView)
	}
	if !strings.Contains(detView, "Kind:") || !strings.Contains(detView, "board post") {
		t.Fatalf("expected Kind: board post badge in detail pane:\n%s", detView)
	}
	// Check footer labels for posts: [d] Resolve, [p] Promote, no [a] Claim
	if !strings.Contains(detView, "Resolve") {
		t.Fatalf("expected [d] Resolve in footer for posts:\n%s", detView)
	}
	if !strings.Contains(detView, "Promote") {
		t.Fatalf("expected [p] Promote in footer for posts:\n%s", detView)
	}
	if strings.Contains(detView, "Claim") {
		t.Fatalf("expected Claim to be omitted in footer for posts:\n%s", detView)
	}
}

func TestTUILinkCursorAndFollowHistory(t *testing.T) {
	task1 := &domain.Task{
		ID:          "T-1",
		Title:       "Task One",
		Status:      domain.StatusOpen,
		TopicPath:   "engine",
		Description: "Refers to [[T-2]] and [[T-3#L5]].",
		Notes: []domain.TaskNote{
			{Content: "Also check [[@bugs]] topic.", CreatedAt: time.Now()},
		},
	}
	task2 := &domain.Task{
		ID:        "T-2",
		Title:     "Task Two",
		Status:    domain.StatusOpen,
		TopicPath: "engine",
	}
	task3 := &domain.Task{
		ID:          "T-3",
		Title:       "Task Three",
		Status:      domain.StatusOpen,
		TopicPath:   "engine",
		Description: "Line 1\nLine 2\nLine 3\nLine 4\nLine 5: Target anchor\nLine 6",
	}

	m := Model{
		tasks:        []*domain.Task{task1, task2, task3},
		topicsLoaded: true,
		width:        120,
		height:       30,
		theme:        NewKanagawaTheme(),
		activePane:   PaneDetail,
		cursor:       0,
	}

	// Move link cursor forward with ']l'
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mustModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = mustModel(t, updated)

	if m.linkCursor != 1 {
		t.Fatalf("expected linkCursor 1 after ]l, got %d", m.linkCursor)
	}

	// Move link cursor forward again with ']l' to link 2 ([[T-3#L5]])
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mustModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = mustModel(t, updated)

	if m.linkCursor != 2 {
		t.Fatalf("expected linkCursor 2 after second ]l, got %d", m.linkCursor)
	}

	// Follow focused link with Enter or 'gf'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, updated)

	// Cursor should now be on T-3
	cur := m.currentTask()
	if cur == nil || cur.ID != "T-3" {
		t.Fatalf("expected currentTask T-3 after following link, got %v", cur)
	}
	if len(m.backStack) != 1 {
		t.Fatalf("expected 1 history entry in backStack, got %d", len(m.backStack))
	}
	if m.backStack[0].ItemID != "T-1" {
		t.Fatalf("expected previous history item to be T-1, got %s", m.backStack[0].ItemID)
	}

	// Go back with 'gb'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = mustModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = mustModel(t, updated)

	cur = m.currentTask()
	if cur == nil || cur.ID != "T-1" {
		t.Fatalf("expected currentTask T-1 after gb, got %v", cur)
	}
	if len(m.fwdStack) != 1 {
		t.Fatalf("expected 1 history entry in fwdStack after gb, got %d", len(m.fwdStack))
	}

	// Go forward with 'gB'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = mustModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	m = mustModel(t, updated)

	cur = m.currentTask()
	if cur == nil || cur.ID != "T-3" {
		t.Fatalf("expected currentTask T-3 after gB, got %v", cur)
	}
}

func TestTUIGraphTreePointersAndGhosts(t *testing.T) {
	// DAG with multi-parent child T-3 (blocked by both T-1 and T-2),
	// and cross-topic dependency G-99 (lives in @core, blocked by T-3)
	t1 := &domain.Task{ID: "T-1", Title: "Parent One", TopicPath: "app", Blocks: []string{"T-3"}}
	t2 := &domain.Task{ID: "T-2", Title: "Parent Two", TopicPath: "app", Blocks: []string{"T-3"}}
	t3 := &domain.Task{ID: "T-3", Title: "Shared Child", TopicPath: "app", DependsOn: []string{"T-1", "T-2"}, Blocks: []string{"G-99"}}
	ghost := &domain.Task{ID: "G-99", Title: "Core Engine", TopicPath: "core", DependsOn: []string{"T-3"}}

	m := Model{
		tasks:        []*domain.Task{t1, t2, t3},
		related:      map[string]*domain.Task{"G-99": ghost},
		topicPath:    "app",
		treeViewMode: true,
		width:        120,
		height:       30,
		theme:        NewKanagawaTheme(),
	}

	rows := m.getTreeRows()
	var hasPointer, hasGhost bool
	for _, r := range rows {
		if r.Pointer {
			hasPointer = true
		}
		if r.Ghost {
			hasGhost = true
		}
	}
	if !hasPointer {
		t.Fatalf("expected DAG tree to render at least one pointer row (↪) for shared child/parent")
	}
	if !hasGhost {
		t.Fatalf("expected DAG tree to render ghost row (⇢) for cross-topic dependency G-99")
	}

	// Verify rendered view includes glyphs
	rendered := m.View()
	if !strings.Contains(rendered, "↪") && !strings.Contains(rendered, "->") {
		t.Fatalf("expected pointer glyph in rendered view:\n%s", rendered)
	}
	if !strings.Contains(rendered, "⇢") && !strings.Contains(rendered, "~>") {
		t.Fatalf("expected ghost glyph in rendered view:\n%s", rendered)
	}
}

func TestTUINarrowLayoutFallback(t *testing.T) {
	tasks := layoutFixture(5)
	// Narrow screen width = 90 (below splitMinWidth = 110)
	m := Model{
		tasks:      tasks,
		width:      90,
		height:     24,
		theme:      NewKanagawaTheme(),
		activePane: PaneList,
	}

	l := m.layout()
	if l.split {
		t.Fatalf("expected split to be false on narrow screen (width 90)")
	}
	if l.sidebarWidth != 0 {
		t.Fatalf("expected sidebarWidth to be 0 on narrow stacked screen, got %d", l.sidebarWidth)
	}

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 24 {
		t.Fatalf("expected 24 lines, got %d", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 90 {
			t.Fatalf("line %d is %d cells wide (> 90): %q", i, w, line)
		}
	}
}

func TestTopicGroupRecursiveViewAndCreation(t *testing.T) {
	pID1 := int64(10)
	topics := []*domain.Topic{
		{ID: 10, Slug: "sim", Title: "Simulation Group", Kind: domain.TopicGroup, Path: "sim", ChildCount: 2, OpenCount: 3},
		{ID: 11, Slug: "nav", Title: "Navigation", Kind: domain.TopicTasks, ParentID: &pID1, Path: "sim/nav", OpenCount: 2},
		{ID: 12, Slug: "simulation", Title: "Sim Core", Kind: domain.TopicTasks, ParentID: &pID1, Path: "sim/simulation", OpenCount: 1},
		{ID: 20, Slug: "empty", Title: "Empty Group", Kind: domain.TopicGroup, Path: "empty", ChildCount: 0, OpenCount: 0},
	}
	tasks := []*domain.Task{
		{ID: "T-101", Title: "Nav Task", Status: domain.StatusOpen, TopicPath: "sim/nav", CreatedAt: time.Now()},
		{ID: "T-102", Title: "Physics Task", Status: domain.StatusOpen, TopicPath: "sim/simulation", CreatedAt: time.Now()},
	}

	m := Model{
		topics:       topics,
		topicsLoaded: true,
		tasks:        tasks,
		topicPath:    "sim",
		width:        160,
		height:       30,
		theme:        NewKanagawaTheme(),
		activePane:   PaneList,
	}

	// 1. Recursive topic filter
	if !m.isTopicRecursive() {
		t.Fatalf("expected isTopicRecursive to be true for TopicGroup 'sim'")
	}
	filter := m.listFilter()
	if !filter.TopicRecursive {
		t.Fatalf("expected listFilter().TopicRecursive to be true for TopicGroup 'sim'")
	}

	// 2. Table view includes TOPIC column and child topics
	view := m.View()
	if !strings.Contains(view, "TOPIC") {
		t.Fatalf("expected TOPIC column in view when viewing a group:\n%s", view)
	}
	if !strings.Contains(view, "@sim/nav") || !strings.Contains(view, "@sim/sim") {
		t.Fatalf("expected child topic tags in view:\n%s", view)
	}

	// 3. Empty group state message
	mEmpty := Model{
		topics:       topics,
		topicsLoaded: true,
		tasks:        nil,
		topicPath:    "empty",
		width:        120,
		height:       30,
		theme:        NewKanagawaTheme(),
		activePane:   PaneList,
	}
	emptyView := mEmpty.View()
	if !strings.Contains(emptyView, "Empty group. Press 'N' in sidebar to create a sub-topic") {
		t.Fatalf("expected empty group message in view, got:\n%s", emptyView)
	}

	// 4. Pressing 'n' in a group opens sub-topic picker
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	mPick := mustModel(t, updated)
	if mPick.picker == nil {
		t.Fatalf("expected picker to open when pressing 'n' in group with multiple children")
	}
	if mPick.picker.kind != pickNewItemTopic {
		t.Fatalf("expected picker kind pickNewItemTopic, got %v", mPick.picker.kind)
	}
	if !strings.Contains(mPick.picker.title, "@sim") {
		t.Fatalf("expected picker title to reference @sim, got %q", mPick.picker.title)
	}
	items := mPick.picker.items
	if len(items) != 2 {
		t.Fatalf("expected 2 child topics in picker, got %d", len(items))
	}
	if items[0].Value != "sim/nav" || items[1].Value != "sim/simulation" {
		t.Fatalf("unexpected picker items: %+v", items)
	}

	// 5. Pressing 'n' in empty group sets friendly status message
	updatedEmpty, _ := mEmpty.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	mEmptyAfter := mustModel(t, updatedEmpty)
	if !strings.Contains(mEmptyAfter.statusMsg, "has no writable sub-topics") {
		t.Fatalf("expected status message about no writable sub-topics, got %q", mEmptyAfter.statusMsg)
	}
}

func TestTUIActivePaneKeyGating(t *testing.T) {
	task1 := &domain.Task{ID: "T-1", Title: "Task One", Status: domain.StatusOpen}
	task2 := &domain.Task{ID: "T-2", Title: "Task Two", Status: domain.StatusOpen}
	m := Model{
		tasks:        []*domain.Task{task1, task2},
		selected:     map[string]bool{"T-1": true},
		activePane:   PaneSidebar,
		width:        140,
		height:       30,
		theme:        NewKanagawaTheme(),
		topicsLoaded: true,
	}

	// 1. Sidebar focus: list mutation keys should not execute batch or task mutations
	for _, key := range []rune{'d', 's', 'a', 'e', 'v', 'V', 'A', 'L', 'p'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		mUpdated := mustModel(t, updated)
		if mUpdated.confirmDialog != nil {
			t.Fatalf("key %q opened confirmation dialog while in PaneSidebar", string(key))
		}
		if task1.Status != domain.StatusOpen {
			t.Fatalf("key %q mutated task status in PaneSidebar", string(key))
		}
		if !strings.Contains(mUpdated.statusMsg, "Tab to Tasks") {
			t.Fatalf("expected guidance status message for key %q in PaneSidebar, got %q", string(key), mUpdated.statusMsg)
		}
	}

	// 'M' in sidebar triggers topic move, never batch item move
	updatedM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	mUpdatedM := mustModel(t, updatedM)
	if mUpdatedM.picker != nil && mUpdatedM.picker.kind == pickMoveItems {
		t.Fatalf("key 'M' triggered pickMoveItems while in PaneSidebar")
	}

	// 2. Detail focus: multi-select keys should be disabled
	m.activePane = PaneDetail
	m.selected = make(map[string]bool)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	mDetail := mustModel(t, updated)
	if len(mDetail.selected) > 0 {
		t.Fatalf("key 'v' toggled selection while in PaneDetail")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	mDetail = mustModel(t, updated)
	if len(mDetail.selected) > 0 {
		t.Fatalf("key 'V' selected all while in PaneDetail")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	mDetail = mustModel(t, updated)
	if len(mDetail.selected) > 0 {
		t.Fatalf("space toggled selection while in PaneDetail")
	}
}

func TestTUISidebarMouseClickAndScroll(t *testing.T) {
	topics := []*domain.Topic{
		{ID: 1, Title: "Backend", Slug: "backend", Kind: domain.TopicTasks},
		{ID: 2, Title: "Frontend", Slug: "frontend", Kind: domain.TopicTasks},
	}
	m := Model{
		topics:       topics,
		topicsLoaded: true,
		tasks: []*domain.Task{
			{ID: "T-1", Title: "Task One"},
		},
		width:      140,
		height:     30,
		theme:      NewKanagawaTheme(),
		activePane: PaneList,
	}

	l := m.layout()
	if l.sidebarWidth <= 0 {
		t.Fatalf("expected positive sidebarWidth, got %d", l.sidebarWidth)
	}

	// Test paneAt boundary geometry
	if p := m.paneAt(l, 5, 5); p != PaneSidebar {
		t.Fatalf("expected x=5 to be PaneSidebar, got %v", p)
	}
	if p := m.paneAt(l, l.sidebarWidth+5, 5); p != PaneList {
		t.Fatalf("expected x in list to be PaneList, got %v", p)
	}
	if p := m.paneAt(l, l.sidebarWidth+l.listWidth+10, 5); p != PaneDetail {
		t.Fatalf("expected x in detail to be PaneDetail, got %v", p)
	}

	// Left click inside sidebar at row 3 (first entry)
	mouseClick := tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      5,
		Y:      listFirstRowY,
	}
	updated, _ := m.Update(mouseClick)
	mClicked := mustModel(t, updated)
	if mClicked.activePane != PaneSidebar {
		t.Fatalf("expected activePane to be PaneSidebar after clicking sidebar, got %v", mClicked.activePane)
	}

	// Mouse wheel down inside sidebar
	wheelDown := tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		X:      5,
		Y:      5,
	}
	updated, _ = mClicked.Update(wheelDown)
	mScrolled := mustModel(t, updated)
	if mScrolled.sidebarCursor <= mClicked.sidebarCursor {
		t.Fatalf("expected sidebarCursor to advance on wheel down in sidebar")
	}
}

func TestTUIVimKMotionHalfPageUp(t *testing.T) {
	var tasks []*domain.Task
	for i := 0; i < 20; i++ {
		tasks = append(tasks, &domain.Task{ID: fmt.Sprintf("T-%d", i+1), Title: fmt.Sprintf("Task %d", i+1), Kind: domain.KindTask})
	}
	m := Model{
		tasks:      tasks,
		cursor:     15,
		width:      140,
		height:     20,
		theme:      NewKanagawaTheme(),
		activePane: PaneList,
	}

	// Press 'K' - should jump half page up (decrement cursor)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	mUp := mustModel(t, updated)
	if mUp.cursor >= 15 {
		t.Fatalf("expected cursor to move up on 'K', got %d", mUp.cursor)
	}
	if mUp.kindFilter != "" {
		t.Fatalf("expected kindFilter to remain unchanged on 'K', got %q", mUp.kindFilter)
	}

	// Press 'O' - should cycle kind filter
	updated, _ = mUp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	mCycled := mustModel(t, updated)
	if mCycled.kindFilter != domain.KindTask {
		t.Fatalf("expected kindFilter 'task' after pressing 'O', got %q", mCycled.kindFilter)
	}
}
