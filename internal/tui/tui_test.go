package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"task_cli/internal/domain"
)

func TestTUIViewResponsiveDimensions(t *testing.T) {
	testTasks := []*domain.Task{
		{
			ID:          "T-1",
			Scope:       "local",
			Title:       "Add mandatory tk workflow and grx quickstart to AGENTS.md with extended instructions",
			Description: "Scope: update only AGENTS.md with mandatory task tracking, a concise tk tutorial, and grx guidance for complex searches. Acceptance: commands verified against installed CLIs; preserve existing user instructions.",
			Status:      domain.StatusInProgress,
			Priority:    domain.PriorityHigh,
			Tags:        []string{"docs", "workflow", "quickstart", "kernel"},
			Author:      "agent-instructions-20260924",
			Assignee:    "agent-instructions-20260924",
			CreatedAt:   time.Now().Add(-5 * time.Minute),
			Notes: []domain.TaskNote{
				{
					Author:    "agent-instructions-20260924",
					Content:   "Claimed task as 'agent-instructions-20260924' and verified background socket listener is ready.",
					CreatedAt: time.Now().Add(-4 * time.Minute),
				},
				{
					Author:    "agent-instructions-20260924",
					Content:   "Added mandatory tk workflow and quickstart at the top of AGENTS.md, plus grx proximity, Boolean, and filename examples.",
					CreatedAt: time.Now().Add(-2 * time.Minute),
				},
			},
			CommitSHA: "f0d6b9e",
			CommitMsg: "docs: require tk task coordination and add search quickstart",
			DiffStat:  "1 file changed, 43 insertions(+)",
			DiffContent: `--- a/AGENTS.md
+++ b/AGENTS.md
@@ -1,3 +1,5 @@
+## Mandatory Task Coordination
+Use tk for managing tasks.`,
		},
		{
			ID:        "T-2",
			Scope:     "local",
			Title:     "Short task",
			Status:    domain.StatusOpen,
			Priority:  domain.PriorityNormal,
			CreatedAt: time.Now().Add(-30 * time.Minute),
		},
		{
			ID:        "T-3",
			Scope:     "local",
			Title:     "Completed task with commit",
			Status:    domain.StatusDone,
			Priority:  domain.PriorityLow,
			Tags:      []string{"cleanup"},
			CommitSHA: "a1b2c3d",
			CommitMsg: "refactor: clean up unused structs",
			CreatedAt: time.Now().Add(-2 * time.Hour),
		},
	}

	widths := []int{55, 65, 80, 95, 120, 160}
	heights := []int{20, 24, 30, 45}

	for _, w := range widths {
		for _, h := range heights {
			m := Model{
				tasks:    testTasks,
				cursor:   0,
				width:    w,
				height:   h,
				theme:    NewKanagawaTheme(),
				scope:    "local",
				repoPath: "/test/repo",
			}

			output := m.View()
			if output == "" {
				t.Fatalf("View() returned empty string for %dx%d", w, h)
			}

			lines := strings.Split(output, "\n")
			for i, line := range lines {
				// Strip trailing newline effects and evaluate visual width
				lineLen := lipgloss.Width(line)
				if lineLen > w {
					t.Errorf("Width %d x Height %d: Line %d exceeds width (actual: %d): %q", w, h, i, lineLen, line)
				}
			}
		}
	}
}

func TestTUIConfirmationView(t *testing.T) {
	m := Model{
		tasks: []*domain.Task{
			{ID: "T-1", Title: "Task to delete", Status: domain.StatusOpen, Priority: domain.PriorityNormal},
		},
		cursor:        0,
		width:         80,
		height:        24,
		theme:         NewKanagawaTheme(),
		confirmDelete: true,
		deleteTaskID:  "T-1",
	}

	out := m.View()
	if !strings.Contains(out, "Delete task T-1 permanently?") && !strings.Contains(out, "Delete T-1? [y/N]") {
		t.Errorf("Expected confirmation prompt in View(), got:\n%s", out)
	}

	// Narrow window
	m.width = 40
	outNarrow := m.View()
	if !strings.Contains(outNarrow, "Delete T-1? [y/N]") {
		t.Errorf("Expected compact confirmation prompt in narrow View(), got:\n%s", outNarrow)
	}
}

