package daemon

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

// TestDaemonListTasksMultiStatus checks that the statuses field of the
// list_tasks payload is honoured end-to-end by the daemon and storage.
func TestDaemonListTasksMultiStatus(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{GlobalDir: tmpDir, SocketPath: filepath.Join(tmpDir, "tk.sock")}
	srv := NewServer(cfg)
	defer func() {
		srv.mu.Lock()
		for _, db := range srv.dbs {
			_ = db.Close()
		}
		srv.mu.Unlock()
	}()
	repoPath := filepath.Join(tmpDir, "repo")

	titleByStatus := map[domain.Status]string{}
	for i, st := range []domain.Status{domain.StatusOpen, domain.StatusInProgress, domain.StatusDone, domain.StatusClosed} {
		title := "task-" + string(st)
		titleByStatus[st] = title
		payload, err := json.Marshal(protocol.CreateTaskPayload{Title: title, Status: st, Priority: domain.PriorityNormal})
		if err != nil {
			t.Fatal(err)
		}
		resp := srv.dispatch(protocol.Request{ID: "c" + string(rune('0'+i)), Action: protocol.ActionCreateTask, Payload: payload, Scope: "local", RepoPath: repoPath})
		if !resp.Success {
			t.Fatalf("create %s: %s", st, resp.Error)
		}
	}

	list := func(statuses ...domain.Status) []string {
		t.Helper()
		payload, err := json.Marshal(protocol.ListTasksPayload{Scope: "local", RepoPath: repoPath, Statuses: statuses})
		if err != nil {
			t.Fatal(err)
		}
		resp := srv.dispatch(protocol.Request{ID: "l", Action: protocol.ActionListTasks, Payload: payload, Scope: "local", RepoPath: repoPath})
		if !resp.Success {
			t.Fatalf("list: %s", resp.Error)
		}
		var tasks []*domain.Task
		if err := json.Unmarshal(resp.Data, &tasks); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var titles []string
		for _, task := range tasks {
			titles = append(titles, task.Title)
		}
		sort.Strings(titles)
		return titles
	}

	got := list(domain.StatusOpen, domain.StatusInProgress)
	want := []string{titleByStatus[domain.StatusInProgress], titleByStatus[domain.StatusOpen]}
	sort.Strings(want)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("open,in_progress: got %v, want %v", got, want)
	}

	if got := list(); len(got) != 4 {
		t.Fatalf("no status filter: expected 4 tasks, got %v", got)
	}
}
