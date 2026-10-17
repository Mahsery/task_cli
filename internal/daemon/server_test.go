package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
	"task_cli/internal/storage/sqlite"
)

func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "tk-daemon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	cfg := &config.Config{
		GlobalDir:  tmpDir,
		SocketPath: filepath.Join(tmpDir, "tk.sock"),
	}
	srv := NewServer(cfg)
	t.Cleanup(func() {
		srv.mu.Lock()
		for _, db := range srv.dbs {
			_ = db.Close()
		}
		srv.mu.Unlock()
		_ = os.RemoveAll(tmpDir)
	})
	repoPath := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(repoPath, 0o755)
	return srv, repoPath
}

func TestOpenDatabaseAfterRepositoryMove(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateTask("local", "/old/repo", "Existing task", "", domain.StatusOpen, domain.PriorityNormal, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openDatabase(dbPath, "local", "/new/repo")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	tasks, err := db.ListTasks(domain.TaskFilter{Scope: "local", RepoPath: "/new/repo"})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("existing task hidden after move: tasks=%v err=%v", tasks, err)
	}
	next, err := db.CreateTask("local", "/new/repo", "New task", "", domain.StatusOpen, domain.PriorityNormal, nil, "", "")
	if err != nil || next.ID != "T-2" {
		t.Fatalf("create after move: task=%v err=%v", next, err)
	}
}

