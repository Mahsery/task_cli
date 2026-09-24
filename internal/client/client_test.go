package client

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"task_cli/internal/config"
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
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "mock.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on mock socket: %v", err)
	}
	defer ln.Close()

	// Handle one connection in goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

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
