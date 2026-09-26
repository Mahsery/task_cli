package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
	"task_cli/internal/storage/sqlite"
)

type Server struct {
	cfg      *config.Config
	pubsub   *PubSub
	mu       sync.Mutex
	dbs      map[string]*sqlite.DB
	unixLn   net.Listener
	tcpLn    net.Listener
	stopCh   chan struct{}
	shutdown bool
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:    cfg,
		pubsub: NewPubSub(),
		dbs:    make(map[string]*sqlite.DB),
		stopCh: make(chan struct{}),
	}
}

func (s *Server) getDB(scope, repoPath string) (*sqlite.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shutdown {
		return nil, errors.New("server is shutting down")
	}

	var dbPath string
	if scope == "global" || repoPath == "" {
		dbPath = s.cfg.GlobalDBPath()
	} else {
		dbPath = s.cfg.LocalDBPath(repoPath)
	}

	if db, exists := s.dbs[dbPath]; exists {
		// Check if file was unlinked/deleted on disk while held open in memory
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			log.Printf("[tkd] Database %s was deleted on disk while active in daemon memory; auto-resurrecting...", dbPath)
			if rErr := db.Resurrect(dbPath); rErr != nil {
				log.Printf("[tkd] Failed to resurrect %s: %v", dbPath, rErr)
			} else {
				log.Printf("[tkd] Successfully resurrected %s on disk", dbPath)
				go s.syncCentralBackup(db, repoPath)
			}
		}
		return db, nil
	}

	// Database is not currently in memory. Check if missing on disk.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		centralDir := s.cfg.CentralBackupDir(repoPath)
		latestBackup := findLatestBackupInDir(centralDir)
		if latestBackup != "" {
			log.Printf("[tkd] Database %s missing on disk, found central backup %s; auto-restoring...", dbPath, latestBackup)
			_ = os.MkdirAll(filepath.Dir(dbPath), 0755)
			if err := copyFile(latestBackup, dbPath); err == nil {
				log.Printf("[tkd] Successfully auto-restored %s from central backup", dbPath)
			} else {
				log.Printf("[tkd] Warning: failed to copy central backup %s to %s: %v", latestBackup, dbPath, err)
			}
		}
	}

	db, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %q: %w", dbPath, err)
	}

	s.dbs[dbPath] = db
	go s.syncCentralBackup(db, repoPath)
	return db, nil
}

func findLatestBackupInDir(dir string) string {
	backups, err := sqlite.ListBackupsInDir(dir, "central")
	if err != nil || len(backups) == 0 {
		return ""
	}
	return backups[0].Path
}

func (s *Server) syncCentralBackup(db *sqlite.DB, repoPath string) {
	if db == nil {
		return
	}
	centralDir := s.cfg.CentralBackupDir(repoPath)
	_ = os.MkdirAll(centralDir, 0755)
	s.ensureCentralRepoMeta(centralDir, repoPath)
	_, _ = db.CreateRollingBackupInDir(centralDir, "", 10)
}

