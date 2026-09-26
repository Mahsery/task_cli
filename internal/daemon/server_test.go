package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
	"task_cli/internal/storage/sqlite"
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

func TestDaemonGhostAutoResurrection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-daemon-ghost-*")
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
		Title:       "Ghost Task",
		Description: "testing ghost db resurrection",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityHigh,
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

	localDBPath := cfg.LocalDBPath(repoPath)
	if _, err := os.Stat(localDBPath); err != nil {
		t.Fatalf("expected local db to exist: %v", err)
	}

	// 2. Simulate user/script running `rm -rf .tasks` while daemon holds it open
	tasksDir := filepath.Dir(localDBPath)
	if err := os.RemoveAll(tasksDir); err != nil {
		t.Fatalf("failed to remove .tasks directory: %v", err)
	}
	if _, err := os.Stat(localDBPath); !os.IsNotExist(err) {
		t.Fatalf("expected local db to be deleted on disk")
	}

	// 3. Make next call to daemon. getDB must detect unlinked file and auto-resurrect!
	listPayload, _ := json.Marshal(protocol.ListTasksPayload{
		Scope:    "local",
		RepoPath: repoPath,
	})
	listResp := srv.dispatch(protocol.Request{
		ID:       "req-2",
		Action:   protocol.ActionListTasks,
		Payload:  listPayload,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !listResp.Success {
		t.Fatalf("list_tasks after unlinking failed: %s", listResp.Error)
	}

	var tasks []*domain.Task
	_ = json.Unmarshal(listResp.Data, &tasks)
	if len(tasks) != 1 || tasks[0].Title != "Ghost Task" {
		t.Fatalf("expected 1 task 'Ghost Task', got %+v", tasks)
	}

	// 4. Verify local db was physically resurrected on disk
	stat, err := os.Stat(localDBPath)
	if err != nil || stat.Size() == 0 {
		t.Fatalf("expected resurrected local db on disk with non-zero size, got stat: %v", err)
	}
}

func TestDaemonCentralAutoRestore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-daemon-central-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		GlobalDir:  tmpDir,
		SocketPath: filepath.Join(tmpDir, "tk.sock"),
	}

	srv := NewServer(cfg)
	repoPath := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(repoPath, 0755)

	// 1. Create a task via daemon (this triggers backup sync to central vault)
	createPayload, _ := json.Marshal(protocol.CreateTaskPayload{
		Title:       "Central Vault Task",
		Description: "should be auto-restored",
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

	// Explicitly create a backup in both local and central
	backupResp := srv.dispatch(protocol.Request{
		ID:       "req-backup",
		Action:   protocol.ActionBackupCreate,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !backupResp.Success {
		t.Fatalf("backup_create failed: %s", backupResp.Error)
	}

	// Verify central backup vault has a snapshot
	centralDir := cfg.CentralBackupDir(repoPath)
	centralBackups, err := sqlite.ListBackupsInDir(centralDir, "central")
	if err != nil || len(centralBackups) == 0 {
		t.Fatalf("expected central backups in %s, got err=%v len=%d", centralDir, err, len(centralBackups))
	}

	// 2. Shut down daemon / drop DB cache
	srv.mu.Lock()
	for _, db := range srv.dbs {
		db.Close()
	}
	srv.dbs = make(map[string]*sqlite.DB)
	srv.mu.Unlock()

	// 3. Delete local .tasks completely from disk (simulating reboot or power loss)
	localDBPath := cfg.LocalDBPath(repoPath)
	_ = os.RemoveAll(filepath.Dir(localDBPath))
	if _, err := os.Stat(localDBPath); !os.IsNotExist(err) {
		t.Fatalf("expected local db to not exist on disk")
	}

	// 4. Create a fresh daemon server instance
	freshSrv := NewServer(cfg)
	defer func() {
		freshSrv.mu.Lock()
		for _, db := range freshSrv.dbs {
			db.Close()
		}
		freshSrv.mu.Unlock()
	}()

	// 5. Query tasks: daemon should auto-restore from central vault!
	listPayload, _ := json.Marshal(protocol.ListTasksPayload{
		Scope:    "local",
		RepoPath: repoPath,
	})
	listResp := freshSrv.dispatch(protocol.Request{
		ID:       "req-2",
		Action:   protocol.ActionListTasks,
		Payload:  listPayload,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !listResp.Success {
		t.Fatalf("list_tasks failed: %s", listResp.Error)
	}

	var tasks []*domain.Task
	_ = json.Unmarshal(listResp.Data, &tasks)
	if len(tasks) != 1 || tasks[0].Title != "Central Vault Task" {
		t.Fatalf("expected 1 task 'Central Vault Task' auto-restored from central, got %+v", tasks)
	}

	// 6. Verify local db was recreated on disk
	if stat, err := os.Stat(localDBPath); err != nil || stat.Size() == 0 {
		t.Fatalf("expected auto-restored db on disk, got stat: %v", err)
	}
}

func TestDaemonBackupListAndRestoreLatest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-daemon-backup-*")
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

	// Create task
	srv.dispatch(protocol.Request{
		ID:       "req-1",
		Action:   protocol.ActionCreateTask,
		Payload:  json.RawMessage(`{"title":"Task Before Backup","status":"open","priority":"normal"}`),
		Scope:    "local",
		RepoPath: repoPath,
	})

	// Create backup
	srv.dispatch(protocol.Request{
		ID:       "req-backup",
		Action:   protocol.ActionBackupCreate,
		Scope:    "local",
		RepoPath: repoPath,
	})

	// List backups: should show both local and central
	listResp := srv.dispatch(protocol.Request{
		ID:       "req-list",
		Action:   protocol.ActionBackupList,
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !listResp.Success {
		t.Fatalf("backup_list failed: %s", listResp.Error)
	}
	var backups []protocol.BackupInfo
	_ = json.Unmarshal(listResp.Data, &backups)
	if len(backups) == 0 {
		t.Fatalf("expected backups to be listed")
	}

	hasLocal, hasCentral := false, false
	for _, b := range backups {
		if b.Location == "local" {
			hasLocal = true
		}
		if b.Location == "central" {
			hasCentral = true
		}
	}
	if !hasLocal || !hasCentral {
		t.Errorf("expected both local and central backups in list, got hasLocal=%v, hasCentral=%v", hasLocal, hasCentral)
	}

	// Restore with "latest"
	restoreResp := srv.dispatch(protocol.Request{
		ID:       "req-restore",
		Action:   protocol.ActionBackupRestore,
		Payload:  json.RawMessage(`{"backup_path":"latest"}`),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !restoreResp.Success {
		t.Fatalf("restore latest failed: %s", restoreResp.Error)
	}
}

