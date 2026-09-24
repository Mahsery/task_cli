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
	theme        Theme
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
		theme:        NewKanagawaTheme(),
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

	theme := m.theme
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

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(WaveAqua2))
	var scopeBadge string
	if m.scope == "global" {
		scopeBadge = theme.ScopeGlobal.Render("[global]")
	} else {
		scopeBadge = theme.ScopeLocal.Render("[local]")
	}

	countsLabel := fmt.Sprintf("%s │ %s │ %s",
		theme.CountOpen.Render(fmt.Sprintf("%d Open", openCount)),
		theme.CountProg.Render(fmt.Sprintf("%d Active", progCount)),
		theme.CountDone.Render(fmt.Sprintf("%d Done", doneCount)),
	)
	headerText := fmt.Sprintf("%s %s  ─  %s", titleStyle.Render("tk: task_cli"), scopeBadge, countsLabel)
	if m.isSearch {
		headerText += "  ─  " + theme.SearchPrompt.Render(fmt.Sprintf("[Search: %s_] (Enter apply, Esc cancel)", m.filterText))
	} else if m.filterText != "" {
		headerText += "  ─  " + theme.FilterIndicator.Render(fmt.Sprintf("[Filter: %q (Esc to clear)]", m.filterText))
	}
	sb.WriteString(theme.Header.Width(width).Render(headerText))
	sb.WriteString("\n")

	// Table columns header
	colHeader := fmt.Sprintf("  %-6s %-12s %-8s %-32s %-14s %s", "ID", "STATUS", "PRI", "TITLE", "TAGS", "AGE")
	sb.WriteString(theme.ColHeader.Render(colHeader))
	sb.WriteString("\n")
	sb.WriteString(theme.Border.Render(strings.Repeat("─", width)))
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
			isCursor := (i == m.cursor)
			cursor := " "
			if isCursor {
				cursor = theme.Cursor.Render("►")
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

			idStr := theme.TaskID.Render(fmt.Sprintf("%-6s", t.ID))
			statusStr := StatusStyle(t.Status).Render(fmt.Sprintf("%-12s", string(t.Status)))
			priStr := PriorityStyle(t.Priority).Render(fmt.Sprintf("%-8s", string(t.Priority)))
			titleStr := theme.TaskTitle.Render(fmt.Sprintf("%-32s", title))
			tagsDisplay := theme.Tag.Render(fmt.Sprintf("%-14s", tagsStr))
			ageDisplay := theme.Age.Render(age)

			line := fmt.Sprintf("%s %s %s %s %s %s %s",
				cursor, idStr, statusStr, priStr, titleStr, tagsDisplay, ageDisplay,
			)

			if isCursor {
				sb.WriteString(theme.RowSelected.Width(width).Render(line))
			} else {
				sb.WriteString(line)
			}
			sb.WriteString("\n")
		}

		if len(m.tasks) > maxRows {
			pagination := fmt.Sprintf("  ── Showing %d-%d of %d tasks (▲/▼ to scroll) ──", m.scrollOffset+1, endIdx, len(m.tasks))
			sb.WriteString(theme.FooterDesc.Render(pagination))
			sb.WriteString("\n")
		}
	}

	sb.WriteString(theme.Border.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	// 3. Detail Pane
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		t := m.tasks[m.cursor]
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			theme.DetailID.Render(t.ID+":"),
			theme.DetailTitle.Render(t.Title),
		))

		// Metadata line
		metaLine := fmt.Sprintf("  %s %s   %s %s   %s %s",
			theme.DetailLabel.Render("Status:"),
			StatusStyle(t.Status).Render(string(t.Status)),
			theme.DetailLabel.Render("Priority:"),
			PriorityStyle(t.Priority).Render(string(t.Priority)),
			theme.DetailLabel.Render("Scope:"),
			theme.TaskID.Render(t.Scope),
		)
		if t.Assignee != "" {
			metaLine += fmt.Sprintf("   %s %s", theme.DetailLabel.Render("Assignee:"), theme.NoteHeader.Render(t.Assignee))
		}
		if len(t.Tags) > 0 {
			metaLine += fmt.Sprintf("   %s %s", theme.DetailLabel.Render("Tags:"), theme.Tag.Render(strings.Join(t.Tags, ", ")))
		}
		sb.WriteString(metaLine + "\n\n")

		if t.Description != "" {
			descLines := strings.Split(t.Description, "\n")
			for i, line := range descLines {
				if i >= 4 {
					sb.WriteString(theme.Age.Render("    ...\n"))
					break
				}
				sb.WriteString("    " + theme.DetailDesc.Render(line) + "\n")
			}
		}

		if t.CommitSHA != "" {
			sb.WriteString(theme.DetailResolved.Render(
				fmt.Sprintf("  ✓ Resolved by commit %s: %s\n", t.CommitSHA, t.CommitMsg),
			))
			if t.DiffStat != "" {
				statLines := strings.Split(t.DiffStat, "\n")
				for _, sl := range statLines {
					sb.WriteString("    " + theme.DiffStat.Render(sl) + "\n")
				}
			}
			if m.showDiff && t.DiffContent != "" {
				sb.WriteString("\n  " + theme.Border.Render("── Diff Content ──") + "\n")
				diffLines := strings.Split(t.DiffContent, "\n")
				for i, dl := range diffLines {
					if i >= 10 {
						sb.WriteString(theme.Age.Render("    [diff truncated, press enter to collapse]\n"))
						break
					}
					if strings.HasPrefix(dl, "+") {
						sb.WriteString("    " + theme.DiffAdd.Render(dl) + "\n")
					} else if strings.HasPrefix(dl, "-") {
						sb.WriteString("    " + theme.DiffSub.Render(dl) + "\n")
					} else if strings.HasPrefix(dl, "@@") {
						sb.WriteString("    " + theme.DiffHunk.Render(dl) + "\n")
					} else {
						sb.WriteString("    " + theme.DetailDesc.Render(dl) + "\n")
					}
				}
			}
		}

		if len(t.Notes) > 0 {
			sb.WriteString(theme.NoteHeader.Render(
				fmt.Sprintf("  • %d note(s):\n", len(t.Notes)),
			))
			for i, n := range t.Notes {
				if i >= 3 {
					sb.WriteString(theme.Age.Render(fmt.Sprintf("    ... and %d more note(s)\n", len(t.Notes)-3)))
					break
				}
				authorStr := ""
				if n.Author != "" {
					authorStr = " by " + theme.NoteAuthor.Render(n.Author)
				}
				sb.WriteString(fmt.Sprintf("    [%s%s] %s\n",
					theme.NoteTime.Render(timeAgo(n.CreatedAt)),
					authorStr,
					theme.NoteContent.Render(n.Content),
				))
			}
		}
	}

	sb.WriteString(theme.Border.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	// 4. Status Bar / Hotkey footer
	doneLabel := theme.CountProg.Render("[d] Done")
	statusLabel := theme.CountOpen.Render("[s] In Prog")
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		switch m.tasks[m.cursor].Status {
		case domain.StatusDone, domain.StatusClosed:
			doneLabel = theme.CountDone.Render("[d] Reopen")
			statusLabel = theme.CountProg.Render("[s] In Prog")
		case domain.StatusInProgress:
			doneLabel = theme.CountProg.Render("[d] Done")
			statusLabel = theme.CountOpen.Render("[s] Open")
		default: // open
			doneLabel = theme.CountDone.Render("[d] Done")
			statusLabel = theme.CountProg.Render("[s] In Prog")
		}
	}

	key := func(k, action string) string {
		return theme.FooterKey.Render("["+k+"]") + " " + theme.FooterDesc.Render(action)
	}

	keys := []string{
		key("j/k", "Move"),
		key("n", "New"),
		key("e", "Edit"),
		doneLabel,
		statusLabel,
		theme.DiffSub.Render("[x]") + " " + theme.FooterDesc.Render("Del"),
		key("Enter", "Diff"),
		key("/", "Search"),
		theme.FilterIndicator.Render("[Esc]") + " " + theme.FooterDesc.Render("Clear"),
		key("g", "Scope"),
		key("r", "Refresh"),
		theme.Age.Render("[q] Quit"),
	}

	footerText := strings.Join(keys, "  ")
	if m.statusMsg != "" {
		footerText = theme.FooterStatus.Render("["+m.statusMsg+"]") + "  " + footerText
	}
	sb.WriteString(theme.FooterBg.Width(width).Render(footerText))

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