func (s *Server) ensureCentralRepoMeta(centralDir, repoPath string) {
	if repoPath == "" {
		return
	}
	metaFile := filepath.Join(centralDir, "meta.json")
	if _, err := os.Stat(metaFile); err == nil {
		return
	}
	meta := map[string]string{
		"repo_path":  repoPath,
		"created_at": time.Now().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err == nil {
		_ = os.WriteFile(metaFile, data, 0644)
	}
}


func (s *Server) Start() error {
	// 1. Prepare unix socket
	_ = os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0755)

	// Test if existing socket is active
	if conn, err := net.Dial("unix", s.cfg.SocketPath); err == nil {
		conn.Close()
		return fmt.Errorf("daemon is already running on %s", s.cfg.SocketPath)
	}
	_ = os.Remove(s.cfg.SocketPath)

	unixLn, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket %s: %w", s.cfg.SocketPath, err)
	}
	s.unixLn = unixLn
	_ = os.Chmod(s.cfg.SocketPath, 0600)

	log.Printf("[tkd] Listening on unix socket %s", s.cfg.SocketPath)

	// 2. Optional TCP socket (e.g. for remote SSH forwarding)
	if s.cfg.TCPAddr != "" {
		tcpLn, err := net.Listen("tcp", s.cfg.TCPAddr)
		if err != nil {
			log.Printf("[tkd] Warning: failed to listen on TCP %s: %v", s.cfg.TCPAddr, err)
		} else {
			s.tcpLn = tcpLn
			log.Printf("[tkd] Listening on TCP %s", s.cfg.TCPAddr)
			go s.acceptLoop(tcpLn)
		}
	}

	// 3. Graceful signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[tkd] Received shutdown signal...")
		s.Stop()
	}()

	// 4. Background maintenance (idle WAL checkpoints to minimize disk footprint and SSD churn)
	go s.maintenanceLoop()

	return s.acceptLoop(unixLn)
}

func (s *Server) maintenanceLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			type dbEntry struct {
				db       *sqlite.DB
				repoPath string
			}
			var entries []dbEntry
			for path, db := range s.dbs {
				repoPath := ""
				if path != s.cfg.GlobalDBPath() {
					repoPath = filepath.Dir(filepath.Dir(path))
				}
				entries = append(entries, dbEntry{db: db, repoPath: repoPath})
			}
			s.mu.Unlock()

			for _, e := range entries {
				_ = e.db.Checkpoint("PASSIVE")
				s.syncCentralBackup(e.db, e.repoPath)
			}
		}
	}
}

func (s *Server) Stop() {
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return
	}
	s.shutdown = true
	s.mu.Unlock()

	close(s.stopCh)
	if s.unixLn != nil {
		s.unixLn.Close()
	}
	if s.tcpLn != nil {
		s.tcpLn.Close()
	}
	_ = os.Remove(s.cfg.SocketPath)

	s.mu.Lock()
	for path, db := range s.dbs {
		log.Printf("[tkd] Closing database: %s", path)
		_ = db.Close()
	}
	s.mu.Unlock()

	log.Println("[tkd] Daemon stopped.")
}

func (s *Server) acceptLoop(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return nil
			default:
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
				log.Printf("[tkd] Accept error: %v", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req protocol.Request
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("[tkd] Decode error: %v", err)
			return
		}

		// Handle streaming events action separately
		if req.Action == protocol.ActionStreamEvents {
			s.handleStreamEvents(conn, enc, req)
			return
		}

		resp := s.dispatch(req)
		if err := enc.Encode(resp); err != nil {
			log.Printf("[tkd] Encode error: %v", err)
			return
		}

		if req.Action == protocol.ActionShutdown {
			go func() {
				time.Sleep(100 * time.Millisecond)
				s.Stop()
				os.Exit(0)
			}()
			return
		}
	}
}

func (s *Server) dispatch(req protocol.Request) protocol.Response {
	resp := s.executeDispatch(req)
	if resp.Success {
		switch req.Action {
		case protocol.ActionCreateTask, protocol.ActionUpdateTask, protocol.ActionClaimTask,
			protocol.ActionDeleteTask, protocol.ActionRestoreTask, protocol.ActionAttachGit,
			protocol.ActionAddDependency, protocol.ActionRemoveDependency, protocol.ActionOrphanTask,
			protocol.ActionReapStaleTasks, protocol.ActionTransferTask, protocol.ActionArchiveTask,
			protocol.ActionUnarchiveTask:
			if db, err := s.getDB(req.Scope, req.RepoPath); err == nil {
				go s.syncCentralBackup(db, req.RepoPath)
			}
		}
	}
	return resp
}