func TestTUISplitView(t *testing.T) {
	task := &domain.Task{
		ID:        "T-1",
		Title:     "Feature split view",
		Status:    domain.StatusInProgress,
		Priority:  domain.PriorityHigh,
		Assignee:  "mahsery",
		CreatedAt: time.Now().Add(-10 * time.Minute),
		UpdatedAt: time.Now().Add(-2 * time.Minute),
		Notes: []domain.TaskNote{
			{Author: "mahsery", Content: "Working on dual-pane layout", CreatedAt: time.Now().Add(-2 * time.Minute)},
		},
	}

	// 1. Wide terminal (>= 110): split view with vertical divider and details pane
	mWide := Model{
		tasks:    []*domain.Task{task},
		cursor:   0,
		width:    120,
		height:   30,
		theme:    NewKanagawaTheme(),
		scope:    "local",
		repoPath: "/test/repo",
	}
	if !mWide.isSplitView() {
		t.Errorf("Expected isSplitView() to be true for width 120")
	}
	outWide := mWide.View()
	if !strings.Contains(outWide, "Details & Messages") {
		t.Errorf("Expected split view header in wide view")
	}
	if !strings.Contains(outWide, "│") {
		t.Errorf("Expected vertical divider '│' in split view")
	}
	if !strings.Contains(outWide, "Working on dual-pane layout") {
		t.Errorf("Expected note content in split view right pane")
	}

	// 2. Narrow terminal (< 110): stacked view
	mNarrow := Model{
		tasks:    []*domain.Task{task},
		cursor:   0,
		width:    80,
		height:   24,
		theme:    NewKanagawaTheme(),
		scope:    "local",
		repoPath: "/test/repo",
	}
	if mNarrow.isSplitView() {
		t.Errorf("Expected isSplitView() to be false for width 80")
	}
	outNarrow := mNarrow.View()
	if strings.Contains(outNarrow, "Task Details & Latest Activity") {
		t.Errorf("Expected stacked view without split view header in width 80")
	}
}

func TestTUIMultiSelectionToggle(t *testing.T) {
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Task One", Status: domain.StatusOpen},
		{ID: "T-2", Title: "Task Two", Status: domain.StatusInProgress},
		{ID: "T-3", Title: "Task Three", Status: domain.StatusDone},
	}

	m := Model{
		tasks:    tasks,
		cursor:   0,
		selected: make(map[string]bool),
		width:    80,
		height:   24,
		theme:    NewKanagawaTheme(),
	}

	// 1. Space on T-1 toggles T-1 selected and moves cursor to index 1 (T-2)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if !m.selected["T-1"] {
		t.Errorf("Expected T-1 to be selected after space")
	}
	if m.cursor != 1 {
		t.Errorf("Expected cursor to advance to 1, got %d", m.cursor)
	}

	// 2. 'v' on T-2 toggles T-2 selected without moving cursor
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)
	if !m.selected["T-2"] {
		t.Errorf("Expected T-2 to be selected after 'v'")
	}
	if m.cursor != 1 {
		t.Errorf("Expected cursor to remain at 1, got %d", m.cursor)
	}

	// 3. 'V' (Select all): since not all are selected, should select all 3
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	m = updated.(Model)
	if len(m.selected) != 3 {
		t.Errorf("Expected all 3 tasks to be selected after 'V', got %d", len(m.selected))
	}

	// 4. 'V' again: since all are selected, should deselect all
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	m = updated.(Model)
	if len(m.selected) != 0 {
		t.Errorf("Expected 0 tasks selected after second 'V', got %d", len(m.selected))
	}

	// 5. Select T-1, then press Esc to clear selection
	m.selected["T-1"] = true
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if len(m.selected) != 0 {
		t.Errorf("Expected selection to be cleared on Esc, got %d", len(m.selected))
	}
}

