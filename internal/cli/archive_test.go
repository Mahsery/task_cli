package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"task_cli/internal/client"
	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

func TestArchiveCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"archive"})
	if err != nil || cmd == nil {
		t.Fatalf("archive command not registered: %v", err)
	}

	if cmd.Name() != "archive" {
		t.Errorf("expected command name archive, got %s", cmd.Name())
	}

	// Verify alias 'arc'
	aliasCmd, _, err := RootCmd.Find([]string{"arc"})
	if err != nil || aliasCmd == nil || aliasCmd.Name() != "archive" {
		t.Errorf("expected 'arc' alias to resolve to archive command, got %v", aliasCmd)
	}

	flags := []string{"message", "actor", "list"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on archive command", f)
		}
	}
	if cmd.Flags().ShorthandLookup("m") == nil {
		t.Error("expected -m shorthand on archive command")
	}
	if cmd.Flags().ShorthandLookup("l") == nil {
		t.Error("expected -l shorthand on archive command")
	}

	// Verify subcommands: 'list' and 'unarchive'
	listSub, _, err := RootCmd.Find([]string{"archive", "list"})
	if err != nil || listSub == nil || listSub.Name() != "list" {
		t.Errorf("expected 'archive list' subcommand, got %v", listSub)
	}

	// Subcommand alias 'ls'
	lsSub, _, err := RootCmd.Find([]string{"archive", "ls"})
	if err != nil || lsSub == nil || lsSub.Name() != "list" {
		t.Errorf("expected 'archive ls' alias, got %v", lsSub)
	}

	unarchiveSub, _, err := RootCmd.Find([]string{"archive", "unarchive"})
	if err != nil || unarchiveSub == nil || unarchiveSub.Name() != "unarchive" {
		t.Errorf("expected 'archive unarchive' subcommand, got %v", unarchiveSub)
	}

	// Subcommand alias 'restore'
	restoreSub, _, err := RootCmd.Find([]string{"archive", "restore"})
	if err != nil || restoreSub == nil || restoreSub.Name() != "unarchive" {
		t.Errorf("expected 'archive restore' alias, got %v", restoreSub)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("failed to render archive help: %v", err)
	}
}

func TestUnarchiveCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"unarchive"})
	if err != nil || cmd == nil {
		t.Fatalf("unarchive root command not registered: %v", err)
	}

	if cmd.Name() != "unarchive" {
		t.Errorf("expected command name unarchive, got %s", cmd.Name())
	}

	flags := []string{"message", "actor"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on unarchive command", f)
		}
	}
	if cmd.Flags().ShorthandLookup("m") == nil {
		t.Error("expected -m shorthand on unarchive command")
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("failed to render unarchive help: %v", err)
	}
}

func TestListArchivedFlagRegistration(t *testing.T) {
	flag := listCmd.Flags().Lookup("archived")
	if flag == nil {
		t.Fatal("expected --archived flag on list command")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected --archived default to be 'false', got %s", flag.DefValue)
	}
}

func TestArchiveAndUnarchiveExecutionWithMock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tk-cli-archive-test-*")
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

	// Mock server handling requests
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)

				for {
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
							Title:      "Test Task " + p.ID,
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
							Title:  "Test Task " + p.ID,
							Status: domain.StatusOpen,
						}
						data, _ := json.Marshal(task)
						_ = enc.Encode(protocol.Response{
							ID:      req.ID,
							Success: true,
							Data:    data,
						})

					case protocol.ActionListTasks:
						tasks := []*domain.Task{
							{
								ID:         "T-1",
								Title:      "Archived Item 1",
								Status:     domain.StatusArchived,
								ArchivedAt: &now,
								CreatedAt:  now.Add(-2 * time.Hour),
							},
						}
						data, _ := json.Marshal(tasks)
						_ = enc.Encode(protocol.Response{
							ID:      req.ID,
							Success: true,
							Data:    data,
						})

					default:
						_ = enc.Encode(protocol.Response{
							ID:      req.ID,
							Success: true,
						})
					}
				}
			}(conn)
		}
	}()

	origClient := taskClient
	origCfg := cfg
	origScope := scope
	origRepoRoot := repoRoot
	defer func() {
		taskClient = origClient
		cfg = origCfg
		scope = origScope
		repoRoot = origRepoRoot
	}()

	testCfg := &config.Config{
		SocketPath: sockPath,
		Actor:      "test-actor",
	}
	cfg = testCfg
	scope = "local"
	repoRoot = "/repo/test"
	taskClient = client.New(testCfg, scope, repoRoot)

	// 1. Test runUnarchive with single ID
	if err := runUnarchive([]string{"T-1"}, "test-actor", "reopening"); err != nil {
		t.Fatalf("runUnarchive failed: %v", err)
	}

	// 2. Test runArchiveList
	if err := runArchiveList(); err != nil {
		t.Fatalf("runArchiveList failed: %v", err)
	}

	// 3. Test runArchiveList with JSON flag
	jsonFlag = true
	if err := runArchiveList(); err != nil {
		t.Fatalf("runArchiveList with json failed: %v", err)
	}

	// 4. Test runUnarchive with JSON flag
	if err := runUnarchive([]string{"T-1"}, "test-actor", "reopening"); err != nil {
		t.Fatalf("runUnarchive with json failed: %v", err)
	}
	jsonFlag = false
}