func (s *Server) executeDispatch(req protocol.Request) protocol.Response {
	resp := protocol.Response{
		ID:      req.ID,
		Success: true,
	}

	switch req.Action {
	case protocol.ActionPing:
		resp.Data = json.RawMessage(`{"status":"pong"}`)
		return resp

	case protocol.ActionCreateTask:
		var p protocol.CreateTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		task, err := db.CreateTask(req.Scope, req.RepoPath, p.Title, p.Description, p.Status, p.Priority, p.Tags, p.Author, p.Assignee, p.DependsOn...)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskCreated,
			Timestamp: time.Now(),
			Task:      task,
			Message:   fmt.Sprintf("+ %s created: %q", task.ID, task.Title),
		})

		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionGetTask:
		var p protocol.GetTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		task, err := db.GetTask(p.ID)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionUpdateTask:
		var p protocol.UpdateTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		// Hard completion enforcement: cannot mark done/closed if unfinished dependencies exist
		if p.Status != nil && (*p.Status == domain.StatusDone || *p.Status == domain.StatusClosed) && !p.Force {
			if blocked, prereqs, err := db.CheckBlocked(p.ID); err == nil && blocked {
				var titles []string
				for _, pr := range prereqs {
					titles = append(titles, fmt.Sprintf("%s [%s] %q", pr.ID, pr.Status, pr.Title))
				}
				return errorResp(req.ID, fmt.Sprintf("cannot mark %s as %s: blocked by %d unfinished task(s):\n  • %s\n(use --force to override)", p.ID, *p.Status, len(prereqs), strings.Join(titles, "\n  • ")))
			}
		}

		task, err := db.UpdateTask(p.ID, p.Title, p.Description, p.Status, p.Priority, p.Tags, p.AddTags, p.RemoveTags, p.Assignee, p.AddNote, p.Author)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskUpdated,
			Timestamp: time.Now(),
			Task:      task,
			Message:   fmt.Sprintf("~ %s updated (status: %s)", task.ID, task.Status),
		})

		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionClaimTask:
		var p protocol.ClaimTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		task, err := db.ClaimTask(p.ID, p.Assignee, p.Force)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskUpdated,
			Timestamp: time.Now(),
			Task:      task,
			Message:   fmt.Sprintf("* %s claimed by %q", task.ID, p.Assignee),
		})

		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionOrphanTask:
		var p protocol.OrphanTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		task, err := db.OrphanTask(p.ID, p.Actor, p.Reason)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskUpdated,
			Timestamp: time.Now(),
			Task:      task,
			Message:   fmt.Sprintf("⚠ %s orphaned (dropped assignee, status: open)", task.ID),
		})

		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionTransferTask:
		var p protocol.TransferTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		task, err := db.TransferTask(p.ID, p.Actor, p.NewAssignee, p.Note, p.Force)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskUpdated,
			Timestamp: time.Now(),
			Task:      task,
			Message:   fmt.Sprintf("* %s transferred to %q", task.ID, p.NewAssignee),
		})

		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionArchiveTask:
		var p protocol.ArchiveTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		task, err := db.ArchiveTask(p.ID, p.Actor, p.Reason)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskUpdated,
			Timestamp: time.Now(),
			Task:      task,
			Message:   fmt.Sprintf("⊘ %s archived", task.ID),
		})

		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionUnarchiveTask:
		var p protocol.UnarchiveTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		task, err := db.UnarchiveTask(p.ID, p.Actor, p.Reason)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskUpdated,
			Timestamp: time.Now(),
			Task:      task,
			Message:   fmt.Sprintf("↺ %s unarchived", task.ID),
		})

		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionReapStaleTasks:
		var p protocol.ReapStaleTasksPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		candidates, orphanedIDs, err := db.ReapStaleTasks(req.Scope, req.RepoPath, p.Threshold, p.DryRun, p.Actor, p.Reason)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		for _, oID := range orphanedIDs {
			if t, err := db.GetTask(oID); err == nil {
				s.pubsub.Publish(protocol.Event{
					Type:      protocol.EventTaskUpdated,
					Timestamp: time.Now(),
					Task:      t,
					Message:   fmt.Sprintf("⚠ %s orphaned due to inactivity", t.ID),
				})
			}
		}

		result := protocol.ReapStaleTasksResult{
			Threshold:  p.Threshold,
			DryRun:     p.DryRun,
			Candidates: candidates,
			Orphaned:   orphanedIDs,
		}
		data, _ := json.Marshal(result)
		resp.Data = data
		return resp

	case protocol.ActionListTasks:
		var p protocol.ListTasksPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(p.Scope, p.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		filter := domain.TaskFilter{
			Scope:           p.Scope,
			RepoPath:        p.RepoPath,
			Status:          p.Status,
			Priority:        p.Priority,
			Tag:             p.Tag,
			Assignee:        p.Assignee,
			Search:          p.Search,
			Sort:            p.Sort,
			Limit:           p.Limit,
			IncludeNotes:    p.IncludeNotes,
			OnlyReady:       p.OnlyReady,
			OnlyBlocked:     p.OnlyBlocked,
			OnlyOrphaned:    p.OnlyOrphaned,
			IncludeArchived: p.IncludeArchived,
			OnlyArchived:    p.OnlyArchived,
			DependsOn:       p.DependsOn,
			Blocks:          p.Blocks,
		}
		tasks, err := db.ListTasks(filter)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		data, _ := json.Marshal(tasks)
		resp.Data = data
		return resp

	case protocol.ActionDeleteTask:
		var p protocol.DeleteTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		if err := db.DeleteTask(p.ID); err != nil {
			return errorResp(req.ID, err.Error())
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskDeleted,
			Timestamp: time.Now(),
			Task:      &domain.Task{ID: p.ID},
			Message:   fmt.Sprintf("- %s deleted", p.ID),
		})

		resp.Data = json.RawMessage(`{"status":"deleted"}`)
		return resp

	case protocol.ActionRestoreTask:
		var p protocol.RestoreTaskPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		var restored *domain.Task
		if p.ID != "" {
			restored, err = db.RestoreTaskByID(p.ID)
			if err != nil {
				return errorResp(req.ID, err.Error())
			}
		} else if p.Task != nil {
			if err := db.RestoreTask(p.Task); err != nil {
				return errorResp(req.ID, err.Error())
			}
			restored = p.Task
		} else {
			return errorResp(req.ID, "either id or task must be provided for restore")
		}

		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskCreated,
			Timestamp: time.Now(),
			Task:      restored,
			Message:   fmt.Sprintf("+ %s restored: %q", restored.ID, restored.Title),
		})

		data, _ := json.Marshal(restored)
		resp.Data = data
		return resp

	case protocol.ActionListTombstones:
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		tombstones, err := db.ListTombstones(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		data, _ := json.Marshal(tombstones)
		resp.Data = data
		return resp

	case protocol.ActionAddDependency:
		var p protocol.AddDependencyPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		if err := db.AddDependency(p.TaskID, p.DependsOnID); err != nil {
			return errorResp(req.ID, err.Error())
		}

		updatedTask, _ := db.GetTask(p.TaskID)
		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskUpdated,
			Timestamp: time.Now(),
			Task:      updatedTask,
			Message:   fmt.Sprintf("🔗 %s linked to depend on %s", p.TaskID, p.DependsOnID),
		})

		resp.Data = json.RawMessage(`{"status":"linked"}`)
		return resp

	case protocol.ActionRemoveDependency:
		var p protocol.RemoveDependencyPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		if err := db.RemoveDependency(p.TaskID, p.DependsOnID); err != nil {
			return errorResp(req.ID, err.Error())
		}

		updatedTask, _ := db.GetTask(p.TaskID)
		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventTaskUpdated,
			Timestamp: time.Now(),
			Task:      updatedTask,
			Message:   fmt.Sprintf("🔓 %s unlinked from %s", p.TaskID, p.DependsOnID),
		})

		resp.Data = json.RawMessage(`{"status":"unlinked"}`)
		return resp

	case protocol.ActionGetDependencies:
		var p protocol.GetDependenciesPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		task, err := db.GetTask(p.TaskID)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		data, _ := json.Marshal(protocol.DependenciesData{
			TaskID:    task.ID,
			DependsOn: task.DependsOn,
			BlockedBy: task.BlockedBy,
			Blocks:    task.Blocks,
			IsBlocked: task.IsBlocked,
		})
		resp.Data = data
		return resp

	case protocol.ActionAttachGit:
		var p protocol.AttachGitPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		// Hard completion enforcement: cannot mark done if unfinished dependencies exist
		if p.MarkDone && !p.Force {
			if blocked, prereqs, err := db.CheckBlocked(p.TaskID); err == nil && blocked {
				var titles []string
				for _, pr := range prereqs {
					titles = append(titles, fmt.Sprintf("%s [%s] %q", pr.ID, pr.Status, pr.Title))
				}
				return errorResp(req.ID, fmt.Sprintf("cannot mark %s as DONE: blocked by %d unfinished task(s):\n  • %s\n(use --force to override)", p.TaskID, len(prereqs), strings.Join(titles, "\n  • ")))
			}
		}

		task, err := db.AttachGit(p.TaskID, p.CommitSHA, p.CommitMsg, p.DiffStat, p.DiffContent, p.MarkDone)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}

		msg := fmt.Sprintf("+ %s git info attached (%s)", task.ID, p.CommitSHA)
		if p.MarkDone {
			msg = fmt.Sprintf("✓ %s marked DONE (+ %s)", task.ID, p.CommitSHA)
		}
		s.pubsub.Publish(protocol.Event{
			Type:      protocol.EventGitAttached,
			Timestamp: time.Now(),
			Task:      task,
			Message:   msg,
		})

		data, _ := json.Marshal(task)
		resp.Data = data
		return resp

	case protocol.ActionShutdown:
		resp.Data = json.RawMessage(`{"status":"shutting_down"}`)
		return resp

	case protocol.ActionBackupCreate:
		return s.handleBackupCreate(req)

	case protocol.ActionBackupList:
		return s.handleBackupList(req)

	case protocol.ActionBackupRestore:
		return s.handleBackupRestore(req)

	case protocol.ActionBackupVerify:
		return s.handleBackupVerify(req)

	default:
		return errorResp(req.ID, fmt.Sprintf("unknown action %q", req.Action))
	}
}

