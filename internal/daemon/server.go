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
		return db, nil
	}

	db, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %q: %w", dbPath, err)
	}

	s.dbs[dbPath] = db
	return db, nil
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
			dbs := make([]*sqlite.DB, 0, len(s.dbs))
			for _, db := range s.dbs {
				dbs = append(dbs, db)
			}
			s.mu.Unlock()

			for _, db := range dbs {
				_ = db.Checkpoint("PASSIVE")
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
		task, err := db.CreateTask(req.Scope, req.RepoPath, p.Title, p.Description, p.Status, p.Priority, p.Tags, p.Author, p.Assignee)
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
			Scope:    p.Scope,
			RepoPath: p.RepoPath,
			Status:   p.Status,
			Priority: p.Priority,
			Tag:      p.Tag,
			Assignee: p.Assignee,
			Search:   p.Search,
			Sort:     p.Sort,
			Limit:    p.Limit,
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
			Message:   fmt.Sprintf("- %s deleted", p.ID),
		})

		resp.Data = json.RawMessage(`{"status":"deleted"}`)
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