func TestTUIMultiSelectionVisualMarkers(t *testing.T) {
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Task One", Status: domain.StatusOpen, Priority: domain.PriorityNormal},
		{ID: "T-2", Title: "Task Two", Status: domain.StatusInProgress, Priority: domain.PriorityHigh},
	}

	// Stacked view
	m := Model{
		tasks:    tasks,
		cursor:   0,
		selected: map[string]bool{"T-1": true},
		width:    80,
		height:   24,
		theme:    NewKanagawaTheme(),
	}

	out := m.View()
	// T-1 is cursor and selected -> should contain "►" and "●"
	if !strings.Contains(out, "►") || !strings.Contains(out, "●") {
		t.Errorf("Expected selection marker ● and cursor ► in view, got:\n%s", out)
	}
	// T-2 is non-cursor and unselected -> should contain unselected marker "○"
	if !strings.Contains(out, "○") {
		t.Errorf("Expected unselected marker ○ in view when selection active, got:\n%s", out)
	}

	// Verify column alignment invariant: no lines exceed width
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) > 80 {
			t.Errorf("Line %d exceeds width 80: %q", i, l)
		}
	}

	// Split view
	mSplit := Model{
		tasks:    tasks,
		cursor:   1, // T-2 is cursor (unselected), T-1 is non-cursor (selected)
		selected: map[string]bool{"T-1": true},
		width:    120,
		height:   30,
		theme:    NewKanagawaTheme(),
	}
	outSplit := mSplit.View()
	if !strings.Contains(outSplit, "●") || !strings.Contains(outSplit, "○") {
		t.Errorf("Expected ● and ○ in split view, got:\n%s", outSplit)
	}
	// Footer badge
	if !strings.Contains(outSplit, "1 selected") {
		t.Errorf("Expected '1 selected' badge in footer, got:\n%s", outSplit)
	}
}

func TestTUIBatchConfirmationView(t *testing.T) {
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Task One"},
		{ID: "T-2", Title: "Task Two"},
	}

	m := Model{
		tasks:         tasks,
		selected:      map[string]bool{"T-1": true, "T-2": true},
		confirmDelete: true,
		deleteTaskID:  "", // batch delete
		width:         80,
		height:        24,
		theme:         NewKanagawaTheme(),
	}

	out := m.View()
	if !strings.Contains(out, "Delete 2 selected tasks permanently?") && !strings.Contains(out, "Delete 2 tasks? [y/N]") {
		t.Errorf("Expected batch deletion confirmation prompt, got:\n%s", out)
	}
}

func TestTUIDualPaneNavigation(t *testing.T) {
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Task One", Description: "Description of Task One"},
		{ID: "T-2", Title: "Task Two", Description: "Description of Task Two"},
	}

	m := Model{
		tasks:      tasks,
		activePane: PaneList,
		cursor:     0,
		width:      120,
		height:     30,
		theme:      NewKanagawaTheme(),
	}

	// 1. Initial state: PaneList
	if m.activePane != PaneList {
		t.Errorf("Expected initial activePane to be PaneList, got %v", m.activePane)
	}

	// 2. Tab toggles to PaneDetail
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.activePane != PaneDetail {
		t.Errorf("Expected activePane to be PaneDetail after Tab, got %v", m.activePane)
	}
	if !strings.Contains(m.statusMsg, "Details pane") {
		t.Errorf("Expected statusMsg to mention Details pane, got %q", m.statusMsg)
	}

	// 3. Tab toggles back to PaneList
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.activePane != PaneList {
		t.Errorf("Expected activePane to be PaneList after second Tab, got %v", m.activePane)
	}

	// 4. 'l' switches to PaneDetail
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	if m.activePane != PaneDetail {
		t.Errorf("Expected activePane to be PaneDetail after 'l', got %v", m.activePane)
	}

	// 5. 'h' switches back to PaneList
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(Model)
	if m.activePane != PaneList {
		t.Errorf("Expected activePane to be PaneList after 'h', got %v", m.activePane)
	}

	// 6. Right arrow switches to PaneDetail, Left arrow switches to PaneList
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.activePane != PaneDetail {
		t.Errorf("Expected activePane to be PaneDetail after Right arrow, got %v", m.activePane)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if m.activePane != PaneList {
		t.Errorf("Expected activePane to be PaneList after Left arrow, got %v", m.activePane)
	}

	// 7. Focus PaneDetail and hit Esc -> returns to PaneList
	m.activePane = PaneDetail
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.activePane != PaneList {
		t.Errorf("Expected Esc in PaneDetail to return focus to PaneList, got %v", m.activePane)
	}
}