func (s *Server) handleStreamEvents(conn net.Conn, enc *json.Encoder, req protocol.Request) {
	// Acknowledge connection
	ack := protocol.Response{
		ID:      req.ID,
		Success: true,
		Data:    json.RawMessage(`{"streaming":true}`),
	}
	if err := enc.Encode(ack); err != nil {
		return
	}

	eventCh := s.pubsub.Subscribe()
	defer s.pubsub.Unsubscribe(eventCh)

	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-s.stopCh:
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			eventJSON, _ := json.Marshal(event)
			resp := protocol.Response{
				Success: true,
				Data:    eventJSON,
			}
			if err := enc.Encode(resp); err != nil {
				return
			}
		}
	}
}

func errorResp(id, msg string) protocol.Response {
	return protocol.Response{
		ID:      id,
		Success: false,
		Error:   msg,
	}
}

func (s *Server) handleBackupCreate(req protocol.Request) protocol.Response {
	var p protocol.BackupCreatePayload
	if len(req.Payload) > 0 {
		_ = json.Unmarshal(req.Payload, &p)
	}
	db, err := s.getDB(req.Scope, req.RepoPath)
	if err != nil {
		return errorResp(req.ID, err.Error())
	}
	targetPath, err := db.CreateRollingBackup(p.Name, p.MaxSnapshots)
	if err != nil {
		return errorResp(req.ID, "failed to create backup: "+err.Error())
	}

	// Also mirror to central backup vault
	centralDir := s.cfg.CentralBackupDir(req.RepoPath)
	_ = os.MkdirAll(centralDir, 0755)
	s.ensureCentralRepoMeta(centralDir, req.RepoPath)
	_, cErr := db.CreateRollingBackupInDir(centralDir, p.Name, p.MaxSnapshots)
	if cErr != nil {
		log.Printf("[tkd] Warning: failed to create central backup in %s: %v", centralDir, cErr)
	}

	data, _ := json.Marshal(map[string]string{
		"path":    targetPath,
		"message": fmt.Sprintf("✓ Created backup snapshot: %s (mirrored in central vault)", targetPath),
	})
	return protocol.Response{
		ID:      req.ID,
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleBackupList(req protocol.Request) protocol.Response {
	db, err := s.getDB(req.Scope, req.RepoPath)
	if err != nil {
		return errorResp(req.ID, err.Error())
	}
	localBackups, _ := db.ListBackups()

	centralDir := s.cfg.CentralBackupDir(req.RepoPath)
	centralBackups, _ := sqlite.ListBackupsInDir(centralDir, "central")

	all := append([]sqlite.BackupMeta{}, localBackups...)
	all = append(all, centralBackups...)
	sort.Slice(all, func(i, j int) bool {
		return all[i].ModTime.After(all[j].ModTime)
	})

	res := make([]protocol.BackupInfo, 0, len(all))
	for _, b := range all {
		res = append(res, protocol.BackupInfo{
			Name:     b.Name,
			Path:     b.Path,
			Size:     b.Size,
			ModTime:  b.ModTime,
			Location: b.Location,
		})
	}

	data, _ := json.Marshal(res)
	return protocol.Response{
		ID:      req.ID,
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleBackupVerify(req protocol.Request) protocol.Response {
	var p protocol.BackupVerifyPayload
	if len(req.Payload) > 0 {
		_ = json.Unmarshal(req.Payload, &p)
	}

	if p.Target != "" {
		tempDB, err := sqlite.Open(p.Target)
		if err != nil {
			return errorResp(req.ID, "failed to open backup file: "+err.Error())
		}
		defer tempDB.Close()
		if err := tempDB.IntegrityCheck(); err != nil {
			return errorResp(req.ID, "integrity check failed for backup: "+err.Error())
		}
		data, _ := json.Marshal(map[string]string{
			"status":  "ok",
			"target":  p.Target,
			"message": fmt.Sprintf("✓ Backup file %s passed SQLite integrity check", p.Target),
		})
		return protocol.Response{ID: req.ID, Success: true, Data: data}
	}

	db, err := s.getDB(req.Scope, req.RepoPath)
	if err != nil {
		return errorResp(req.ID, err.Error())
	}
	if err := db.IntegrityCheck(); err != nil {
		return errorResp(req.ID, "integrity check failed: "+err.Error())
	}
	data, _ := json.Marshal(map[string]string{
		"status":  "ok",
		"target":  db.Path(),
		"message": fmt.Sprintf("✓ Database %s passed SQLite integrity check", db.Path()),
	})
	return protocol.Response{ID: req.ID, Success: true, Data: data}
}

func (s *Server) handleBackupRestore(req protocol.Request) protocol.Response {
	var p protocol.BackupRestorePayload
	if len(req.Payload) > 0 {
		_ = json.Unmarshal(req.Payload, &p)
	}

	targetBackup := strings.TrimSpace(p.BackupPath)
	if targetBackup == "" || strings.EqualFold(targetBackup, "latest") {
		// Find latest across local and central
		db, err := s.getDB(req.Scope, req.RepoPath)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		localBackups, _ := db.ListBackups()
		centralDir := s.cfg.CentralBackupDir(req.RepoPath)
		centralBackups, _ := sqlite.ListBackupsInDir(centralDir, "central")
		all := append([]sqlite.BackupMeta{}, localBackups...)
		all = append(all, centralBackups...)
		if len(all) == 0 {
			return errorResp(req.ID, "no backup snapshots found to restore")
		}
		sort.Slice(all, func(i, j int) bool {
			return all[i].ModTime.After(all[j].ModTime)
		})
		targetBackup = all[0].Path
	} else if !filepath.IsAbs(targetBackup) {
		if _, err := os.Stat(targetBackup); err != nil {
			var localDir string
			if req.Scope == "global" || req.RepoPath == "" {
				localDir = filepath.Join(s.cfg.GlobalDir, "backups")
			} else {
				localDir = filepath.Join(req.RepoPath, ".tasks", "backups")
			}
			localCand := filepath.Join(localDir, targetBackup)
			if _, err := os.Stat(localCand); err == nil {
				targetBackup = localCand
			} else {
				centralCand := filepath.Join(s.cfg.CentralBackupDir(req.RepoPath), targetBackup)
				if _, err := os.Stat(centralCand); err == nil {
					targetBackup = centralCand
				}
			}
		}
	}

	// 1. Verify backup file exists and passes integrity check
	testDB, err := sqlite.Open(targetBackup)
	if err != nil {
		return errorResp(req.ID, "backup file cannot be opened: "+err.Error())
	}
	if err := testDB.IntegrityCheck(); err != nil {
		testDB.Close()
		return errorResp(req.ID, "backup file failed integrity check: "+err.Error())
	}
	testDB.Close()

	// 2. Lock server and close active database connection
	s.mu.Lock()
	defer s.mu.Unlock()

	var dbPath string
	if req.Scope == "global" || req.RepoPath == "" {
		dbPath = s.cfg.GlobalDBPath()
	} else {
		dbPath = s.cfg.LocalDBPath(req.RepoPath)
	}

	if activeDB, exists := s.dbs[dbPath]; exists {
		_ = activeDB.Close()
		delete(s.dbs, dbPath)
	}

	// 3. Create pre-restore safety copy of current dbPath if it exists
	if _, err := os.Stat(dbPath); err == nil {
		preRestorePath := fmt.Sprintf("%s.pre-restore-%s.bak", dbPath, time.Now().Format("20060102-150405"))
		_ = copyFile(dbPath, preRestorePath)
	}

	// Ensure directory exists
	_ = os.MkdirAll(filepath.Dir(dbPath), 0755)

	// 4. Copy to a staging file in the target directory and verify before replacing
	tmpPath := fmt.Sprintf("%s.tmp-restore-%d", dbPath, time.Now().UnixNano())
	if err := copyFile(targetBackup, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return errorResp(req.ID, "failed to copy backup to staging file: "+err.Error())
	}

	testRestored, err := sqlite.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return errorResp(req.ID, "staging database failed to open: "+err.Error())
	}
	if err := testRestored.IntegrityCheck(); err != nil {
		testRestored.Close()
		_ = os.Remove(tmpPath)
		return errorResp(req.ID, "staging database failed integrity check: "+err.Error())
	}
	testRestored.Close()

	// 5. Clean up old WAL/SHM and atomically move staging file into place
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")

	if err := os.Rename(tmpPath, dbPath); err != nil {
		_ = os.Remove(tmpPath)
		return errorResp(req.ID, "failed to atomically replace target db: "+err.Error())
	}

	// 6. Reopen DB to verify and cache
	newDB, err := sqlite.Open(dbPath)
	if err != nil {
		return errorResp(req.ID, "restored database failed to open: "+err.Error())
	}
	s.dbs[dbPath] = newDB

	go s.syncCentralBackup(newDB, req.RepoPath)

	data, _ := json.Marshal(map[string]string{
		"status":  "ok",
		"message": fmt.Sprintf("✓ Successfully restored database from %s", targetBackup),
	})
	return protocol.Response{ID: req.ID, Success: true, Data: data}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

