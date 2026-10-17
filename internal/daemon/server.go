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

func (s *Server) resolveDBTarget(scope, repoPath, reqDBPath string) (dbPath, resolvedScope, resolvedRepo, backupKey string) {
	customDB := strings.TrimSpace(reqDBPath)
	if customDB == "" && s.cfg != nil {
		customDB = strings.TrimSpace(s.cfg.DBPath)
	}
	if customDB != "" {
		if scope == domain.ScopeGlobal {
			return customDB, domain.ScopeGlobal, "", customDB
		}
		if repoPath == "" {
			repoPath = filepath.Dir(customDB)
		}
		return customDB, domain.ScopeLocal, repoPath, customDB
	}
	if scope == "global" || repoPath == "" {
		return s.cfg.GlobalDBPath(), "global", "", ""
	}
	return s.cfg.LocalDBPath(repoPath), scope, repoPath, repoPath
}

func (s *Server) getDBForReq(req protocol.Request) (*sqlite.DB, error) {
	return s.getDBWithCustomPath(req.Scope, req.RepoPath, req.DBPath)
}

func (s *Server) getDBWithCustomPath(scope, repoPath, customDBPath string) (*sqlite.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shutdown {
		return nil, errors.New("server is shutting down")
	}

	dbPath, scope, repoPath, backupKey := s.resolveDBTarget(scope, repoPath, customDBPath)

	if db, exists := s.dbs[dbPath]; exists {
		// Check if file was unlinked/deleted on disk while held open in memory
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			log.Printf("[tkd] Database %s was deleted on disk while active in daemon memory; auto-resurrecting...", dbPath)
			if rErr := db.Resurrect(dbPath); rErr != nil {
				log.Printf("[tkd] Failed to resurrect %s: %v", dbPath, rErr)
			} else {
				log.Printf("[tkd] Successfully resurrected %s on disk", dbPath)
				go s.syncCentralBackup(db, backupKey)
			}
		}
		return db, nil
	}

	// Database is not currently in memory. Check if missing on disk.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		centralDir := s.cfg.CentralBackupDir(backupKey)
		latestBackup := findLatestBackupInDir(centralDir)
		if latestBackup != "" {
			log.Printf("[tkd] Database %s missing on disk, found central backup %s; auto-restoring...", dbPath, latestBackup)
			_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)
			if err := copyFile(latestBackup, dbPath); err == nil {
				log.Printf("[tkd] Successfully auto-restored %s from central backup", dbPath)
			} else {
				log.Printf("[tkd] Warning: failed to copy central backup %s to %s: %v", latestBackup, dbPath, err)
			}
		}
	}

	db, err := openDatabase(dbPath, scope, repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %q: %w", dbPath, err)
	}

	s.dbs[dbPath] = db
	go s.syncCentralBackup(db, backupKey)
	return db, nil
}

func openDatabase(dbPath, scope, repoPath string) (*sqlite.DB, error) {
	if scope == "global" || repoPath == "" {
		return sqlite.Open(dbPath)
	}
	return sqlite.OpenRepository(dbPath, repoPath)
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
	_ = os.MkdirAll(centralDir, 0o755)
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
		_ = os.WriteFile(metaFile, data, 0o644)
	}
}

