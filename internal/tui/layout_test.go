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

// layoutFixture builds tasks whose detail content varies wildly in length, which is
// exactly what used to make the stacked layout jump around.
func layoutFixture(n int) []*domain.Task {
	tasks := make([]*domain.Task, n)
	for i := range tasks {
		t := &domain.Task{
			ID:        fmt.Sprintf("T-%d", i+1),
			Title:     fmt.Sprintf("Task %d %s", i+1, strings.Repeat("with a long title ", i%4)),
			Status:    domain.StatusOpen,
			Priority:  domain.PriorityHigh,
			Tags:      []string{"perf", "scale"},
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		}
		if i%3 == 0 {
			t.Description = strings.Repeat("A long description sentence that wraps. ", 30)
			for j := 0; j < 12; j++ {
				t.Notes = append(t.Notes, domain.TaskNote{Author: "Claude", Content: strings.Repeat("note text ", 25), CreatedAt: time.Now()})
			}
		}
		tasks[i] = t
	}
	return tasks
}

func frameLines(t *testing.T, m Model) []string {
	t.Helper()
	return strings.Split(m.View(), "\n")
}

func TestViewAlwaysFillsExactlyOneScreen(t *testing.T) {
	sizes := [][2]int{{60, 20}, {80, 24}, {95, 46}, {109, 30}, {110, 30}, {160, 50}}
	for _, taskCount := range []int{0, 1, 3, 40} {
		for _, sz := range sizes {
			for _, active := range []Pane{PaneList, PaneDetail} {
				name := fmt.Sprintf("%dx%d/tasks=%d/pane=%d", sz[0], sz[1], taskCount, active)
				t.Run(name, func(t *testing.T) {
					m := Model{tasks: layoutFixture(taskCount), width: sz[0], height: sz[1], theme: NewKanagawaTheme(), activePane: active}
					for cursor := 0; cursor < min(taskCount, 6); cursor++ {
						m.cursor = cursor
						lines := frameLines(t, m)
						if len(lines) != sz[1] {
							t.Fatalf("cursor %d: frame has %d lines, want exactly %d", cursor, len(lines), sz[1])
						}
						for i, line := range lines {
							if w := ansi.StringWidth(line); w > sz[0] {
								t.Fatalf("cursor %d: line %d is %d cells wide (> %d): %q", cursor, i, w, sz[0], ansi.Strip(line))
							}
						}
						if !strings.Contains(ansi.Strip(lines[0]), "tk:") {
							t.Fatalf("header not on first line: %q", ansi.Strip(lines[0]))
						}
						if !strings.Contains(ansi.Strip(lines[len(lines)-1]), "[") {
							t.Fatalf("legend not on last line: %q", ansi.Strip(lines[len(lines)-1]))
						}
					}
				})
			}
		}
	}
}

// The stacked layout must not move when navigating between tasks with very different detail content.
func TestStackedLayoutIsStableWhileNavigating(t *testing.T) {
	m := Model{tasks: layoutFixture(30), width: 95, height: 46, theme: NewKanagawaTheme()}
	if m.isSplitView() {
		t.Fatal("precondition: expected stacked layout at width 95")
	}
	detailBarRow := func(m Model) int {
		for i, line := range frameLines(t, m) {
			if strings.Contains(ansi.Strip(line), "Details & Messages") {
				return i
			}
		}
		return -1
	}
	want := detailBarRow(m)
	if want <= 2 {
		t.Fatalf("detail title bar not found below the list (row %d)", want)
	}
	for i := 0; i < 12; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = updated.(Model)
		if got := detailBarRow(m); got != want {
			t.Fatalf("after %d moves the detail pane moved from row %d to %d", i+1, want, got)
		}
	}
	// Focusing the detail pane (l) and back (h) must not move it either.
	for _, k := range []rune{'l', 'h'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = updated.(Model)
		if got := detailBarRow(m); got != want {
			t.Fatalf("after %q the detail pane moved from row %d to %d", k, want, got)
		}
	}
}

// The cursor must always stay on a visible row (never hidden behind the pagination line).
func TestCursorAlwaysVisible(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 30}} {
		m := Model{tasks: layoutFixture(50), width: sz[0], height: sz[1], theme: NewKanagawaTheme()}
		for i := 0; i < 49; i++ {
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
			m = updated.(Model)
			found := false
			for _, line := range frameLines(t, m) {
				if strings.Contains(ansi.Strip(line), "► "+m.tasks[m.cursor].ID+" ") {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%dx%d: cursor row %s not rendered after %d moves", sz[0], sz[1], m.tasks[m.cursor].ID, i+1)
			}
		}
	}
}

func TestDetailPaneScrollsInStackedMode(t *testing.T) {
	m := Model{tasks: layoutFixture(5), width: 90, height: 30, theme: NewKanagawaTheme()}
	if m.maxDetailScrollOffset() == 0 {
		t.Fatal("precondition: fixture task 1 should overflow the stacked detail pane")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	before := m.View()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.detailScrollOffset != 1 {
		t.Fatalf("expected detail scroll offset 1, got %d", m.detailScrollOffset)
	}
	if m.View() == before {
		t.Fatal("scrolling the detail pane did not change the rendered frame")
	}
}

func TestHeaderKeepsSearchInputVisibleWhenNarrow(t *testing.T) {
	m := Model{tasks: layoutFixture(3), width: 50, height: 20, theme: NewKanagawaTheme(), isSearch: true, filterText: "villager"}
	header := ansi.Strip(frameLines(t, m)[0])
	if !strings.Contains(header, "villager") {
		t.Fatalf("search input not visible in narrow header: %q", header)
	}
}
