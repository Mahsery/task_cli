package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"task_cli/internal/client"
	"task_cli/internal/domain"
	"task_cli/internal/editor"
	"task_cli/internal/git"
	"task_cli/internal/protocol"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	client       *client.Client
	editorCmd    string
	tasks        []*domain.Task
	cursor       int
	scrollOffset int
	width        int
	height       int
	scope        string
	repoPath     string
	filterText   string
	prevFilter   string
	isSearch     bool
	statusMsg    string
	err          error
	showDiff     bool
}

func NewModel(c *client.Client, editorCmd, scope, repoPath string) Model {
	return Model{
		client:       c,
		editorCmd:    editorCmd,
		scope:        scope,
		repoPath:     repoPath,
		cursor:       0,
		scrollOffset: 0,
	}
}

type reloadMsg []*domain.Task
type statusMsg string
type errMsg error

func (m Model) Init() tea.Cmd {
	return m.fetchTasksCmd()
}

func (m Model) fetchTasksCmd() tea.Cmd {
	return func() tea.Msg {
		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, err := m.client.ListTasks(filter)
		if err != nil {
			return errMsg(err)
		}
		return reloadMsg(tasks)
	}
}

func (m Model) getMaxRows() int {
	if m.height <= 0 {
		return 8
	}
	// Reserve 14 lines for headers, detail pane, borders, and footer
	rows := m.height - 14
	if rows < 5 {
		return 5
	}
	if rows > 25 {
		return 25
	}
	return rows
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	maxRows := m.getMaxRows()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case reloadMsg:
		m.tasks = msg
		if m.cursor >= len(m.tasks) {
			if len(m.tasks) > 0 {
				m.cursor = len(m.tasks) - 1
			} else {
				m.cursor = 0
			}
		}
		if m.cursor < m.scrollOffset {
			m.scrollOffset = m.cursor
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		return m, nil

	case statusMsg:
		m.statusMsg = string(msg)
		return m, nil

	case errMsg:
		m.err = msg
		m.statusMsg = fmt.Sprintf("Error: %v", msg)
		return m, nil

	case tea.KeyMsg:
		if m.isSearch {
			switch msg.String() {
			case "enter":
				m.isSearch = false
				return m, m.fetchTasksCmd()
			case "esc":
				m.isSearch = false
				m.filterText = ""
				return m, m.fetchTasksCmd()
			case "backspace":
				runes := []rune(m.filterText)
				if len(runes) > 0 {
					m.filterText = string(runes[:len(runes)-1])
					return m, m.fetchTasksCmd()
				}
			default:
				if len(msg.Runes) > 0 {
					m.filterText += string(msg.Runes)
					return m, m.fetchTasksCmd()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scrollOffset {
					m.scrollOffset = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
				if m.cursor >= m.scrollOffset+maxRows {
					m.scrollOffset = m.cursor - maxRows + 1
				}
			}

		case "esc", "c":
			if m.filterText != "" {
				m.filterText = ""
				m.statusMsg = "Filter reset"
				return m, m.fetchTasksCmd()
			}

		case "g":
			// Toggle local / global scope
			if m.scope == "local" {
				m.scope = "global"
			} else {
				m.scope = "local"
			}
			m.client.Scope = m.scope
			m.statusMsg = fmt.Sprintf("Switched to %s scope", m.scope)
			return m, m.fetchTasksCmd()

		case "r":
			m.statusMsg = "Refreshed"
			return m, m.fetchTasksCmd()

		case "/":
			m.isSearch = true
			m.prevFilter = m.filterText
			m.filterText = ""
			return m, nil

		case "enter":
			m.showDiff = !m.showDiff

		case "x":
			// Delete task
			if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				return m, m.deleteTaskCmd(t.ID)
			}

		case "d":
			// Reversible Done: marks done or reopens if already done
			if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				if t.Status == domain.StatusDone || t.Status == domain.StatusClosed {
					return m, m.reopenTaskCmd(t.ID)
				}
				return m, m.markDoneCmd(t.ID)
			}

		case "s":
			// Toggle status: open <-> in_progress
			if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				newStatus := domain.StatusInProgress
				if t.Status == domain.StatusInProgress {
					newStatus = domain.StatusOpen
				} else if t.Status == domain.StatusDone || t.Status == domain.StatusClosed {
					newStatus = domain.StatusInProgress
				}
				return m, m.updateStatusCmd(t.ID, newStatus)
			}

		case "n":
			// Create new task via editor
			tmpPath, err := editor.CreateNewTaskFile("")
			if err != nil {
				return m, func() tea.Msg { return errMsg(err) }
			}
			cmd := editor.BuildEditorCmd(m.editorCmd, tmpPath)
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				defer os.Remove(tmpPath)
				if err != nil {
					return errMsg(err)
				}
				data, err := os.ReadFile(tmpPath)
				if err != nil {
					return errMsg(err)
				}
				parsed, err := editor.ParseNewTaskContent(string(data))
				if err != nil {
					return errMsg(err)
				}
				author := git.GetAuthor()
				_, err = m.client.CreateTask(protocol.CreateTaskPayload{
					Title:       parsed.Title,
					Description: parsed.Description,
					Status:      domain.StatusOpen,
					Priority:    parsed.Priority,
					Tags:        parsed.Tags,
					Author:      author,
					Assignee:    parsed.Assignee,
				})
				if err != nil {
					return errMsg(err)
				}
				return m.fetchTasksCmd()()
			})

		case "e":
			// Edit existing task via editor
			if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				tmpPath, err := editor.CreateEditTaskFile(t)
				if err != nil {
					return m, func() tea.Msg { return errMsg(err) }
				}
				cmd := editor.BuildEditorCmd(m.editorCmd, tmpPath)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					defer os.Remove(tmpPath)
					if err != nil {
						return errMsg(err)
					}
					data, err := os.ReadFile(tmpPath)
					if err != nil {
						return errMsg(err)
					}
					parsed, err := editor.ParseEditTaskContent(string(data), t)
					if err != nil {
						return errMsg(err)
					}
					author := git.GetAuthor()
					var addNote *string
					if parsed.NewNote != "" {
						addNote = &parsed.NewNote
					}
					_, err = m.client.UpdateTask(protocol.UpdateTaskPayload{
						ID:          t.ID,
						Title:       &parsed.Title,
						Description: &parsed.Description,
						Status:      &parsed.Status,
						Priority:    &parsed.Priority,
						Tags:        &parsed.Tags,
						Assignee:    &parsed.Assignee,
						AddNote:     addNote,
						Author:      author,
					})
					if err != nil {
						return errMsg(err)
					}
					return m.fetchTasksCmd()()
				})
			}
		}
	}

	return m, nil
}

