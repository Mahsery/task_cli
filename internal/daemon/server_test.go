package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

func TestDaemonArchiveAndUnarchive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-daemon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		GlobalDir:  tmpDir,
		SocketPath: filepath.Join(tmpDir, "tk.sock"),
	}

	srv := NewServer(cfg)
	defer func() {
		srv.mu.Lock()
		for _, db := range srv.dbs {
			db.Close()
		}
		srv.mu.Unlock()
	}()

	repoPath := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(repoPath, 0755)

	// 1. Create a task via daemon
	createPayload, _ := json.Marshal(protocol.CreateTaskPayload{
		Title:       "Daemon Archive Test",
		Description: "testing archive ipc",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityNormal,
	})
	createResp := srv.dispatch(protocol.Request{
		ID:       "req-1",
		Action:   protocol.ActionCreateTask,
		Payload:  createPayload,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !createResp.Success {
		t.Fatalf("create_task failed: %s", createResp.Error)
	}
	var created domain.Task
	_ = json.Unmarshal(createResp.Data, &created)

	// 2. Archive task via ActionArchiveTask
	archivePayload, _ := json.Marshal(protocol.ArchiveTaskPayload{
		ID:     created.ID,
		Actor:  "agent-test",
		Reason: "archiving via daemon",
	})
	archiveResp := srv.dispatch(protocol.Request{
		ID:       "req-2",
		Action:   protocol.ActionArchiveTask,
		Payload:  archivePayload,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !archiveResp.Success {
		t.Fatalf("task.archive failed: %s", archiveResp.Error)
	}
	var archived domain.Task
	_ = json.Unmarshal(archiveResp.Data, &archived)
	if !archived.IsArchived() {
		t.Errorf("expected task to be archived")
	}

	// 3. List tasks (default should not include archived)
	listDefaultPayload, _ := json.Marshal(protocol.ListTasksPayload{
		Scope:    "local",
		RepoPath: repoPath,
	})
	listDefaultResp := srv.dispatch(protocol.Request{
		ID:       "req-3",
		Action:   protocol.ActionListTasks,
		Payload:  listDefaultPayload,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !listDefaultResp.Success {
		t.Fatalf("list_tasks default failed: %s", listDefaultResp.Error)
	}
	var defaultList []*domain.Task
	_ = json.Unmarshal(listDefaultResp.Data, &defaultList)
	if len(defaultList) != 0 {
		t.Errorf("expected 0 tasks in default list, got %d", len(defaultList))
	}

	// 4. List tasks with OnlyArchived
	listArchivedPayload, _ := json.Marshal(protocol.ListTasksPayload{
		Scope:        "local",
		RepoPath:     repoPath,
		OnlyArchived: true,
	})
	listArchivedResp := srv.dispatch(protocol.Request{
		ID:       "req-4",
		Action:   protocol.ActionListTasks,
		Payload:  listArchivedPayload,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !listArchivedResp.Success {
		t.Fatalf("list_tasks only_archived failed: %s", listArchivedResp.Error)
	}
	var archivedList []*domain.Task
	_ = json.Unmarshal(listArchivedResp.Data, &archivedList)
	if len(archivedList) != 1 || archivedList[0].ID != created.ID {
		t.Errorf("expected 1 archived task, got %d", len(archivedList))
	}

	// 5. Unarchive task via ActionUnarchiveTask
	unarchivePayload, _ := json.Marshal(protocol.UnarchiveTaskPayload{
		ID:     created.ID,
		Actor:  "agent-test",
		Reason: "unarchiving via daemon",
	})
	unarchiveResp := srv.dispatch(protocol.Request{
		ID:       "req-5",
		Action:   protocol.ActionUnarchiveTask,
		Payload:  unarchivePayload,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !unarchiveResp.Success {
		t.Fatalf("task.unarchive failed: %s", unarchiveResp.Error)
	}
	var unarchived domain.Task
	_ = json.Unmarshal(unarchiveResp.Data, &unarchived)
	if unarchived.IsArchived() {
		t.Errorf("expected task to not be archived after unarchive")
	}

	// 6. List tasks again (default should now include unarchived task)
	listAfterResp := srv.dispatch(protocol.Request{
		ID:       "req-6",
		Action:   protocol.ActionListTasks,
		Payload:  listDefaultPayload,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !listAfterResp.Success {
		t.Fatalf("list_tasks after unarchive failed: %s", listAfterResp.Error)
	}
	var afterList []*domain.Task
	_ = json.Unmarshal(listAfterResp.Data, &afterList)
	if len(afterList) != 1 || afterList[0].ID != created.ID {
		t.Errorf("expected 1 task after unarchive, got %d", len(afterList))
	}
}