func (s *Server) Start() error {
	// 1. Prepare unix socket unless running in TCP-only mode (when --tcp is given without an explicit --socket)
	bindUnix := s.cfg.SocketPath != "" && (s.cfg.TCPAddr == "" || s.cfg.ExplicitSocket)
	if bindUnix {
		_ = os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o755)

		// Test if existing socket is active
		if conn, err := net.Dial("unix", s.cfg.SocketPath); err == nil {
			_ = conn.Close()
			return fmt.Errorf("daemon is already running on %s", s.cfg.SocketPath)
		}
		_ = os.Remove(s.cfg.SocketPath)

		unixLn, err := net.Listen("unix", s.cfg.SocketPath)
		if err != nil {
			return fmt.Errorf("failed to listen on unix socket %s: %w", s.cfg.SocketPath, err)
		}
		s.unixLn = unixLn
		_ = os.Chmod(s.cfg.SocketPath, 0o600)

		log.Printf("[tkd] Listening on unix socket %s", s.cfg.SocketPath)
	}

	// 2. Optional TCP socket (e.g. for isolated testing or remote SSH forwarding)
	if s.cfg.TCPAddr != "" {
		tcpLn, err := net.Listen("tcp", s.cfg.TCPAddr)
		if err != nil {
			if s.unixLn != nil {
				_ = s.unixLn.Close()
				_ = os.Remove(s.cfg.SocketPath)
				s.unixLn = nil
			}
			return fmt.Errorf("failed to listen on TCP %s: %w", s.cfg.TCPAddr, err)
		}
		s.tcpLn = tcpLn
		log.Printf("[tkd] Listening on TCP %s", s.cfg.TCPAddr)
	}

	if s.unixLn == nil && s.tcpLn == nil {
		return errors.New("no unix socket or TCP address configured for daemon")
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

	if s.unixLn != nil && s.tcpLn != nil {
		go s.acceptLoop(s.tcpLn)
		s.acceptLoop(s.unixLn)
	} else if s.unixLn != nil {
		s.acceptLoop(s.unixLn)
	} else {
		s.acceptLoop(s.tcpLn)
	}
	return nil
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
		_ = s.unixLn.Close()
		_ = os.Remove(s.cfg.SocketPath)
	}
	if s.tcpLn != nil {
		_ = s.tcpLn.Close()
	}

	s.mu.Lock()
	for path, db := range s.dbs {
		log.Printf("[tkd] Closing database: %s", path)
		_ = db.Close()
	}
	s.mu.Unlock()

	log.Println("[tkd] Daemon stopped.")
}

// acceptLoop serves connections until the listener is closed or the server stops.
func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				if errors.Is(err, net.ErrClosed) {
					return
				}
				log.Printf("[tkd] Accept error: %v", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer func() { _ = conn.Close() }()

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
	if req.DBPath == "" && s.cfg != nil && s.cfg.DBPath != "" {
		req.DBPath = s.cfg.DBPath
	}
	if req.Scope == domain.ScopeGlobal || (req.RepoPath == "" && req.DBPath == "") {
		req.Scope = domain.ScopeGlobal
		req.RepoPath = ""
	} else if req.RepoPath == "" && req.DBPath != "" {
		req.Scope = domain.ScopeLocal
		req.RepoPath = filepath.Dir(req.DBPath)
	}
	resp := s.executeDispatch(req)
	if resp.Success {
		switch req.Action {
		case protocol.ActionCreateTask, protocol.ActionUpdateTask, protocol.ActionClaimTask,
			protocol.ActionReviewTask, protocol.ActionDeleteTask, protocol.ActionRestoreTask, protocol.ActionAttachGit,
			protocol.ActionAddDependency, protocol.ActionRemoveDependency, protocol.ActionOrphanTask,
			protocol.ActionReapStaleTasks, protocol.ActionTransferTask, protocol.ActionArchiveTask,
			protocol.ActionUnarchiveTask, protocol.ActionCreateTopic, protocol.ActionUpdateTopic,
			protocol.ActionMoveTopic, protocol.ActionDeleteTopic, protocol.ActionMoveItems, protocol.ActionPromotePost:
			if db, err := s.getDBForReq(req); err == nil {
				_, _, _, backupKey := s.resolveDBTarget(req.Scope, req.RepoPath, req.DBPath)
				go s.syncCentralBackup(db, backupKey)
			}
		}
	}
	return resp
}