func TestTUIIndependentContentScrolling(t *testing.T) {
	// Create a task with lots of notes to ensure scrolling has plenty of content lines
	notes := make([]domain.TaskNote, 25)
	for i := range notes {
		notes[i] = domain.TaskNote{
			Author:    "tester",
			Content:   fmt.Sprintf("Progress update note line %d with extra descriptive text", i+1),
			CreatedAt: time.Now().Add(time.Duration(-i) * time.Hour),
		}
	}

	tasks := []*domain.Task{
		{
			ID:          "T-1",
			Title:       "Task One",
			Description: "First task description",
			Notes:       notes,
		},
		{
			ID:          "T-2",
			Title:       "Task Two",
			Description: "Second task description",
		},
	}

	m := Model{
		tasks:              tasks,
		activePane:         PaneList,
		cursor:             0,
		detailScrollOffset: 0,
		width:              120,
		height:             20,
		theme:              NewKanagawaTheme(),
	}

	// When activePane == PaneList:
	// 'j' moves cursor to 1, detailScrollOffset stays 0
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Errorf("Expected cursor to advance to 1 in PaneList, got %d", m.cursor)
	}
	if m.detailScrollOffset != 0 {
		t.Errorf("Expected detailScrollOffset to remain 0 in PaneList, got %d", m.detailScrollOffset)
	}

	// 'k' moves cursor back to 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if m.cursor != 0 {
		t.Errorf("Expected cursor to return to 0, got %d", m.cursor)
	}

	// Now switch to PaneDetail!
	m.activePane = PaneDetail

	// In PaneDetail, 'j' scrolls content (detailScrollOffset++), cursor stays 0!
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.cursor != 0 {
		t.Errorf("Expected cursor to remain locked at 0 in PaneDetail, got %d", m.cursor)
	}
	if m.detailScrollOffset != 1 {
		t.Errorf("Expected detailScrollOffset to increment to 1, got %d", m.detailScrollOffset)
	}

	// 'j' again
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.detailScrollOffset != 2 {
		t.Errorf("Expected detailScrollOffset to be 2, got %d", m.detailScrollOffset)
	}

	// 'k' scrolls up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if m.detailScrollOffset != 1 {
		t.Errorf("Expected detailScrollOffset to decrement to 1, got %d", m.detailScrollOffset)
	}
	if m.cursor != 0 {
		t.Errorf("Expected cursor to remain locked at 0, got %d", m.cursor)
	}

	// Fast scrolling: J adds 6
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	m = updated.(Model)
	if m.detailScrollOffset != 7 {
		t.Errorf("Expected detailScrollOffset to be 7 after 'J', got %d", m.detailScrollOffset)
	}

	// 'g' jumps to top (0)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(Model)
	if m.detailScrollOffset != 0 {
		t.Errorf("Expected detailScrollOffset to reset to 0 after 'g', got %d", m.detailScrollOffset)
	}

	// Test rendered output in PaneDetail:
	viewOut := m.View()
	if !strings.Contains(viewOut, "DETAILS PANE") {
		t.Errorf("Expected footer to display DETAILS PANE when activePane is PaneDetail, got:\n%s", viewOut)
	}
	if !strings.Contains(viewOut, "[Active • j/k scroll") {
		t.Errorf("Expected panel header to show active scroll indicator, got:\n%s", viewOut)
	}

	// Verify header pinning: even when scrolled down, panel header is still rendered
	m.detailScrollOffset = 5
	viewScrolled := m.View()
	if !strings.Contains(viewScrolled, "Details & Messages") {
		t.Errorf("Expected pinned header line to remain visible when scrolled, got:\n%s", viewScrolled)
	}
}