func TestDaemonArchiveAndUnarchive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-daemon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := &config.Config{
		GlobalDir:  tmpDir,
		SocketPath: filepath.Join(tmpDir, "tk.sock"),
	}

	srv := NewServer(cfg)
	defer func() {
		srv.mu.Lock()
		for _, db := range srv.dbs {
			_ = db.Close()
		}
		srv.mu.Unlock()
	}()

	repoPath := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(repoPath, 0o755)

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
	if runtime.GOOS == "windows" {
		t.Skip("Windows SQLite VFS locks open database files against deletion; closed-file auto-restore is tested by TestDaemonCentralAutoRestore")
	}
	tmpDir, err := os.MkdirTemp("", "tk-daemon-ghost-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := &config.Config{
		GlobalDir:  tmpDir,
		SocketPath: filepath.Join(tmpDir, "tk.sock"),
	}

	srv := NewServer(cfg)
	defer func() {
		srv.mu.Lock()
		for _, db := range srv.dbs {
			_ = db.Close()
		}
		srv.mu.Unlock()
	}()

	repoPath := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(repoPath, 0o755)

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
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := &config.Config{
		GlobalDir:  tmpDir,
		SocketPath: filepath.Join(tmpDir, "tk.sock"),
	}

	srv := NewServer(cfg)
	repoPath := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(repoPath, 0o755)

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
		_ = db.Close()
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
			_ = db.Close()
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
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := &config.Config{
		GlobalDir:  tmpDir,
		SocketPath: filepath.Join(tmpDir, "tk.sock"),
	}

	srv := NewServer(cfg)
	defer func() {
		srv.mu.Lock()
		for _, db := range srv.dbs {
			_ = db.Close()
		}
		srv.mu.Unlock()
	}()

	repoPath := filepath.Join(tmpDir, "repo")
	_ = os.MkdirAll(repoPath, 0o755)

	// Create task
	createResp := srv.dispatch(protocol.Request{
		ID:       "req-1",
		Action:   protocol.ActionCreateTask,
		Payload:  json.RawMessage(`{"title":"Task Before Backup","status":"open","priority":"normal"}`),
		Scope:    "local",
		RepoPath: repoPath,
	})
	var t1 domain.Task
	_ = json.Unmarshal(createResp.Data, &t1)

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

	// Verify task still exists in restored database
	getResp := srv.dispatch(protocol.Request{
		ID:       "req-get-after-restore",
		Action:   protocol.ActionGetTask,
		Payload:  json.RawMessage(fmt.Sprintf(`{"id":%q}`, t1.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !getResp.Success {
		t.Fatalf("failed to fetch task after restore: %s", getResp.Error)
	}
}

func TestDaemonBackupRestore_MalformedPayloadReturnsError(t *testing.T) {
	srv, repoPath := setupTestServer(t)

	// Send malformed JSON payload to ActionBackupRestore
	resp := srv.dispatch(protocol.Request{
		ID:       "req-bad-json",
		Action:   protocol.ActionBackupRestore,
		Payload:  json.RawMessage(`{"backup_path":`),
		Scope:    "local",
		RepoPath: repoPath,
	})

	if resp.Success {
		t.Fatalf("expected malformed JSON payload to fail, but got success")
	}
	if !strings.Contains(resp.Error, "invalid restore payload") {
		t.Errorf("expected error message to contain 'invalid restore payload', got: %s", resp.Error)
	}
}

func TestDaemonDependencies_PubSubEventIntegrity(t *testing.T) {
	srv, repoPath := setupTestServer(t)

	// Create two tasks
	c1 := srv.dispatch(protocol.Request{
		ID:       "c1",
		Action:   protocol.ActionCreateTask,
		Payload:  json.RawMessage(`{"title":"Task 1"}`),
		Scope:    "local",
		RepoPath: repoPath,
	})
	var t1 domain.Task
	_ = json.Unmarshal(c1.Data, &t1)

	c2 := srv.dispatch(protocol.Request{
		ID:       "c2",
		Action:   protocol.ActionCreateTask,
		Payload:  json.RawMessage(`{"title":"Task 2"}`),
		Scope:    "local",
		RepoPath: repoPath,
	})
	var t2 domain.Task
	_ = json.Unmarshal(c2.Data, &t2)

	subCh := srv.pubsub.Subscribe()
	defer srv.pubsub.Unsubscribe(subCh)

	// Link T-2 to depend on T-1
	linkResp := srv.dispatch(protocol.Request{
		ID:       "link-1",
		Action:   protocol.ActionAddDependency,
		Payload:  json.RawMessage(fmt.Sprintf(`{"task_id":%q,"depends_on_id":%q}`, t2.ID, t1.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !linkResp.Success {
		t.Fatalf("link failed: %s", linkResp.Error)
	}

	select {
	case ev := <-subCh:
		if ev.Type != protocol.EventTaskUpdated {
			t.Errorf("expected EventTaskUpdated, got %s", ev.Type)
		}
		if ev.Task == nil {
			t.Fatalf("expected non-nil Task in event, got nil")
		}
		if ev.Task.ID != t2.ID {
			t.Errorf("expected event task ID %s, got %s", t2.ID, ev.Task.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for link event")
	}

	// Unlink T-2 from T-1
	unlinkResp := srv.dispatch(protocol.Request{
		ID:       "unlink-1",
		Action:   protocol.ActionRemoveDependency,
		Payload:  json.RawMessage(fmt.Sprintf(`{"task_id":%q,"depends_on_id":%q}`, t2.ID, t1.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !unlinkResp.Success {
		t.Fatalf("unlink failed: %s", unlinkResp.Error)
	}

	select {
	case ev := <-subCh:
		if ev.Type != protocol.EventTaskUpdated {
			t.Errorf("expected EventTaskUpdated, got %s", ev.Type)
		}
		if ev.Task == nil {
			t.Fatalf("expected non-nil Task in event, got nil")
		}
		if ev.Task.ID != t2.ID {
			t.Errorf("expected event task ID %s, got %s", t2.ID, ev.Task.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for unlink event")
	}
}

func TestDaemonReviewAndCloseGate(t *testing.T) {
	srv, repoPath := setupTestServer(t)

	// 1. Create T-1, T-2 (unrelated), and T-3 (depends on T-1)
	c1 := srv.dispatch(protocol.Request{
		ID:       "c1",
		Action:   protocol.ActionCreateTask,
		Payload:  json.RawMessage(`{"title":"Task 1","reviewer":"alice"}`),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !c1.Success {
		t.Fatalf("create T-1 failed: %s", c1.Error)
	}
	var t1 domain.Task
	_ = json.Unmarshal(c1.Data, &t1)

	c2 := srv.dispatch(protocol.Request{
		ID:       "c2",
		Action:   protocol.ActionCreateTask,
		Payload:  json.RawMessage(`{"title":"Task 2 (Unrelated)"}`),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !c2.Success {
		t.Fatalf("create T-2 failed: %s", c2.Error)
	}
	var t2 domain.Task
	_ = json.Unmarshal(c2.Data, &t2)

	c3 := srv.dispatch(protocol.Request{
		ID:       "c3",
		Action:   protocol.ActionCreateTask,
		Payload:  json.RawMessage(fmt.Sprintf(`{"title":"Task 3 (Depends on T-1)","depends_on":[%q]}`, t1.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !c3.Success {
		t.Fatalf("create T-3 failed: %s", c3.Error)
	}
	var t3 domain.Task
	_ = json.Unmarshal(c3.Data, &t3)

	// 2. Attempting to mark T-1 done via ActionAttachGit without review must fail
	doneResp1 := srv.dispatch(protocol.Request{
		ID:       "done-1",
		Action:   protocol.ActionAttachGit,
		Payload:  json.RawMessage(fmt.Sprintf(`{"task_id":%q,"mark_done":true}`, t1.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if doneResp1.Success {
		t.Fatalf("expected ActionAttachGit mark_done on unreviewed T-1 to fail")
	}

	// 3. Review unrelated T-2 without a note (optional note!), then close T-2 while T-1 is open -> MUST SUCCEED!
	revResp2 := srv.dispatch(protocol.Request{
		ID:       "rev-2",
		Action:   protocol.ActionReviewTask,
		Payload:  json.RawMessage(fmt.Sprintf(`{"id":%q,"reviewer":"bob","author":"bob"}`, t2.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !revResp2.Success {
		t.Fatalf("ActionReviewTask T-2 failed: %s", revResp2.Error)
	}
	doneResp2OK := srv.dispatch(protocol.Request{
		ID:       "done-2-ok",
		Action:   protocol.ActionAttachGit,
		Payload:  json.RawMessage(fmt.Sprintf(`{"task_id":%q,"mark_done":true}`, t2.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !doneResp2OK.Success {
		t.Fatalf("expected closing reviewed unrelated T-2 while T-1 is open to succeed, got: %s", doneResp2OK.Error)
	}

	// 4. Review connected T-3, then try to close T-3 while prerequisite T-1 is open -> must fail!
	revResp3 := srv.dispatch(protocol.Request{
		ID:       "rev-3",
		Action:   protocol.ActionReviewTask,
		Payload:  json.RawMessage(fmt.Sprintf(`{"id":%q,"reviewer":"carol","note":"LGTM T-3","author":"carol"}`, t3.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !revResp3.Success {
		t.Fatalf("ActionReviewTask T-3 failed: %s", revResp3.Error)
	}
	doneResp3Early := srv.dispatch(protocol.Request{
		ID:       "done-3-early",
		Action:   protocol.ActionAttachGit,
		Payload:  json.RawMessage(fmt.Sprintf(`{"task_id":%q,"mark_done":true}`, t3.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if doneResp3Early.Success || !strings.Contains(doneResp3Early.Error, "blocked by unfinished prerequisite task(s): T-1") {
		t.Fatalf("expected closing connected T-3 before prerequisite T-1 to fail, got success=%v err=%q", doneResp3Early.Success, doneResp3Early.Error)
	}

	// 5. Review T-1, close T-1, then close T-3 -> both succeed!
	revResp1 := srv.dispatch(protocol.Request{
		ID:       "rev-1",
		Action:   protocol.ActionReviewTask,
		Payload:  json.RawMessage(fmt.Sprintf(`{"id":%q,"note":"Approved T-1","author":"alice"}`, t1.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !revResp1.Success {
		t.Fatalf("ActionReviewTask T-1 failed: %s", revResp1.Error)
	}
	doneResp1OK := srv.dispatch(protocol.Request{
		ID:       "done-1-ok",
		Action:   protocol.ActionAttachGit,
		Payload:  json.RawMessage(fmt.Sprintf(`{"task_id":%q,"mark_done":true}`, t1.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !doneResp1OK.Success {
		t.Fatalf("expected closing reviewed T-1 to succeed, got: %s", doneResp1OK.Error)
	}
	doneResp3OK := srv.dispatch(protocol.Request{
		ID:       "done-3-ok",
		Action:   protocol.ActionAttachGit,
		Payload:  json.RawMessage(fmt.Sprintf(`{"task_id":%q,"mark_done":true}`, t3.ID)),
		Scope:    "local",
		RepoPath: repoPath,
	})
	if !doneResp3OK.Success {
		t.Fatalf("expected closing reviewed T-3 after T-1 is closed to succeed, got: %s", doneResp3OK.Error)
	}
}
