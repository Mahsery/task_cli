package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"task_cli/internal/client"
	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

func TestStatusCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"status"})
	if err != nil || cmd == nil {
		t.Fatalf("status command not registered: %v", err)
	}

	if cmd.Name() != "status" {
		t.Errorf("expected command name 'status', got %s", cmd.Name())
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("failed to render status help: %v", err)
	}
}

func TestIsTaskOrphaned(t *testing.T) {
	var nilTask *domain.Task
	if isTaskOrphaned(nilTask) {
		t.Error("expected nil task to not be orphaned")
	}

	now := time.Now()

	// 1. Tagged 'orphaned' (not archived)
	tagged := &domain.Task{
		ID:        "T-1",
		Title:     "Tagged Orphaned",
		Status:    domain.StatusOpen,
		Tags:      []string{"orphaned"},
		CreatedAt: now,
	}
	if !isTaskOrphaned(tagged) {
		t.Error("expected task tagged orphaned to be recognized as orphaned")
	}

	// 2. Case-insensitive tag 'Orphaned'
	taggedCase := &domain.Task{
		ID:        "T-2",
		Title:     "Case Tagged Orphaned",
		Status:    domain.StatusOpen,
		Tags:      []string{"Orphaned"},
		CreatedAt: now,
	}
	if !isTaskOrphaned(taggedCase) {
		t.Error("expected task tagged 'Orphaned' to be recognized as orphaned")
	}

	// 3. Tagged 'orphaned' BUT archived -> MUST return false
	archivedTagged := &domain.Task{
		ID:         "T-3",
		Title:      "Archived Tagged",
		Status:     domain.StatusArchived,
		Tags:       []string{"orphaned"},
		ArchivedAt: &now,
		CreatedAt:  now.Add(-2 * time.Hour),
	}
	if isTaskOrphaned(archivedTagged) {
		t.Error("expected archived task tagged 'orphaned' to NOT be considered orphaned")
	}

	// 4. Stale unassigned task older than 1h (not archived)
	staleUnassigned := &domain.Task{
		ID:        "T-4",
		Title:     "Stale Unassigned",
		Status:    domain.StatusOpen,
		Assignee:  "",
		CreatedAt: now.Add(-2 * time.Hour),
	}
	if !isTaskOrphaned(staleUnassigned) {
		t.Error("expected stale unassigned task older than 1h to be recognized as orphaned")
	}

	// 5. Stale unassigned task BUT archived -> MUST return false
	staleArchived := &domain.Task{
		ID:         "T-5",
		Title:      "Stale Archived",
		Status:     domain.StatusArchived,
		Assignee:   "",
		ArchivedAt: &now,
		CreatedAt:  now.Add(-2 * time.Hour),
	}
	if isTaskOrphaned(staleArchived) {
		t.Error("expected stale archived task to NOT be considered orphaned")
	}

	// 6. Recent unassigned task (<1h) -> false
	recentUnassigned := &domain.Task{
		ID:        "T-6",
		Title:     "Recent Unassigned",
		Status:    domain.StatusOpen,
		Assignee:  "",
		CreatedAt: now.Add(-10 * time.Minute),
	}
	if isTaskOrphaned(recentUnassigned) {
		t.Error("expected recent task to not be orphaned")
	}

	// 7. Assigned in-progress task -> false
	assignedInProgress := &domain.Task{
		ID:        "T-7",
		Title:     "Assigned Task",
		Status:    domain.StatusInProgress,
		Assignee:  "alice",
		CreatedAt: now.Add(-2 * time.Hour),
	}
	if isTaskOrphaned(assignedInProgress) {
		t.Error("expected assigned task to not be orphaned")
	}
}