func TestTUIScrollBoundsInBothPanes(t *testing.T) {
	// Create tasks
	notes := make([]domain.TaskNote, 30)
	for i := range notes {
		notes[i] = domain.TaskNote{
			Author:    "tester",
			Content:   fmt.Sprintf("Note %d with verbose text to guarantee scrolling", i+1),
			CreatedAt: time.Now().Add(time.Duration(-i) * time.Hour),
		}
	}
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Task 1", Notes: notes},
		{ID: "T-2", Title: "Task 2"},
		{ID: "T-3", Title: "Task 3"},
	}

	m := Model{
		tasks:              tasks,
		activePane:         PaneDetail,
		cursor:             0,
		detailScrollOffset: 0,
		width:              120,
		height:             20,
		theme:              NewKanagawaTheme(),
	}

	maxDetailOffset := m.maxDetailScrollOffset()
	if maxDetailOffset <= 0 {
		t.Fatalf("Expected maxDetailOffset > 0, got %d", maxDetailOffset)
	}

	// 1. Jump to bottom of right pane with 'G'
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(Model)
	if m.detailScrollOffset != maxDetailOffset {
		t.Errorf("Expected detailScrollOffset to equal maxDetailOffset (%d) after 'G', got %d", maxDetailOffset, m.detailScrollOffset)
	}

	// 2. Press 'j' (down) 20 times past bottom: must NOT accumulate!
	for i := 0; i < 20; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = updated.(Model)
	}
	if m.detailScrollOffset != maxDetailOffset {
		t.Errorf("Expected detailScrollOffset to remain clamped at %d after mashing 'j', got %d", maxDetailOffset, m.detailScrollOffset)
	}

	// 3. Press 'k' (up) ONCE: must scroll up immediately by 1 line!
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if m.detailScrollOffset != maxDetailOffset-1 {
		t.Errorf("Expected detailScrollOffset to decrement immediately to %d on single 'k', got %d", maxDetailOffset-1, m.detailScrollOffset)
	}

	// 4. Press 'k' (up) past top: must clamp to 0 and not become negative
	for i := 0; i < maxDetailOffset+10; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = updated.(Model)
	}
	if m.detailScrollOffset != 0 {
		t.Errorf("Expected detailScrollOffset to clamp at 0, got %d", m.detailScrollOffset)
	}

	// 5. Test left pane (Tasks List)
	m.activePane = PaneList
	m.cursor = 0

	// Mash 'k' 10 times past top
	for i := 0; i < 10; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = updated.(Model)
	}
	if m.cursor != 0 {
		t.Errorf("Expected cursor to remain at 0, got %d", m.cursor)
	}

	// Mash 'j' 20 times past bottom (tasks length is 3, max cursor is 2)
	for i := 0; i < 20; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = updated.(Model)
	}
	if m.cursor != 2 {
		t.Errorf("Expected cursor to clamp at last task index 2, got %d", m.cursor)
	}

	// Single 'k' from bottom must move to index 1 immediately
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Errorf("Expected cursor to move immediately to 1 on single 'k', got %d", m.cursor)
	}
}