func (s *Server) executeDispatch(req protocol.Request) protocol.Response {
	switch req.Action {
	case protocol.ActionPing:
		return protocol.Response{
			ID:      req.ID,
			Success: true,
			Data:    json.RawMessage(`{"status":"pong"}`),
		}

	case protocol.ActionCreateTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.CreateTaskPayload) (*domain.Task, error) {
			task, err := db.CreateItem(sqlite.CreateItemParams{
				Scope: req.Scope, RepoPath: req.RepoPath, Kind: p.Kind, Topic: p.Topic,
				Title: p.Title, Description: p.Description, Status: p.Status, Priority: p.Priority,
				Tags: p.Tags, Author: p.Author, Assignee: p.Assignee, Reviewer: p.Reviewer, DependsOn: p.DependsOn,
				Branch: p.Branch, ExternalRef: p.ExternalRef, PRURL: p.PRURL,
			})
			if err != nil {
				return nil, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskCreated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("+ %s created: %q", task.ID, task.Title),
			})
			return task, nil
		})

	case protocol.ActionGetTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.GetTaskPayload) (*domain.Task, error) {
			return db.GetTask(p.ID)
		})

	case protocol.ActionUpdateTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.UpdateTaskPayload) (*domain.Task, error) {
			// Hard completion enforcement: cannot mark done/closed without review or when prerequisites/earlier tasks are open
			if p.Status != nil && (*p.Status == domain.StatusDone || *p.Status == domain.StatusClosed) {
				if err := db.CheckCanClose(p.ID, p.Reviewer, p.AddReviewNote, p.Force); err != nil {
					return nil, err
				}
			}

			task, err := db.UpdateTaskExtended(sqlite.UpdateTaskOpts{
				ID: p.ID, Title: p.Title, Description: p.Description, Status: p.Status,
				Priority: p.Priority, Tags: p.Tags, AddTags: p.AddTags, RemoveTags: p.RemoveTags,
				Assignee: p.Assignee, Reviewer: p.Reviewer, AddNote: p.AddNote, AddReviewNote: p.AddReviewNote, Author: p.Author,
				Branch: p.Branch, ExternalRef: p.ExternalRef, PRURL: p.PRURL,
			})
			if err != nil {
				return nil, err
			}

			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("~ %s updated (status: %s)", task.ID, task.Status),
			})
			return task, nil
		})

	case protocol.ActionReviewTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.ReviewTaskPayload) (*domain.Task, error) {
			task, err := db.ReviewTask(p.ID, p.Reviewer, p.Note, p.Author)
			if err != nil {
				return nil, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("★ %s reviewed by %q", task.ID, task.Reviewer),
			})
			return task, nil
		})

	case protocol.ActionClaimTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.ClaimTaskPayload) (*domain.Task, error) {
			task, err := db.ClaimTaskWithBranch(p.ID, p.Assignee, p.Branch, p.Force)
			if err != nil {
				return nil, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("* %s claimed by %q", task.ID, p.Assignee),
			})
			return task, nil
		})

	case protocol.ActionOrphanTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.OrphanTaskPayload) (*domain.Task, error) {
			task, err := db.OrphanTask(p.ID, p.Actor, p.Reason)
			if err != nil {
				return nil, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("⚠ %s orphaned (dropped assignee, status: open)", task.ID),
			})
			return task, nil
		})

	case protocol.ActionTransferTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.TransferTaskPayload) (*domain.Task, error) {
			task, err := db.TransferTask(p.ID, p.Actor, p.NewAssignee, p.Note, p.Force)
			if err != nil {
				return nil, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("* %s transferred to %q", task.ID, p.NewAssignee),
			})
			return task, nil
		})

	case protocol.ActionArchiveTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.ArchiveTaskPayload) (*domain.Task, error) {
			task, err := db.ArchiveTask(p.ID, p.Actor, p.Reason)
			if err != nil {
				return nil, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("⊘ %s archived", task.ID),
			})
			return task, nil
		})

	case protocol.ActionUnarchiveTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.UnarchiveTaskPayload) (*domain.Task, error) {
			task, err := db.UnarchiveTask(p.ID, p.Actor, p.Reason)
			if err != nil {
				return nil, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("↺ %s unarchived", task.ID),
			})
			return task, nil
		})

	case protocol.ActionReapStaleTasks:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.ReapStaleTasksPayload) (protocol.ReapStaleTasksResult, error) {
			candidates, orphanedIDs, err := db.ReapStaleTasks(req.Scope, req.RepoPath, p.Threshold, p.DryRun, p.Actor, p.Reason)
			if err != nil {
				return protocol.ReapStaleTasksResult{}, err
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
			return protocol.ReapStaleTasksResult{
				Threshold:  p.Threshold,
				DryRun:     p.DryRun,
				Candidates: candidates,
				Orphaned:   orphanedIDs,
			}, nil
		})

	case protocol.ActionListTasks:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.ListTasksPayload) ([]*domain.Task, error) {
			if (p.Scope != "" && p.Scope != req.Scope) || (p.RepoPath != "" && p.RepoPath != req.RepoPath) {
				var err error
				db, err = s.getDBWithCustomPath(p.Scope, p.RepoPath, req.DBPath)
				if err != nil {
					return nil, err
				}
			}
			filter := p.ToFilter()
			if filter.RepoPath == "" && req.RepoPath != "" && filter.Scope != domain.ScopeGlobal {
				filter.RepoPath = req.RepoPath
			}
			return db.ListTasks(filter)
		})

	case protocol.ActionDeleteTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.DeleteTaskPayload) (json.RawMessage, error) {
			if err := db.DeleteTask(p.ID); err != nil {
				return nil, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskDeleted,
				Timestamp: time.Now(),
				Task:      &domain.Task{ID: p.ID},
				Message:   fmt.Sprintf("- %s deleted", p.ID),
			})
			return json.RawMessage(`{"status":"deleted"}`), nil
		})

	case protocol.ActionRestoreTask:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.RestoreTaskPayload) (*domain.Task, error) {
			var restored *domain.Task
			var err error
			if p.ID != "" {
				restored, err = db.RestoreTaskByID(p.ID)
				if err != nil {
					return nil, err
				}
			} else if p.Task != nil {
				if err := db.RestoreTask(p.Task); err != nil {
					return nil, err
				}
				restored = p.Task
			} else {
				return nil, errors.New("either id or task must be provided for restore")
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskCreated,
				Timestamp: time.Now(),
				Task:      restored,
				Message:   fmt.Sprintf("+ %s restored: %q", restored.ID, restored.Title),
			})
			return restored, nil
		})

	case protocol.ActionListTombstones:
		return handleWithDB(s, req, func(db *sqlite.DB, _ struct{}) ([]*domain.Task, error) {
			return db.ListTombstones(req.Scope, req.RepoPath)
		})

	case protocol.ActionAddDependency:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.AddDependencyPayload) (json.RawMessage, error) {
			if err := db.AddDependency(p.TaskID, p.DependsOnID); err != nil {
				return nil, err
			}
			updatedTask, err := db.GetTask(p.TaskID)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch updated task %s: %w", p.TaskID, err)
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      updatedTask,
				Message:   fmt.Sprintf("🔗 %s linked to depend on %s", p.TaskID, p.DependsOnID),
			})
			return json.RawMessage(`{"status":"linked"}`), nil
		})

	case protocol.ActionRemoveDependency:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.RemoveDependencyPayload) (json.RawMessage, error) {
			if err := db.RemoveDependency(p.TaskID, p.DependsOnID); err != nil {
				return nil, err
			}
			updatedTask, err := db.GetTask(p.TaskID)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch updated task %s: %w", p.TaskID, err)
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      updatedTask,
				Message:   fmt.Sprintf("🔓 %s unlinked from %s", p.TaskID, p.DependsOnID),
			})
			return json.RawMessage(`{"status":"unlinked"}`), nil
		})

	case protocol.ActionGetDependencies:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.GetDependenciesPayload) (protocol.DependenciesData, error) {
			task, err := db.GetTask(p.TaskID)
			if err != nil {
				return protocol.DependenciesData{}, err
			}
			return protocol.DependenciesData{
				TaskID:    task.ID,
				DependsOn: task.DependsOn,
				BlockedBy: task.BlockedBy,
				Blocks:    task.Blocks,
				IsBlocked: task.IsBlocked,
			}, nil
		})

	case protocol.ActionAttachGit:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.AttachGitPayload) (*domain.Task, error) {
			if p.MarkDone {
				if err := db.CheckCanClose(p.TaskID, nil, nil, p.Force); err != nil {
					return nil, err
				}
			}

			commits := p.Commits
			if len(commits) == 0 && strings.TrimSpace(p.CommitSHA) != "" {
				commits = []domain.TaskCommit{
					{
						SHA:         p.CommitSHA,
						Subject:     p.CommitMsg,
						DiffStat:    p.DiffStat,
						DiffContent: p.DiffContent,
					},
				}
			}
			task, err := db.AttachGitCommits(p.TaskID, p.CommitSHA, p.CommitMsg, p.DiffStat, p.DiffContent, commits, p.MarkDone)
			if err != nil {
				return nil, err
			}

			msg := fmt.Sprintf("+ %s git info attached (%s)", task.ID, task.CommitSHA)
			if p.MarkDone {
				msg = fmt.Sprintf("✓ %s marked DONE (+ %s)", task.ID, task.CommitSHA)
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventGitAttached,
				Timestamp: time.Now(),
				Task:      task,
				Message:   msg,
			})
			return task, nil
		})

	case protocol.ActionListTopics:
		return handleWithDB(s, req, func(db *sqlite.DB, _ struct{}) ([]*domain.Topic, error) {
			return db.ListTopics()
		})

	case protocol.ActionGetTopic:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.TopicRefPayload) (*domain.Topic, error) {
			return db.GetTopic(p.Ref)
		})

	case protocol.ActionCreateTopic:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.CreateTopicPayload) (*domain.Topic, error) {
			tp, err := db.CreateTopic(p.Title, p.Slug, p.Description, p.Kind, p.Parent)
			if err != nil {
				return nil, err
			}
			s.publishTopics(fmt.Sprintf("+ topic @%s created (%s)", tp.Path, tp.Kind))
			return tp, nil
		})

	case protocol.ActionUpdateTopic:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.UpdateTopicPayload) (*domain.Topic, error) {
			tp, err := db.UpdateTopic(p.Ref, p.Title, p.Slug, p.Description, p.Kind)
			if err != nil {
				return nil, err
			}
			s.publishTopics(fmt.Sprintf("~ topic @%s updated", tp.Path))
			return tp, nil
		})

	case protocol.ActionMoveTopic:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.MoveTopicPayload) (*domain.Topic, error) {
			tp, err := db.MoveTopic(p.Ref, p.Parent)
			if err != nil {
				return nil, err
			}
			s.publishTopics(fmt.Sprintf("~ topic moved to @%s", tp.Path))
			return tp, nil
		})

	case protocol.ActionDeleteTopic:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.DeleteTopicPayload) (json.RawMessage, error) {
			if err := db.DeleteTopic(p.Ref, p.MoveTo); err != nil {
				return nil, err
			}
			s.publishTopics(fmt.Sprintf("- topic @%s deleted", domain.NormalizeTopicRef(p.Ref)))
			return json.RawMessage(`{"status":"deleted"}`), nil
		})

	case protocol.ActionMoveItems:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.MoveItemsPayload) ([]*domain.Task, error) {
			moved, err := db.MoveItems(p.IDs, p.Topic, p.Actor)
			if err != nil {
				return nil, err
			}
			for _, t := range moved {
				dest := "unfiled"
				if t.TopicPath != "" {
					dest = "@" + t.TopicPath
				}
				s.pubsub.Publish(protocol.Event{
					Type:      protocol.EventTaskUpdated,
					Timestamp: time.Now(),
					Task:      t,
					Message:   fmt.Sprintf("→ %s moved to %s", t.ID, dest),
				})
			}
			return moved, nil
		})

	case protocol.ActionPromotePost:
		return handleWithDB(s, req, func(db *sqlite.DB, p protocol.PromotePostPayload) (protocol.PromotePostResult, error) {
			task, post, err := db.PromotePost(p.ID, p.Topic, p.Author)
			if err != nil {
				return protocol.PromotePostResult{}, err
			}
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskCreated,
				Timestamp: time.Now(),
				Task:      task,
				Message:   fmt.Sprintf("⇪ %s promoted to %s: %q", post.ID, task.ID, task.Title),
			})
			s.pubsub.Publish(protocol.Event{
				Type:      protocol.EventTaskUpdated,
				Timestamp: time.Now(),
				Task:      post,
				Message:   fmt.Sprintf("✓ %s resolved (promoted to %s)", post.ID, task.ID),
			})
			return protocol.PromotePostResult{Task: task, Post: post}, nil
		})

	case protocol.ActionShutdown:
		return protocol.Response{
			ID:      req.ID,
			Success: true,
			Data:    json.RawMessage(`{"status":"shutting_down"}`),
		}

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

