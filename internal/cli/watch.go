package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

// WatchEventItem holds a recorded event for the live activity stream.
type WatchEventItem struct {
	Timestamp time.Time
	Message   string
	Type      protocol.EventType
	TaskID    string
}

// WatchState is a point-in-time immutable snapshot of dashboard data for rendering.
type WatchState struct {
	Scope        string
	RepoPath     string
	ActiveTasks  []*domain.Task
	BacklogTasks []*domain.Task
	TotalCount   int
	LatestTask   *domain.Task
	LatestTime   time.Time
	RecentEvents []WatchEventItem
}

// WatchDashboard coordinates thread-safe live state, event subscription, and rendering.
type WatchDashboard struct {
	mu           sync.Mutex
	scope        string
	repoPath     string
	tasks        map[string]*domain.Task
	latestTask   *domain.Task
	latestTime   time.Time
	recentEvents []WatchEventItem
	maxEvents    int
}

// NewWatchDashboard initializes a dashboard from an initial list of tasks.
func NewWatchDashboard(scope, repoPath string, initialTasks []*domain.Task) *WatchDashboard {
	d := &WatchDashboard{
		scope:        scope,
		repoPath:     repoPath,
		tasks:        make(map[string]*domain.Task, len(initialTasks)),
		recentEvents: make([]WatchEventItem, 0, 10),
		maxEvents:    8,
	}

	for _, t := range initialTasks {
		if t == nil {
			continue
		}
		d.tasks[t.ID] = t

		ts := taskLatestTime(t)
		if ts.After(d.latestTime) {
			d.latestTime = ts
			d.latestTask = t
		}
	}

	return d
}

// RecordEvent thread-safely updates the dashboard state from an incoming daemon event.
func (d *WatchDashboard) RecordEvent(ev protocol.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	taskID := ""
	if ev.Task != nil {
		taskID = ev.Task.ID
	}

	item := WatchEventItem{
		Timestamp: ts,
		Message:   ev.Message,
		Type:      ev.Type,
		TaskID:    taskID,
	}
	d.recentEvents = append(d.recentEvents, item)
	if len(d.recentEvents) > d.maxEvents {
		d.recentEvents = d.recentEvents[len(d.recentEvents)-d.maxEvents:]
	}

	if ev.Type == protocol.EventTaskDeleted {
		delID := taskID
		if delID == "" {
			delID = parseTaskIDFromMessage(ev.Message)
		}
		if delID != "" {
			delete(d.tasks, delID)
			if d.latestTask != nil && d.latestTask.ID == delID {
				d.recomputeLatestLocked()
			}
		}
		return
	}

	if ev.Task != nil {
		d.tasks[ev.Task.ID] = ev.Task
		d.latestTask = ev.Task
		d.latestTime = ts
	}
}

func (d *WatchDashboard) recomputeLatestLocked() {
	var bestTask *domain.Task
	var bestTime time.Time
	for _, t := range d.tasks {
		tTime := taskLatestTime(t)
		if tTime.After(bestTime) {
			bestTime = tTime
			bestTask = t
		}
	}
	d.latestTask = bestTask
	d.latestTime = bestTime
}

// Snapshot returns an immutable snapshot of dashboard data for rendering.
func (d *WatchDashboard) Snapshot() WatchState {
	d.mu.Lock()
	defer d.mu.Unlock()

	var active []*domain.Task
	var backlog []*domain.Task

	for _, t := range d.tasks {
		switch t.Status {
		case domain.StatusInProgress:
			active = append(active, t)
		case domain.StatusOpen:
			backlog = append(backlog, t)
		}
	}

	sortTasksForWatch(active)
	sortTasksForWatch(backlog)

	eventsCopy := make([]WatchEventItem, len(d.recentEvents))
	copy(eventsCopy, d.recentEvents)

	return WatchState{
		Scope:        d.scope,
		RepoPath:     d.repoPath,
		ActiveTasks:  active,
		BacklogTasks: backlog,
		TotalCount:   len(d.tasks),
		LatestTask:   d.latestTask,
		LatestTime:   d.latestTime,
		RecentEvents: eventsCopy,
	}
}