func TestTUIConfirmationDialogsAndCancellation(t *testing.T) {
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Task One", Status: domain.StatusOpen, Priority: domain.PriorityNormal},
		{ID: "T-2", Title: "Task Two", Status: domain.StatusInProgress, Priority: domain.PriorityHigh},
		{ID: "T-3", Title: "Task Three", Status: domain.StatusDone, Priority: domain.PriorityUrgent},
	}

	m := Model{
		tasks:     tasks,
		cursor:    0,
		width:     100,
		height:    30,
		theme:     NewKanagawaTheme(),
		undoStack: make([]UndoAction, 0),
	}

	// 1. Test 'x' (Delete) opens ConfirmDelete dialog
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(Model)
	if m.confirmDialog == nil || m.confirmDialog.Kind != ConfirmDelete {
		t.Fatalf("Expected confirmDialog of kind ConfirmDelete, got %+v", m.confirmDialog)
	}
	if len(m.confirmDialog.TargetIDs) != 1 || m.confirmDialog.TargetIDs[0] != "T-1" {
		t.Errorf("Expected target ID T-1, got %v", m.confirmDialog.TargetIDs)
	}

	// 1b. Test cancellation with 'n'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)
	if m.confirmDialog != nil {
		t.Errorf("Expected confirmDialog to be nil after 'n', got %+v", m.confirmDialog)
	}
	if m.statusMsg != "Action cancelled" {
		t.Errorf("Expected statusMsg 'Action cancelled', got %q", m.statusMsg)
	}

	// 2. Test 'd' (Done) on open task opens ConfirmDone dialog
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(Model)
	if m.confirmDialog == nil || m.confirmDialog.Kind != ConfirmDone {
		t.Fatalf("Expected confirmDialog of kind ConfirmDone, got %+v", m.confirmDialog)
	}
	// Cancel with esc
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.confirmDialog != nil {
		t.Errorf("Expected confirmDialog to be nil after esc, got %+v", m.confirmDialog)
	}

	// 2b. Move to T-3 (Done task) and test 'd' triggers ConfirmReopen
	m.cursor = 2
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(Model)
	if m.confirmDialog == nil || m.confirmDialog.Kind != ConfirmReopen {
		t.Fatalf("Expected confirmDialog of kind ConfirmReopen on done task, got %+v", m.confirmDialog)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)

	// 3. Test 's' (Status toggle) opens ConfirmStatus dialog
	m.cursor = 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if m.confirmDialog == nil || m.confirmDialog.Kind != ConfirmStatus {
		t.Fatalf("Expected confirmDialog of kind ConfirmStatus, got %+v", m.confirmDialog)
	}
	if m.confirmDialog.TargetStatus != domain.StatusInProgress {
		t.Errorf("Expected target status InProgress, got %s", m.confirmDialog.TargetStatus)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)

	// 4. Test 'a' (Claim) opens ConfirmClaim dialog
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(Model)
	if m.confirmDialog == nil || m.confirmDialog.Kind != ConfirmClaim {
		t.Fatalf("Expected confirmDialog of kind ConfirmClaim, got %+v", m.confirmDialog)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)
}

func TestTUIBatchConfirmationDialogs(t *testing.T) {
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Task One", Status: domain.StatusOpen, Priority: domain.PriorityNormal},
		{ID: "T-2", Title: "Task Two", Status: domain.StatusInProgress, Priority: domain.PriorityHigh},
	}

	m := Model{
		tasks:    tasks,
		cursor:   0,
		width:    100,
		height:   30,
		theme:    NewKanagawaTheme(),
		selected: map[string]bool{"T-1": true, "T-2": true},
	}

	// 1. Batch delete
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(Model)
	if m.confirmDialog == nil || !m.confirmDialog.IsBatch || m.confirmDialog.Kind != ConfirmDelete {
		t.Fatalf("Expected batch ConfirmDelete, got %+v", m.confirmDialog)
	}
	if len(m.confirmDialog.TargetIDs) != 2 {
		t.Errorf("Expected 2 target IDs, got %d", len(m.confirmDialog.TargetIDs))
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)

	// 2. Batch done
	m.selected = map[string]bool{"T-1": true, "T-2": true}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(Model)
	if m.confirmDialog == nil || !m.confirmDialog.IsBatch || m.confirmDialog.Kind != ConfirmDone {
		t.Fatalf("Expected batch ConfirmDone, got %+v", m.confirmDialog)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)

	// 3. Batch claim
	m.selected = map[string]bool{"T-1": true, "T-2": true}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(Model)
	if m.confirmDialog == nil || !m.confirmDialog.IsBatch || m.confirmDialog.Kind != ConfirmClaim {
		t.Fatalf("Expected batch ConfirmClaim, got %+v", m.confirmDialog)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)
}

