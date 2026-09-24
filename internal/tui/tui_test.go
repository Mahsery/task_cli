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