func sortTasksForWatch(tasks []*domain.Task) {
	sort.Slice(tasks, func(i, j int) bool {
		pi := priorityRank(tasks[i].Priority)
		pj := priorityRank(tasks[j].Priority)
		if pi != pj {
			return pi > pj
		}
		ti := taskLatestTime(tasks[i])
		tj := taskLatestTime(tasks[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return tasks[i].ID < tasks[j].ID
	})
}

func priorityRank(p domain.Priority) int {
	switch p {
	case domain.PriorityUrgent:
		return 4
	case domain.PriorityHigh:
		return 3
	case domain.PriorityNormal:
		return 2
	case domain.PriorityLow:
		return 1
	default:
		return 0
	}
}

func taskLatestTime(t *domain.Task) time.Time {
	if t == nil {
		return time.Time{}
	}
	latest := t.UpdatedAt
	if latest.IsZero() {
		latest = t.CreatedAt
	}
	for _, n := range t.Notes {
		if n.CreatedAt.After(latest) {
			latest = n.CreatedAt
		}
	}
	if t.ClosedAt != nil && t.ClosedAt.After(latest) {
		latest = *t.ClosedAt
	}
	return latest
}

func parseTaskIDFromMessage(msg string) string {
	parts := strings.Fields(msg)
	for _, p := range parts {
		if strings.HasPrefix(p, "T-") || strings.HasPrefix(p, "G-") {
			return strings.Trim(p, ":,.\"')")
		}
	}
	return ""
}

// Render writes a flicker-free frame to the given writer.
func (d *WatchDashboard) Render(w io.Writer) {
	width := 80
	height := 24
	fd := os.Stdout.Fd()
	if term.IsTerminal(fd) {
		if tw, th, err := term.GetSize(fd); err == nil {
			if tw > 0 {
				width = tw
			}
			if th > 0 {
				height = th
			}
		}
	}

	snap := d.Snapshot()
	content := renderDashboardView(&snap, width, height, IsASCIIOnly(), time.Now())

	var sb strings.Builder
	sb.WriteString("\033[H")
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteString("\033[K\n")
	}
	sb.WriteString("\033[J")
	_, _ = io.WriteString(w, sb.String())
}

// renderDashboardView pure rendering function that builds the entire dashboard frame.
func renderDashboardView(state *WatchState, width, height int, ascii bool, now time.Time) string {
	if width < 40 {
		width = 40
	}
	if height < 10 {
		height = 10
	}

	sepChar := "─"
	colSep := "│"
	if ascii {
		sepChar = "-"
		colSep = "|"
	}

	var sb strings.Builder

	// 1. Header
	scopeLabel := state.Scope
	if state.RepoPath != "" {
		repoName := filepath.Base(state.RepoPath)
		scopeLabel = fmt.Sprintf("%s: %s", state.Scope, repoName)
	}
	leftTitle := fmt.Sprintf("tk watch ── Live Task Dashboard [%s]", scopeLabel)
	clockStr := now.Format("15:04:05")
	wLeft := runewidth.StringWidth(leftTitle)
	wClock := runewidth.StringWidth(clockStr)
	gap := width - wLeft - wClock
	if gap >= 2 {
		sb.WriteString(leftTitle + strings.Repeat(" ", gap) + clockStr + "\n")
	} else {
		maxLeft := width - wClock - 2
		if maxLeft < 10 {
			maxLeft = 10
		}
		sb.WriteString(truncateWithEllipsis(leftTitle, maxLeft) + "  " + clockStr + "\n")
	}

	countsLine := fmt.Sprintf("Active: %d in progress  %s  Backlog: %d open  %s  Total: %d tasks",
		len(state.ActiveTasks), colSep, len(state.BacklogTasks), colSep, state.TotalCount)
	sb.WriteString(truncateWithEllipsis(countsLine, width) + "\n")
	sb.WriteString(strings.Repeat(sepChar, width) + "\n\n")

	// 2. Active Tasks Table (In Progress)
	fmt.Fprintf(&sb, "ACTIVE TASKS (In Progress: %d)\n", len(state.ActiveTasks))
	if len(state.ActiveTasks) == 0 {
		sb.WriteString("  (No tasks currently in progress)\n\n")
	} else {
		writeTaskTable(&sb, state.ActiveTasks, true, width, sepChar, now)
		sb.WriteString("\n")
	}

	// 3. Backlog Tasks Table (Open)
	fmt.Fprintf(&sb, "BACKLOG TASKS (Open: %d)\n", len(state.BacklogTasks))
	if len(state.BacklogTasks) == 0 {
		sb.WriteString("  (No open tasks in backlog)\n\n")
	} else {
		maxBacklog := 5
		if height <= 22 {
			maxBacklog = 3
		} else if height >= 36 {
			maxBacklog = 8
		}

		displayedBacklog := state.BacklogTasks
		overflow := 0
		if len(displayedBacklog) > maxBacklog {
			overflow = len(displayedBacklog) - maxBacklog
			displayedBacklog = displayedBacklog[:maxBacklog]
		}

		writeTaskTable(&sb, displayedBacklog, false, width, sepChar, now)
		if overflow > 0 {
			fmt.Fprintf(&sb, "  ... and %d more open tasks in backlog (run 'tk list --ready' to view all)\n", overflow)
		}
		sb.WriteString("\n")
	}

	// 4. Latest Update Card
	cardStr := renderLatestCard(state.LatestTask, state.LatestTime, width, ascii, now)
	sb.WriteString(cardStr)
	sb.WriteString("\n")

	// 5. Recent Activity Stream
	streamHeader := fmt.Sprintf("── Recent Activity (Ctrl+C to quit) %s", strings.Repeat(sepChar, maxInt(2, width-36)))
	sb.WriteString(truncateWithEllipsis(streamHeader, width) + "\n")
	if len(state.RecentEvents) == 0 {
		sb.WriteString("  (Waiting for real-time task events...)\n")
	} else {
		maxShowEvents := 5
		if height <= 22 {
			maxShowEvents = 3
		} else if height >= 36 {
			maxShowEvents = 7
		}
		startIdx := len(state.RecentEvents) - maxShowEvents
		if startIdx < 0 {
			startIdx = 0
		}
		for i := len(state.RecentEvents) - 1; i >= startIdx; i-- {
			ev := state.RecentEvents[i]
			tStr := ev.Timestamp.Format("15:04:05")
			line := fmt.Sprintf("  %s  %s", tStr, ev.Message)
			sb.WriteString(truncateWithEllipsis(line, width) + "\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func writeTaskTable(sb *strings.Builder, tasks []*domain.Task, isActiveTable bool, width int, sepChar string, now time.Time) {
	idCol := 7
	priCol := 8
	ageCol := 7
	otherCol := 16
	if width < 60 {
		otherCol = 10
	}
	fixed := idCol + priCol + ageCol + otherCol + 4
	titleCol := width - fixed
	if titleCol < 15 {
		titleCol = 15
	}

	otherHeader := "ASSIGNEE"
	if !isActiveTable {
		otherHeader = "TOPIC"
	}

	hdr := fmt.Sprintf("%-7s %-8s %-*s %-*s %7s", "ID", "PRI", titleCol, "TITLE", otherCol, otherHeader, "AGE")
	sb.WriteString(truncateWithEllipsis(hdr, width) + "\n")
	sb.WriteString(truncateWithEllipsis(strings.Repeat(sepChar, runewidth.StringWidth(hdr)), width) + "\n")

	for _, t := range tasks {
		idStr := truncateWithEllipsis(t.ID, idCol)
		priStr := truncateWithEllipsis(string(t.Priority), priCol)
		titleStr := truncateWithEllipsis(t.Title, titleCol)

		var otherVal string
		if isActiveTable {
			otherVal = t.Assignee
			if otherVal == "" {
				otherVal = "-"
			}
		} else {
			if t.TopicPath != "" {
				otherVal = "@" + t.TopicPath
			} else if len(t.Tags) > 0 {
				otherVal = strings.Join(t.Tags, ",")
			} else {
				otherVal = "-"
			}
		}
		otherStr := truncateWithEllipsis(otherVal, otherCol)
		ageStr := timeAgoAt(t.CreatedAt, now)

		row := fmt.Sprintf("%-7s %-8s %-*s %-*s %7s", idStr, priStr, titleCol, titleStr, otherCol, otherStr, ageStr)
		sb.WriteString(truncateWithEllipsis(row, width) + "\n")
	}
}

// renderLatestCard formats the "Latest Update" into an executive bordered card.
func renderLatestCard(t *domain.Task, lastTime time.Time, width int, ascii bool, now time.Time) string {
	cardW := width
	if cardW > 92 {
		cardW = 92
	}
	if cardW < 40 {
		cardW = 40
	}
	innerW := cardW - 4

	tl, tr, bl, br := "┌", "┐", "└", "┘"
	h, v := "─", "│"
	divL, divR := "├", "┤"
	if ascii {
		tl, tr, bl, br = "+", "+", "+", "+"
		h, v = "-", "|"
		divL, divR = "+", "+"
	}

	var sb strings.Builder

	// Top border
	label := " LATEST UPDATE "
	topFillerLen := cardW - 2 - runewidth.StringWidth(label) - 1
	if topFillerLen < 1 {
		topFillerLen = 1
	}
	topBorder := tl + h + label + strings.Repeat(h, topFillerLen) + tr
	sb.WriteString(truncateWithEllipsis(topBorder, cardW) + "\n")

	if t == nil {
		sb.WriteString(renderCardLine("No task activity recorded yet", innerW, v) + "\n")
		bottomBorder := bl + strings.Repeat(h, cardW-2) + br
		sb.WriteString(truncateWithEllipsis(bottomBorder, cardW) + "\n")
		return sb.String()
	}

	// Line 1: ID, Status, and Relative Time
	statusBadge := fmt.Sprintf("%s [%s]", t.ID, strings.ToUpper(string(t.Status)))
	agoBadge := fmt.Sprintf("(%s)", timeAgoAt(lastTime, now))
	gap := innerW - runewidth.StringWidth(statusBadge) - runewidth.StringWidth(agoBadge)
	var line1 string
	if gap >= 2 {
		line1 = statusBadge + strings.Repeat(" ", gap) + agoBadge
	} else {
		line1 = truncateWithEllipsis(statusBadge+" "+agoBadge, innerW)
	}
	sb.WriteString(renderCardLine(line1, innerW, v) + "\n")

	// Line 2: Task Title
	titleLine := "Title: " + t.Title
	sb.WriteString(renderCardLine(titleLine, innerW, v) + "\n")

	// Mid divider
	midDivider := divL + strings.Repeat(h, cardW-2) + divR
	sb.WriteString(truncateWithEllipsis(midDivider, cardW) + "\n")

	// Details block
	if len(t.Notes) > 0 {
		lastNote := t.Notes[len(t.Notes)-1]
		author := lastNote.Author
		if author == "" {
			author = "actor"
		}
		sb.WriteString(renderCardLine(fmt.Sprintf("Note by %s:", author), innerW, v) + "\n")
		wrapped := wrapCardText(lastNote.Content, innerW-4, 4)
		for _, wl := range wrapped {
			sb.WriteString(renderCardLine("  "+wl, innerW, v) + "\n")
		}
		if len(wrapped) == 4 && len(strings.Fields(lastNote.Content)) > 30 {
			sb.WriteString(renderCardLine(fmt.Sprintf("  ... (use 'tk show %s' for full note)", t.ID), innerW, v) + "\n")
		}
	} else if t.CommitSHA != "" {
		sha := t.CommitSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		sb.WriteString(renderCardLine(fmt.Sprintf("Resolved by commit %s:", sha), innerW, v) + "\n")
		wrapped := wrapCardText(t.CommitMsg, innerW-4, 2)
		for _, wl := range wrapped {
			sb.WriteString(renderCardLine("  "+wl, innerW, v) + "\n")
		}
		if t.DiffStat != "" {
			sb.WriteString(renderCardLine("  "+truncateWithEllipsis("Diff: "+t.DiffStat, innerW-4), innerW, v) + "\n")
		}
	} else if t.Assignee != "" {
		sb.WriteString(renderCardLine(fmt.Sprintf("Claimed by %s", t.Assignee), innerW, v) + "\n")
	} else {
		author := t.Author
		if author == "" {
			author = "unknown"
		}
		sb.WriteString(renderCardLine(fmt.Sprintf("Created by %s with priority %s", author, t.Priority), innerW, v) + "\n")
	}

	// Bottom border
	bottomBorder := bl + strings.Repeat(h, cardW-2) + br
	sb.WriteString(truncateWithEllipsis(bottomBorder, cardW) + "\n")

	return sb.String()
}

func renderCardLine(content string, innerW int, v string) string {
	w := runewidth.StringWidth(content)
	if w > innerW {
		content = truncateWithEllipsis(content, innerW)
		w = runewidth.StringWidth(content)
	}
	padding := ""
	if innerW > w {
		padding = strings.Repeat(" ", innerW-w)
	}
	return fmt.Sprintf("%s %s%s %s", v, content, padding, v)
}

// wrapCardText cleanly word-wraps text within maxWidth, capped at maxLines.
func wrapCardText(text string, maxWidth, maxLines int) []string {
	if maxWidth <= 10 {
		maxWidth = 10
	}
	if maxLines <= 0 {
		maxLines = 4
	}

	rawLines := strings.Split(text, "\n")
	var result []string

	for _, rawLine := range rawLines {
		rawLine = strings.TrimSpace(rawLine)
		if rawLine == "" {
			continue
		}

		words := strings.Fields(rawLine)
		if len(words) == 0 {
			continue
		}

		var curLine strings.Builder
		curWidth := 0

		for _, word := range words {
			wWidth := runewidth.StringWidth(word)

			if wWidth > maxWidth {
				if curWidth > 0 {
					result = append(result, curLine.String())
					curLine.Reset()
					curWidth = 0
					if len(result) >= maxLines {
						break
					}
				}
				runes := []rune(word)
				for len(runes) > 0 {
					fitCount := 0
					fitW := 0
					for _, r := range runes {
						rw := runewidth.RuneWidth(r)
						if fitW+rw > maxWidth {
							break
						}
						fitW += rw
						fitCount++
					}
					if fitCount == 0 {
						fitCount = 1
					}
					result = append(result, string(runes[:fitCount]))
					runes = runes[fitCount:]
					if len(result) >= maxLines {
						break
					}
				}
				continue
			}

			if curWidth == 0 {
				curLine.WriteString(word)
				curWidth = wWidth
			} else if curWidth+1+wWidth <= maxWidth {
				curLine.WriteByte(' ')
				curLine.WriteString(word)
				curWidth += 1 + wWidth
			} else {
				result = append(result, curLine.String())
				curLine.Reset()
				if len(result) >= maxLines {
					break
				}
				curLine.WriteString(word)
				curWidth = wWidth
			}
		}

		if curWidth > 0 && len(result) < maxLines {
			result = append(result, curLine.String())
		}
		if len(result) >= maxLines {
			break
		}
	}

	if len(result) > maxLines {
		result = result[:maxLines]
	}
	return result
}

func timeAgo(t time.Time) string {
	return timeAgoAt(t, time.Now())
}

func timeAgoAt(t, now time.Time) string {
	if t.IsZero() {
		return "just now"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Stream real-time task events from daemon (great over SSH)",
	Long: `Connect to the background daemon socket and stream live task lifecycle
events (created, updated, resolved, attached git diffs, deleted). In an interactive
terminal, renders a responsive, real-time updating dashboard. In non-interactive or
piped environments, streams raw event logs or JSON.`,
	Example: `  tk watch
  tk watch --json | jq .`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer func() { _ = taskClient.Close() }()

		// Fetch initial task state including notes for current scope/repo
		filter := domain.TaskFilter{
			Scope:        scope,
			RepoPath:     repoRoot,
			IncludeNotes: true,
		}
		initialTasks, _ := taskClient.ListTasks(filter)
		dashboard := NewWatchDashboard(scope, repoRoot, initialTasks)

		isInteractive := IsInteractiveTerminal() && term.IsTerminal(os.Stdout.Fd()) && !jsonFlag

		if jsonFlag {
			snap := dashboard.Snapshot()
			allTasks := append(snap.ActiveTasks, snap.BacklogTasks...)
			for _, t := range allTasks {
				_ = outputJSON(protocol.Event{
					Type:      protocol.EventSnapshot,
					Timestamp: time.Now(),
					Task:      t,
					Message:   fmt.Sprintf("Active task %s: %s", t.ID, t.Title),
				})
			}
			return taskClient.StreamEvents(func(ev protocol.Event) {
				_ = outputJSON(ev)
			})
		}

		if !isInteractive {
			// Non-interactive linear stream mode (CI, agent runners, dumb terminals, or piped)
			snap := dashboard.Snapshot()
			fmt.Fprintf(os.Stderr, "Active Tasks: %d in progress (%d open in backlog)\n", len(snap.ActiveTasks), len(snap.BacklogTasks))
			if snap.LatestTask != nil {
				fmt.Fprintf(os.Stderr, "Latest Update: %s [%s] %s (%s)\n",
					snap.LatestTask.ID, snap.LatestTask.Status, snap.LatestTask.Title, timeAgo(snap.LatestTime))
			}
			fmt.Fprintln(os.Stderr, "── Streaming live task events (Ctrl+C to quit) ──")

			err := taskClient.StreamEvents(func(ev protocol.Event) {
				ts := ev.Timestamp.Format("15:04:05")
				fmt.Printf("[%s] %s\n", ts, ev.Message)
			})
			if err != nil && (errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") || errors.Is(err, io.EOF)) {
				return nil
			}
			return err
		}

		// Interactive live dashboard mode
		sigCh := make(chan os.Signal, 2)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

		sigwinchCh := make(chan os.Signal, 2)
		notifyWindowResize(sigwinchCh)

		redrawCh := make(chan struct{}, 1)
		requestRedraw := func() {
			select {
			case redrawCh <- struct{}{}:
			default:
			}
		}

		// Initial terminal setup: clear screen and hide cursor
		_, _ = fmt.Fprint(os.Stdout, "\033[2J\033[H\033[?25l")
		defer func() { _, _ = fmt.Fprint(os.Stdout, "\033[?25h\n") }()

		dashboard.Render(os.Stdout)

		// Periodic 1-second ticker to update clock and relative time stamps
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		errCh := make(chan error, 1)
		go func() {
			err := taskClient.StreamEvents(func(ev protocol.Event) {
				dashboard.RecordEvent(ev)
				requestRedraw()
			})
			errCh <- err
		}()

		for {
			select {
			case <-sigCh:
				_ = taskClient.Close()
				return nil
			case <-sigwinchCh:
				requestRedraw()
			case <-ticker.C:
				requestRedraw()
			case <-redrawCh:
				dashboard.Render(os.Stdout)
			case err := <-errCh:
				if err != nil && (errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") || errors.Is(err, io.EOF)) {
					return nil
				}
				return err
			}
		}
	},
}