func TestTUIUndoStackOperations(t *testing.T) {
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Task One", Status: domain.StatusOpen, Priority: domain.PriorityNormal, Assignee: "initial-author"},
	}

	m := Model{
		tasks:     tasks,
		cursor:    0,
		width:     120,
		height:    30,
		theme:     NewKanagawaTheme(),
		undoStack: make([]UndoAction, 0),
	}

	// 1. Initial state: 'u' reports nothing to undo
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(Model)
	if m.statusMsg != "Nothing to undo" {
		t.Errorf("Expected 'Nothing to undo', got %q", m.statusMsg)
	}

	// 2. Open Claim dialog and confirm with 'y'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(Model)
	if m.confirmDialog == nil {
		t.Fatal("Expected confirmDialog to be open")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)

	// Confirm dialog must be closed and undo stack must have 1 entry
	if m.confirmDialog != nil {
		t.Errorf("Expected confirmDialog to be cleared after confirmation")
	}
	if len(m.undoStack) != 1 {
		t.Fatalf("Expected 1 item on undoStack, got %d", len(m.undoStack))
	}
	if m.undoStack[0].Kind != UndoClaim {
		t.Errorf("Expected UndoClaim kind, got %v", m.undoStack[0].Kind)
	}
	if m.undoStack[0].Snapshots[0].PreviousAssignee != "initial-author" {
		t.Errorf("Expected previous assignee 'initial-author', got %q", m.undoStack[0].Snapshots[0].PreviousAssignee)
	}

	// Footer must now display [u] Undo
	footer := m.renderFooter(m.width)
	if !strings.Contains(footer, "[u] Undo") {
		t.Errorf("Expected footer to contain '[u] Undo' when undoStack is non-empty, got:\n%s", footer)
	}

	// 3. Pop undo with 'u'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(Model)
	if len(m.undoStack) != 0 {
		t.Errorf("Expected undoStack to be empty after 'u', got %d", len(m.undoStack))
	}
	if !strings.Contains(m.statusMsg, "Undid:") {
		t.Errorf("Expected statusMsg to contain 'Undid:', got %q", m.statusMsg)
	}
}

func TestTUIModalFloatingWindowRendering(t *testing.T) {
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Database migration task", Status: domain.StatusOpen, Priority: domain.PriorityUrgent},
	}

	m := Model{
		tasks:  tasks,
		cursor: 0,
		width:  100,
		height: 24,
		theme:  NewKanagawaTheme(),
		confirmDialog: &ConfirmDialog{
			Kind:      ConfirmDelete,
			Title:     "CONFIRM DELETION",
			Prompt:    "Delete task T-1 permanently?",
			Details:   []string{"T-1: Database migration task"},
			TargetIDs: []string{"T-1"},
		},
	}

	out := m.View()
	if !strings.Contains(out, "CONFIRM DELETION") {
		t.Errorf("Expected modal title 'CONFIRM DELETION' in View(), got:\n%s", out)
	}
	if !strings.Contains(out, "Delete task T-1 permanently?") {
		t.Errorf("Expected modal prompt in View(), got:\n%s", out)
	}
	if !strings.Contains(out, "[y / Enter]") || !strings.Contains(out, "[n / Esc]") {
		t.Errorf("Expected modal action keys in View(), got:\n%s", out)
	}
}

func TestTUITreeViewModeToggleAndRows(t *testing.T) {
	now := time.Now()
	tasks := []*domain.Task{
		{ID: "T-1", Title: "Parent Task", Status: domain.StatusDone, Priority: domain.PriorityNormal, Blocks: []string{"T-2"}, CreatedAt: now},
		{ID: "T-2", Title: "Child Task", Status: domain.StatusOpen, Priority: domain.PriorityHigh, DependsOn: []string{"T-1"}, IsBlocked: false, CreatedAt: now},
		{ID: "T-3", Title: "Independent Task", Status: domain.StatusOpen, Priority: domain.PriorityLow, CreatedAt: now},
	}

	m := Model{
		tasks:        tasks,
		cursor:       0,
		width:        100,
		height:       24,
		theme:        NewKanagawaTheme(),
		treeViewMode: false,
	}

	// 1. Verify getTreeRows builds correct hierarchy
	treeRows := m.getTreeRows()
	if len(treeRows) != 3 {
		t.Fatalf("expected 3 tree rows, got %d", len(treeRows))
	}
	if treeRows[0].Task.ID != "T-1" || treeRows[0].Prefix != "" {
		t.Errorf("expected row 0 to be root T-1 with empty prefix, got %s %q", treeRows[0].Task.ID, treeRows[0].Prefix)
	}
	if treeRows[1].Task.ID != "T-2" || (!strings.Contains(treeRows[1].Prefix, "└──") && !strings.Contains(treeRows[1].Prefix, "\\--")) {
		t.Errorf("expected row 1 to be child T-2 with branch prefix, got %s %q", treeRows[1].Task.ID, treeRows[1].Prefix)
	}
	if treeRows[2].Task.ID != "T-3" || treeRows[2].Prefix != "" {
		t.Errorf("expected row 2 to be root T-3 with empty prefix, got %s %q", treeRows[2].Task.ID, treeRows[2].Prefix)
	}

	// 2. Test 't' key toggles tree mode
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(Model)
	if !m.treeViewMode {
		t.Fatalf("expected treeViewMode to be true after pressing 't'")
	}
	if !strings.Contains(m.statusMsg, "Tree view mode enabled") {
		t.Errorf("expected statusMsg to confirm tree mode, got %q", m.statusMsg)
	}

	// 3. Render View in tree mode: verify TREE / TITLE header and branch prefix
	viewOut := m.View()
	if !strings.Contains(viewOut, "TREE / TITLE") {
		t.Errorf("expected 'TREE / TITLE' column in tree mode view, got:\n%s", viewOut)
	}
	if !strings.Contains(viewOut, "TREE MODE") {
		t.Errorf("expected 'TREE MODE' in top header, got:\n%s", viewOut)
	}
	if !strings.Contains(viewOut, "Child Task") {
		t.Errorf("expected child task in tree mode view, got:\n%s", viewOut)
	}

	// 4. Toggle back with 't'
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(Model)
	if m.treeViewMode {
		t.Fatalf("expected treeViewMode to be false after second 't'")
	}
}

