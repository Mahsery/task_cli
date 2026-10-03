package cli

import (
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"task_cli/internal/client"
	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

func TestApplyStatusFilter(t *testing.T) {
	cases := []struct {
		in           string
		wantStatuses []domain.Status
		wantOrphaned bool
		wantErr      string
	}{
		{in: "", wantStatuses: nil},
		{in: "open", wantStatuses: []domain.Status{domain.StatusOpen}},
		{in: "open,in_progress", wantStatuses: []domain.Status{domain.StatusOpen, domain.StatusInProgress}},
		{in: " open , in-progress ,done", wantStatuses: []domain.Status{domain.StatusOpen, domain.StatusInProgress, domain.StatusDone}},
		{in: "open,o,open", wantStatuses: []domain.Status{domain.StatusOpen}},
		{in: "open,", wantStatuses: []domain.Status{domain.StatusOpen}},
		{in: "orphan", wantOrphaned: true},
		{in: "orphaned", wantOrphaned: true},
		{in: "orphan,orphaned", wantOrphaned: true},
		{in: "open,orphan", wantStatuses: []domain.Status{domain.StatusOpen, domain.StatusOrphan}},
		{in: "open,bogus", wantErr: `invalid status "bogus"`},
		{in: ",", wantErr: "no status given"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			var f domain.TaskFilter
			err := applyStatusFilter(&f, tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(f.Statuses, tc.wantStatuses) {
				t.Errorf("Statuses = %v, want %v", f.Statuses, tc.wantStatuses)
			}
			if f.OnlyOrphaned != tc.wantOrphaned {
				t.Errorf("OnlyOrphaned = %v, want %v", f.OnlyOrphaned, tc.wantOrphaned)
			}
			if f.Status != nil {
				t.Errorf("legacy Status should stay nil, got %v", *f.Status)
			}
		})
	}
}

// TestListCommandSendsMultipleStatuses runs `tk list -s open,in_progress` against a
// mock daemon socket and checks the statuses reach the wire protocol.
func TestListCommandSendsMultipleStatuses(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "mock.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	var (
		mu       sync.Mutex
		payloads []protocol.ListTasksPayload
	)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)
				for {
					var req protocol.Request
					if err := dec.Decode(&req); err != nil {
						return
					}
					resp := protocol.Response{ID: req.ID, Success: true}
					if req.Action == protocol.ActionListTasks {
						var p protocol.ListTasksPayload
						if err := json.Unmarshal(req.Payload, &p); err == nil {
							mu.Lock()
							payloads = append(payloads, p)
							mu.Unlock()
						}
						resp.Data = json.RawMessage(`[]`)
					}
					_ = enc.Encode(resp)
				}
			}(conn)
		}
	}()

	origClient, origCfg, origScope, origRepo := taskClient, cfg, scope, repoRoot
	origStatus, origJSON := listStatus, jsonFlag
	defer func() {
		taskClient, cfg, scope, repoRoot = origClient, origCfg, origScope, origRepo
		listStatus, jsonFlag = origStatus, origJSON
	}()
	cfg = &config.Config{SocketPath: sockPath, Actor: "test-actor"}
	scope = "local"
	repoRoot = "/repo/test"
	taskClient = client.New(cfg, scope, repoRoot)
	jsonFlag = true

	listStatus = "open,in_progress"
	if err := listCmd.RunE(listCmd, nil); err != nil {
		t.Fatalf("list -s open,in_progress failed: %v", err)
	}

	listStatus = "open,nope"
	if err := listCmd.RunE(listCmd, nil); err == nil {
		t.Fatal("expected error for invalid status in list")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 1 {
		t.Fatalf("expected exactly 1 list request (invalid input must not reach daemon), got %d", len(payloads))
	}
	want := []domain.Status{domain.StatusOpen, domain.StatusInProgress}
	if !reflect.DeepEqual(payloads[0].Statuses, want) {
		t.Errorf("payload Statuses = %v, want %v", payloads[0].Statuses, want)
	}
	if payloads[0].OnlyOrphaned {
		t.Error("OnlyOrphaned should not be set for open,in_progress")
	}
}
