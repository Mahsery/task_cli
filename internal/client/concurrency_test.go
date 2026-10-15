package client

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"task_cli/internal/config"
	"task_cli/internal/protocol"
)

// TestConcurrentCallsAreSerialized reproduces the TUI pattern behind the
// "program experienced a panic" crash after closing $EDITOR: several tea.Cmds
// (edit callback, live-event reload, related-item fetch) share one Client and
// issue RPCs concurrently. Every caller must receive the response to its own
// request, with no interleaved frames and no nil-encoder panics.
func TestConcurrentCallsAreSerialized(t *testing.T) {
	dir, err := os.MkdirTemp("", "tk-cc-*")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "c.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				dec := json.NewDecoder(conn)
				enc := json.NewEncoder(conn)
				for {
					var req protocol.Request
					if err := dec.Decode(&req); err != nil {
						return
					}
					var p protocol.GetTaskPayload
					_ = json.Unmarshal(req.Payload, &p)
					data, _ := json.Marshal(map[string]string{"id": p.ID})
					if err := enc.Encode(protocol.Response{ID: req.ID, Success: true, Data: data}); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	c := New(&config.Config{SocketPath: sock}, "local", "")
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	const workers, perWorker = 16, 50
	var wg sync.WaitGroup
	errs := make(chan string, workers*perWorker)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWorker {
				id := "T-" + strconv.Itoa(w*perWorker+i+1)
				task, err := c.GetTask(id)
				if err != nil {
					errs <- id + ": " + err.Error()
					continue
				}
				if task.ID != id {
					errs <- "asked for " + id + ", got response for " + task.ID
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