// handleWithDB unmarshals payload P (if req.Payload is non-empty), resolves the DB
// for the request, and invokes fn. On success, it marshals the returned value to JSON in resp.Data.
func handleWithDB[P any, R any](s *Server, req protocol.Request, fn func(db *sqlite.DB, p P) (R, error)) protocol.Response {
	var p P
	if len(req.Payload) > 0 {
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid payload: "+err.Error())
		}
	}
	db, err := s.getDBForReq(req)
	if err != nil {
		return errorResp(req.ID, err.Error())
	}
	res, err := fn(db, p)
	if err != nil {
		return errorResp(req.ID, err.Error())
	}
	var data []byte
	switch v := any(res).(type) {
	case json.RawMessage:
		data = v
	case []byte:
		data = v
	default:
		data, err = json.Marshal(res)
		if err != nil {
			return errorResp(req.ID, "marshal response error: "+err.Error())
		}
	}
	return protocol.Response{
		ID:      req.ID,
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleBackupCreate(req protocol.Request) protocol.Response {
	return handleWithDB(s, req, func(db *sqlite.DB, p protocol.BackupCreatePayload) (map[string]string, error) {
		if strings.TrimSpace(p.TargetPath) != "" {
			targetPath := strings.TrimSpace(p.TargetPath)
			if err := db.Backup(targetPath); err != nil {
				return nil, fmt.Errorf("failed to write snapshot: %w", err)
			}
			return map[string]string{
				"path":    targetPath,
				"message": fmt.Sprintf("✓ Wrote portable database snapshot: %s", targetPath),
			}, nil
		}

		targetPath, err := db.CreateRollingBackup(p.Name, p.MaxSnapshots)
		if err != nil {
			return nil, fmt.Errorf("failed to create backup: %w", err)
		}

		// Also mirror to central backup vault
		_, _, _, backupKey := s.resolveDBTarget(req.Scope, req.RepoPath, req.DBPath)
		centralDir := s.cfg.CentralBackupDir(backupKey)
		_ = os.MkdirAll(centralDir, 0o755)
		s.ensureCentralRepoMeta(centralDir, backupKey)
		_, cErr := db.CreateRollingBackupInDir(centralDir, p.Name, p.MaxSnapshots)
		if cErr != nil {
			log.Printf("[tkd] Warning: failed to create central backup in %s: %v", centralDir, cErr)
		}

		return map[string]string{
			"path":    targetPath,
			"message": fmt.Sprintf("✓ Created backup snapshot: %s (mirrored in central vault)", targetPath),
		}, nil
	})
}

func (s *Server) handleBackupList(req protocol.Request) protocol.Response {
	return handleWithDB(s, req, func(db *sqlite.DB, _ struct{}) ([]protocol.BackupInfo, error) {
		localBackups, _ := db.ListBackups()

		_, _, _, backupKey := s.resolveDBTarget(req.Scope, req.RepoPath, req.DBPath)
		centralDir := s.cfg.CentralBackupDir(backupKey)
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
		return res, nil
	})
}

func (s *Server) handleBackupVerify(req protocol.Request) protocol.Response {
	var p protocol.BackupVerifyPayload
	if len(req.Payload) > 0 {
		_ = json.Unmarshal(req.Payload, &p)
	}

	if p.Target != "" {
		if err := sqlite.VerifyBackupFile(p.Target); err != nil {
			return errorResp(req.ID, "backup verification failed: "+err.Error())
		}
		data, _ := json.Marshal(map[string]string{
			"status":  "ok",
			"target":  p.Target,
			"message": fmt.Sprintf("✓ Backup file %s passed SQLite integrity check", p.Target),
		})
		return protocol.Response{ID: req.ID, Success: true, Data: data}
	}

	db, err := s.getDBForReq(req)
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
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return errorResp(req.ID, "invalid restore payload: "+err.Error())
		}
	}

	dbPath, scope, repoPath, backupKey := s.resolveDBTarget(req.Scope, req.RepoPath, req.DBPath)

	targetBackup := strings.TrimSpace(p.BackupPath)
	if targetBackup == "" || strings.EqualFold(targetBackup, "latest") {
		// Find latest across local and central
		db, err := s.getDBForReq(req)
		if err != nil {
			return errorResp(req.ID, err.Error())
		}
		localBackups, _ := db.ListBackups()
		centralDir := s.cfg.CentralBackupDir(backupKey)
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
			localDir := filepath.Join(filepath.Dir(dbPath), "backups")
			localCand := filepath.Join(localDir, targetBackup)
			if _, err := os.Stat(localCand); err == nil {
				targetBackup = localCand
			} else {
				centralCand := filepath.Join(s.cfg.CentralBackupDir(backupKey), targetBackup)
				if _, err := os.Stat(centralCand); err == nil {
					targetBackup = centralCand
				}
			}
		}
	}

	// 1. Verify backup file exists on disk and passes read-only integrity check
	if err := sqlite.VerifyBackupFile(targetBackup); err != nil {
		return errorResp(req.ID, "backup file failed verification: "+err.Error())
	}

	// 2. Lock server and close active database connection
	s.mu.Lock()
	defer s.mu.Unlock()

	if activeDB, exists := s.dbs[dbPath]; exists {
		_ = activeDB.Close()
		delete(s.dbs, dbPath)
	}

	// 3. Create pre-restore safety copy of current dbPath if it exists
	if _, err := os.Stat(dbPath); err == nil {
		preRestorePath := fmt.Sprintf("%s.pre-restore-%s.bak", dbPath, time.Now().Format("20060102-150405"))
		if err := copyFile(dbPath, preRestorePath); err != nil {
			return errorResp(req.ID, fmt.Sprintf("failed to create safety pre-restore backup: %v", err))
		}
	}

	// Ensure directory exists
	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)

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
		_ = testRestored.Close()
		_ = os.Remove(tmpPath)
		return errorResp(req.ID, "staging database failed integrity check: "+err.Error())
	}
	_ = testRestored.Close()

	// 5. Clean up old WAL/SHM and atomically move staging file into place
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")

	if err := os.Rename(tmpPath, dbPath); err != nil {
		_ = os.Remove(tmpPath)
		return errorResp(req.ID, "failed to atomically replace target db: "+err.Error())
	}

	// 6. Reopen DB to verify and cache
	newDB, err := openDatabase(dbPath, scope, repoPath)
	if err != nil {
		return errorResp(req.ID, "restored database failed to open: "+err.Error())
	}
	s.dbs[dbPath] = newDB

	go s.syncCentralBackup(newDB, backupKey)

	data, _ := json.Marshal(map[string]string{
		"status":  "ok",
		"message": fmt.Sprintf("✓ Successfully restored database from %s", targetBackup),
	})
	return protocol.Response{ID: req.ID, Success: true, Data: data}
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }() // read-only; close error carries no data-loss risk

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		// Close flushes the written copy; surface its failure unless an earlier error occurred.
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(out, in)
	return err
}

// publishTopics notifies subscribers that the topic tree changed.
func (s *Server) publishTopics(msg string) {
	s.pubsub.Publish(protocol.Event{
		Type:      protocol.EventTopicsChanged,
		Timestamp: time.Now(),
		Message:   msg,
	})
}
