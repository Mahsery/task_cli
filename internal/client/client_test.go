package client

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

func TestPingPassiveNonExistent(t *testing.T) {
	nonExistentSocket := filepath.Join(os.TempDir(), "non-existent-tk-test.sock")
	_ = os.Remove(nonExistentSocket)

	cfg := &config.Config{
		SocketPath: nonExistentSocket,
	}
	c := New(cfg, "local", "")
	err := c.PingPassive()
	if err == nil {
		t.Fatal("expected PingPassive to fail on non-existent socket, but got nil")
	}
}

func TestMockListenerPing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-client-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	sockPath := filepath.Join(tmpDir, "mock.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on mock socket: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Handle one connection in goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)

		var req protocol.Request
		if err := dec.Decode(&req); err != nil {
			return
		}

		if req.Action == protocol.ActionPing {
			_ = enc.Encode(protocol.Response{
				ID:      req.ID,
				Success: true,
			})
		}
	}()

	cfg := &config.Config{
		SocketPath: sockPath,
	}
	c := New(cfg, "local", "")
	if err := c.PingPassive(); err != nil {
		t.Fatalf("PingPassive failed on mock listener: %v", err)
	}

	<-done
}

func TestArchiveAndUnarchiveMock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-client-archive-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	sockPath := filepath.Join(tmpDir, "mock.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on mock socket: %v", err)
	}
	defer func() { _ = ln.Close() }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)

		for i := 0; i < 2; i++ {
			var req protocol.Request
			if err := dec.Decode(&req); err != nil {
				return
			}
			now := time.Now()
			switch req.Action {
			case protocol.ActionArchiveTask:
				var p protocol.ArchiveTaskPayload
				_ = json.Unmarshal(req.Payload, &p)
				task := domain.Task{
					ID:         p.ID,
					Status:     domain.StatusArchived,
					ArchivedAt: &now,
				}
				data, _ := json.Marshal(task)
				_ = enc.Encode(protocol.Response{
					ID:      req.ID,
					Success: true,
					Data:    data,
				})
			case protocol.ActionUnarchiveTask:
				var p protocol.UnarchiveTaskPayload
				_ = json.Unmarshal(req.Payload, &p)
				task := domain.Task{
					ID:     p.ID,
					Status: domain.StatusOpen,
				}
				data, _ := json.Marshal(task)
				_ = enc.Encode(protocol.Response{
					ID:      req.ID,
					Success: true,
					Data:    data,
				})
			}
		}
	}()

	cfg := &config.Config{
		SocketPath: sockPath,
	}
	c := New(cfg, "local", "")
	archived, err := c.ArchiveTask("T-1", "agent", "done")
	if err != nil {
		t.Fatalf("ArchiveTask failed: %v", err)
	}
	if !archived.IsArchived() {
		t.Errorf("expected archived task")
	}

	unarchived, err := c.UnarchiveTask("T-1", "agent", "resume")
	if err != nil {
		t.Fatalf("UnarchiveTask failed: %v", err)
	}
	if unarchived.IsArchived() {
		t.Errorf("expected unarchived task")
	}

	<-done
}