func TestTUIBlockedTaskDoneProtection(t *testing.T) {
	now := time.Now()
	tasks := []*domain.Task{
		{
			ID:        "T-2",
			Title:     "Blocked Child Task",
			Status:    domain.StatusOpen,
			Priority:  domain.PriorityHigh,
			DependsOn: []string{"T-1"},
			IsBlocked: true,
			BlockedBy: []string{"T-1"},
			CreatedAt: now,
		},
	}

	m := Model{
		tasks:  tasks,
		cursor: 0,
		width:  100,
		height: 24,
		theme:  NewKanagawaTheme(),
	}

	// Press 'd' to mark done on blocked task: should be rejected with warning!
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(Model)

	if m.confirmDialog != nil {
		t.Fatalf("expected NO confirmDialog when attempting to complete blocked task, got %v", m.confirmDialog)
	}
	if !strings.Contains(m.statusMsg, "blocked by unfinished prerequisite") {
		t.Fatalf("expected warning message about unfinished prerequisites, got %q", m.statusMsg)
	}
}

func TestTUILinkModalInteraction(t *testing.T) {
	now := time.Now()
	tasks := []*domain.Task{
		{ID: "T-2", Title: "Build API", Status: domain.StatusOpen, Priority: domain.PriorityNormal, CreatedAt: now},
	}

	m := Model{
		tasks:  tasks,
		cursor: 0,
		width:  100,
		height: 24,
		theme:  NewKanagawaTheme(),
	}

	// 1. Press 'L' to open link modal
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	m = updated.(Model)

	if !m.isLinking {
		t.Fatalf("expected isLinking to be true after 'L'")
	}
	if m.linkTaskID != "T-2" {
		t.Fatalf("expected linkTaskID to be T-2, got %s", m.linkTaskID)
	}

	// 2. View() should display floating link modal
	modalView := m.View()
	if !strings.Contains(modalView, "LINK PREREQUISITE DEPENDENCY") {
		t.Fatalf("expected modal title in View(), got:\n%s", modalView)
	}
	if !strings.Contains(modalView, "Make task T-2 depend on prerequisite") {
		t.Fatalf("expected prompt in View(), got:\n%s", modalView)
	}

	// 3. Type 'T', '-', '1'
	for _, r := range []rune{'T', '-', '1'} {
		up, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = up.(Model)
	}
	if m.linkInput != "T-1" {
		t.Fatalf("expected linkInput to be 'T-1', got %q", m.linkInput)
	}

	// 4. Test backspace
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = up.(Model)
	if m.linkInput != "T-" {
		t.Fatalf("expected linkInput to be 'T-' after backspace, got %q", m.linkInput)
	}

	// 5. Test Escape cancels link modal
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = up.(Model)
	if m.isLinking {
		t.Fatalf("expected isLinking to be false after Esc")
	}
	if m.statusMsg != "Link cancelled" {
		t.Fatalf("expected 'Link cancelled' statusMsg, got %q", m.statusMsg)
	}
}