func (m Model) deleteTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.DeleteTask(taskID)
		if err != nil {
			return errMsg(err)
		}
		return m.fetchTasksCmd()()
	}
}

func (m Model) reopenTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		st := domain.StatusOpen
		note := "Reopened task"
		_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
			ID:      taskID,
			Status:  &st,
			AddNote: &note,
		})
		if err != nil {
			return errMsg(err)
		}
		return m.fetchTasksCmd()()
	}
}

func (m Model) updateStatusCmd(taskID string, newStatus domain.Status) tea.Cmd {
	return func() tea.Msg {
		_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{
			ID:     taskID,
			Status: &newStatus,
		})
		if err != nil {
			return errMsg(err)
		}
		return m.fetchTasksCmd()()
	}
}

func (m Model) markDoneCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		// Attempt to grab latest git commit if local
		var sha, msg, stat, diff string
		if m.scope == "local" && m.repoPath != "" {
			if commit, err := git.GetLatestCommit(m.repoPath); err == nil {
				sha = commit.ShortSHA
				msg = commit.Subject
				stat = commit.DiffStat
				diff = commit.DiffContent
			}
		}

		_, err := m.client.AttachGit(taskID, sha, msg, stat, diff, true)
		if err != nil {
			return errMsg(err)
		}

		filter := domain.TaskFilter{
			Scope:    m.scope,
			RepoPath: m.repoPath,
			Search:   m.filterText,
		}
		tasks, _ := m.client.ListTasks(filter)
		return reloadMsg(tasks)
	}
}