func TestStatusReportAndRendering(t *testing.T) {
	now := time.Now()
	archivedTime := now.Add(-1 * time.Hour)

	tasks := []*domain.Task{
		// Open ready task
		{ID: "T-1", Title: "Open Ready", Status: domain.StatusOpen, Priority: domain.PriorityNormal, CreatedAt: now.Add(-10 * time.Minute)},
		// Open blocked task
		{ID: "T-2", Title: "Open Blocked", Status: domain.StatusOpen, Priority: domain.PriorityHigh, IsBlocked: true, BlockedBy: []string{"T-1"}, CreatedAt: now.Add(-20 * time.Minute)},
		// Active in-progress task
		{ID: "T-3", Title: "In Progress Task", Status: domain.StatusInProgress, Priority: domain.PriorityUrgent, Assignee: "agent-test", CreatedAt: now.Add(-30 * time.Minute)},
		// Done task
		{ID: "T-4", Title: "Completed Task", Status: domain.StatusDone, Priority: domain.PriorityNormal, CreatedAt: now.Add(-2 * time.Hour)},
		// Closed task
		{ID: "T-5", Title: "Closed Task", Status: domain.StatusClosed, Priority: domain.PriorityLow, CreatedAt: now.Add(-3 * time.Hour)},
		// Archived task (StatusArchived)
		{ID: "T-6", Title: "Archived Status", Status: domain.StatusArchived, Priority: domain.PriorityNormal, ArchivedAt: &archivedTime, CreatedAt: now.Add(-5 * time.Hour)},
		// Archived task (ArchivedAt set)
		{ID: "T-7", Title: "Archived Timestamp", Status: domain.StatusDone, Priority: domain.PriorityLow, ArchivedAt: &archivedTime, CreatedAt: now.Add(-6 * time.Hour)},
		// Tagged orphaned task (unarchived)
		{ID: "T-8", Title: "Tagged Orphan", Status: domain.StatusOpen, Priority: domain.PriorityNormal, Tags: []string{"orphaned"}, CreatedAt: now.Add(-40 * time.Minute)},
		// Stale unassigned task (unarchived)
		{ID: "T-9", Title: "Stale Task", Status: domain.StatusOpen, Priority: domain.PriorityHigh, Assignee: "", CreatedAt: now.Add(-2 * time.Hour)},
	}

	var (
		openCount       int
		inProgressCount int
		doneCount       int
		orphanedCount   int
		archivedCount   int
		readyCount      int
		blockedCount    int

		activeTasks   []*domain.Task
		orphanedTasks []*domain.Task
		readyTasks    []*domain.Task
		blockedTasks  []*domain.Task
	)

	for _, t := range tasks {
		if t.IsArchived() {
			archivedCount++
			continue
		}

		if isTaskOrphaned(t) {
			orphanedCount++
			orphanedTasks = append(orphanedTasks, t)
		}

		switch t.Status {
		case domain.StatusInProgress:
			inProgressCount++
			activeTasks = append(activeTasks, t)
		case domain.StatusOpen:
			openCount++
			if t.IsBlocked {
				blockedCount++
				blockedTasks = append(blockedTasks, t)
			} else {
				readyCount++
				readyTasks = append(readyTasks, t)
			}
		case domain.StatusDone, domain.StatusClosed:
			doneCount++
		default:
			openCount++
		}
	}

	report := StatusReport{
		Scope:    "local",
		RepoPath: "/repo/test",
		Branch:   "feature-status",
		Actor:    "agent-test",
		Summary: StatusSummary{
			Total:      len(tasks),
			Open:       openCount,
			InProgress: inProgressCount,
			Done:       doneCount,
			Orphaned:   orphanedCount,
			Archived:   archivedCount,
			Ready:      readyCount,
			Blocked:    blockedCount,
		},
		ActiveTasks:   activeTasks,
		OrphanedTasks: orphanedTasks,
		ReadyTasks:    readyTasks,
		BlockedTasks:  blockedTasks,
	}

	// Verify calculated counts
	if report.Summary.Total != 9 {
		t.Errorf("expected total 9, got %d", report.Summary.Total)
	}
	if report.Summary.Archived != 2 {
		t.Errorf("expected archived 2, got %d", report.Summary.Archived)
	}
	if report.Summary.InProgress != 1 {
		t.Errorf("expected in_progress 1, got %d", report.Summary.InProgress)
	}
	if report.Summary.Done != 2 {
		t.Errorf("expected done 2, got %d", report.Summary.Done)
	}
	if report.Summary.Open != 4 { // T-1, T-2, T-8, T-9
		t.Errorf("expected open 4, got %d", report.Summary.Open)
	}
	if report.Summary.Orphaned != 2 { // T-8, T-9
		t.Errorf("expected orphaned 2, got %d", report.Summary.Orphaned)
	}
	if report.Summary.Ready != 3 { // T-1, T-8, T-9
		t.Errorf("expected ready 3, got %d", report.Summary.Ready)
	}
	if report.Summary.Blocked != 1 { // T-2
		t.Errorf("expected blocked 1, got %d", report.Summary.Blocked)
	}

	// Capture renderStatusText output
	rescueStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	renderStatusText(report)

	w.Close()
	outBytes, _ := io.ReadAll(r)
	os.Stdout = rescueStdout
	out := string(outBytes)

	// Verify sections in output
	sections := []string{
		"Repository Status (local):",
		"Repo:    /repo/test",
		"Branch:  feature-status",
		"Actor:   agent-test",
		"Summary:",
		"Open: 4",
		"Active: 1",
		"Done: 2",
		"Orphaned: 2",
		"Archived: 2",
		"Ready: 3",
		"Blocked: 1",
		"Active Tasks (1):",
		"T-3",
		"assigned to you (agent-test)",
		"Orphaned Tasks (2):",
		"T-8",
		"T-9",
		"Hint: 'tk takeover T-8' to claim, or 'tk archive T-8' to archive",
		"Blocked Tasks (1):",
		"T-2",
		"blocked by: T-1",
	}

	for _, sec := range sections {
		if !strings.Contains(out, sec) {
			t.Errorf("renderStatusText missing expected content: %q\nFull output:\n%s", sec, out)
		}
	}
}