func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 80
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("236")).
		Padding(0, 1)

	borderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	var sb strings.Builder

	// 1. Header
	openCount, progCount, doneCount := 0, 0, 0
	for _, t := range m.tasks {
		switch t.Status {
		case domain.StatusOpen:
			openCount++
		case domain.StatusInProgress:
			progCount++
		case domain.StatusDone, domain.StatusClosed:
			doneCount++
		}
	}

	scopeLabel := fmt.Sprintf("[%s]", m.scope)
	countsLabel := fmt.Sprintf("%d Open | %d Active | %d Done", openCount, progCount, doneCount)
	headerText := fmt.Sprintf("tk: task_cli %s  ─  %s", scopeLabel, countsLabel)
	if m.isSearch {
		headerText += fmt.Sprintf("  ─  [Search: %s_] (Enter apply, Esc cancel)", m.filterText)
	} else if m.filterText != "" {
		headerText += fmt.Sprintf("  ─  [Filter: %q (Esc to clear)]", m.filterText)
	}
	sb.WriteString(headerStyle.Width(width).Render(headerText))
	sb.WriteString("\n")

	// Table columns header
	colHeader := fmt.Sprintf("  %-6s %-10s %-8s %-32s %-14s %s", "ID", "STATUS", "PRI", "TITLE", "TAGS", "AGE")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250")).Render(colHeader))
	sb.WriteString("\n")
	sb.WriteString(borderStyle.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	// 2. Task rows (scrollable window)
	maxRows := m.getMaxRows()
	if len(m.tasks) == 0 {
		sb.WriteString("  No tasks found. Press 'n' to create a new task.\n")
	} else {
		endIdx := m.scrollOffset + maxRows
		if endIdx > len(m.tasks) {
			endIdx = len(m.tasks)
		}

		for i := m.scrollOffset; i < endIdx; i++ {
			t := m.tasks[i]
			cursor := " "
			rowStyle := lipgloss.NewStyle()
			if i == m.cursor {
				cursor = "►"
				rowStyle = rowStyle.Bold(true).Background(lipgloss.Color("237")).Foreground(lipgloss.Color("15"))
			}

			title := t.Title
			if len(title) > 30 {
				title = title[:28] + ".."
			}

			tagsStr := strings.Join(t.Tags, ",")
			if len(tagsStr) > 12 {
				tagsStr = tagsStr[:10] + ".."
			}

			age := timeAgo(t.CreatedAt)
			if t.CommitSHA != "" {
				sha := t.CommitSHA
				if len(sha) > 6 {
					sha = sha[:6]
				}
				age = fmt.Sprintf("%s (%s)", age, sha)
			}

			line := fmt.Sprintf("%s %-6s %-10s %-8s %-32s %-14s %s",
				cursor, t.ID, string(t.Status), string(t.Priority), title, tagsStr, age,
			)
			sb.WriteString(rowStyle.Width(width).Render(line))
			sb.WriteString("\n")
		}

		if len(m.tasks) > maxRows {
			pagination := fmt.Sprintf("  ── Showing %d-%d of %d tasks (▲/▼ to scroll) ──", m.scrollOffset+1, endIdx, len(m.tasks))
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(pagination))
			sb.WriteString("\n")
		}
	}

	sb.WriteString(borderStyle.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	// 3. Detail Pane
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		t := m.tasks[m.cursor]
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render(fmt.Sprintf("%s: %s", t.ID, t.Title)))
		sb.WriteString("\n")

		if t.Description != "" {
			descLines := strings.Split(t.Description, "\n")
			for i, line := range descLines {
				if i >= 4 {
					sb.WriteString("  ...\n")
					break
				}
				sb.WriteString("  " + line + "\n")
			}
		}

		if t.CommitSHA != "" {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(
				fmt.Sprintf("  + Resolved by commit %s: %s\n", t.CommitSHA, t.CommitMsg),
			))
			if t.DiffStat != "" {
				statLines := strings.Split(t.DiffStat, "\n")
				for _, sl := range statLines {
					sb.WriteString("    " + sl + "\n")
				}
			}
			if m.showDiff && t.DiffContent != "" {
				sb.WriteString("\n" + borderStyle.Render("── Diff Content ──") + "\n")
				diffLines := strings.Split(t.DiffContent, "\n")
				for i, dl := range diffLines {
					if i >= 10 {
						sb.WriteString("  [diff truncated, press enter to collapse]\n")
						break
					}
					sb.WriteString("  " + dl + "\n")
				}
			}
		}

		if len(t.Notes) > 0 {
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Render(
				fmt.Sprintf("  • %d note(s):\n", len(t.Notes)),
			))
			for i, n := range t.Notes {
				if i >= 3 {
					sb.WriteString(fmt.Sprintf("    ... and %d more note(s)\n", len(t.Notes)-3))
					break
				}
				authorStr := ""
				if n.Author != "" {
					authorStr = fmt.Sprintf(" by %s", n.Author)
				}
				sb.WriteString(fmt.Sprintf("    [%s%s] %s\n", timeAgo(n.CreatedAt), authorStr, n.Content))
			}
		}
	}

	sb.WriteString(borderStyle.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	// 4. Status Bar / Hotkey footer
	doneLabel := "[d] Done"
	statusLabel := "[s] In Prog"
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		switch m.tasks[m.cursor].Status {
		case domain.StatusDone, domain.StatusClosed:
			doneLabel = "[d] Reopen"
			statusLabel = "[s] In Prog"
		case domain.StatusInProgress:
			doneLabel = "[d] Done"
			statusLabel = "[s] Open"
		default: // open
			doneLabel = "[d] Done"
			statusLabel = "[s] In Prog"
		}
	}

	footer := fmt.Sprintf("[j/k] Move  [n] New  [e] Edit  %s  %s  [x] Del  [Enter] Diff  [/] Search  [Esc] Clear  [g] Scope  [r] Refresh  [q] Quit", doneLabel, statusLabel)
	if m.statusMsg != "" {
		footer = fmt.Sprintf("[%s]  %s", m.statusMsg, footer)
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(footer))

	return sb.String()
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