func TestStatusReportJSON(t *testing.T) {
	report := StatusReport{
		Scope:    "local",
		RepoPath: "/test/path",
		Branch:   "main",
		Actor:    "test-actor",
		Summary: StatusSummary{
			Total:      10,
			Open:       2,
			InProgress: 1,
			Done:       5,
			Orphaned:   1,
			Archived:   2,
			Ready:      2,
			Blocked:    0,
		},
		ActiveTasks: []*domain.Task{
			{ID: "T-1", Title: "Active"},
		},
		OrphanedTasks: []*domain.Task{
			{ID: "T-2", Title: "Orphaned"},
		},
		ReadyTasks:   []*domain.Task{},
		BlockedTasks: []*domain.Task{},
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to marshal StatusReport: %v", err)
	}

	var parsed StatusReport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal StatusReport: %v", err)
	}

	if parsed.Scope != "local" || parsed.Summary.Archived != 2 || len(parsed.ActiveTasks) != 1 {
		t.Errorf("unexpected parsed status report: %+v", parsed)
	}
}

func TestStatusExecutionWithMock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-cli-status-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "mock.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on mock socket: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)

				for {
					var req protocol.Request
					if err := dec.Decode(&req); err != nil {
						return
					}

					now := time.Now()
					if req.Action == protocol.ActionListTasks {
						tasks := []*domain.Task{
							{
								ID:        "T-1",
								Title:     "Open Task",
								Status:    domain.StatusOpen,
								Priority:  domain.PriorityNormal,
								CreatedAt: now,
							},
							{
								ID:         "T-2",
								Title:      "Archived Task",
								Status:     domain.StatusArchived,
								Priority:   domain.PriorityLow,
								ArchivedAt: &now,
								CreatedAt:  now.Add(-1 * time.Hour),
							},
						}
						data, _ := json.Marshal(tasks)
						_ = enc.Encode(protocol.Response{
							ID:      req.ID,
							Success: true,
							Data:    data,
						})
					}
				}
			}(conn)
		}
	}()

	origClient := taskClient
	origCfg := cfg
	origScope := scope
	origRepoRoot := repoRoot
	defer func() {
		taskClient = origClient
		cfg = origCfg
		scope = origScope
		repoRoot = origRepoRoot
	}()

	testCfg := &config.Config{
		SocketPath: sockPath,
		Actor:      "test-agent",
	}
	cfg = testCfg
	scope = "local"
	repoRoot = tmpDir
	taskClient = client.New(testCfg, scope, repoRoot)

	// Execute status in text mode
	if err := statusCmd.RunE(statusCmd, []string{}); err != nil {
		t.Fatalf("status command failed in text mode: %v", err)
	}

	// Execute status in JSON mode
	jsonFlag = true
	defer func() { jsonFlag = false }()
	if err := statusCmd.RunE(statusCmd, []string{}); err != nil {
		t.Fatalf("status command failed in json mode: %v", err)
	}
}
