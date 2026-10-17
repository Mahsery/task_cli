package cli

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"task_cli/internal/client"
	"task_cli/internal/config"
	"task_cli/internal/daemon"
	"task_cli/internal/storage/sqlite"
)

func setupRealDaemonForCLITest(t *testing.T) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "tk-ux-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	sockPath := filepath.Join(tmpDir, "tk.sock")
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dCfg := &config.Config{
		GlobalDir:      filepath.Join(tmpDir, "global"),
		SocketPath:     sockPath,
		ExplicitSocket: true,
		Actor:          "test-agent",
	}
	srv := daemon.NewServer(dCfg)
	go func() {
		_ = srv.Start()
	}()
	t.Cleanup(func() {
		srv.Stop()
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("unix", sockPath, 50*time.Millisecond); err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	origClient := taskClient
	origCfg := cfg
	origScope := scope
	origRepoRoot := repoRoot
	origJSON := jsonFlag
	t.Cleanup(func() {
		taskClient = origClient
		cfg = origCfg
		scope = origScope
		repoRoot = origRepoRoot
		jsonFlag = origJSON
	})

	cfg = dCfg
	scope = "local"
	repoRoot = repoDir
	jsonFlag = false
	taskClient = client.New(dCfg, scope, repoRoot)
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	runErr := fn()

	_ = w.Close()
	os.Stdout = origStdout
	out, _ := io.ReadAll(r)
	_ = r.Close()
	return string(out), runErr
}

func TestReadPipedStdinNonBlockingOnIdlePipe(t *testing.T) {
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
		_ = w.Close()
	}()

	start := time.Now()
	text, ok := ReadPipedStdin(true)
	elapsed := time.Since(start)

	if ok || text != "" {
		t.Fatalf("expected ReadPipedStdin on idle open pipe to return ('', false), got (%q, %v)", text, ok)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("ReadPipedStdin took too long on idle open pipe: %v", elapsed)
	}
}

func TestReadPipedStdinReadsActualPipeContent(t *testing.T) {
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
	}()

	_, _ = w.WriteString("Piped Title\nPiped Description Body\n")
	_ = w.Close()

	text, ok := ReadPipedStdin(false)
	if !ok || text != "Piped Title\nPiped Description Body" {
		t.Fatalf("unexpected ReadPipedStdin result: (%q, %v)", text, ok)
	}
}

func TestNonInteractiveEditorSkipAndIdleStdinPipe(t *testing.T) {
	setupRealDaemonForCLITest(t)
	t.Setenv("TERM", "dumb")

	// Simulate an open, idle stdin pipe (common in non-interactive agent shells without </dev/null)
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
		_ = w.Close()
	}()

	// 1. 'tk new' with no flags and an idle open pipe in non-interactive terminal must NOT launch $EDITOR or hang
	newTitle = ""
	newDescription = ""
	newQuiet = false
	err = newCmd.RunE(newCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "non-interactive terminal") {
		t.Fatalf("expected non-interactive error from 'tk new' with no title, got: %v", err)
	}

	// 2. 'tk new -t "Task"' with an open idle stdin pipe must succeed quickly without needing </dev/null
	_ = newCmd.Flags().Set("title", "Created without devnull")
	defer func() {
		newTitle = ""
		if f := newCmd.Flags().Lookup("title"); f != nil {
			f.Changed = false
		}
	}()
	start := time.Now()
	out, err := captureStdout(t, func() error {
		return newCmd.RunE(newCmd, nil)
	})
	if err != nil {
		t.Fatalf("expected 'tk new -t' to succeed with open idle stdin pipe, got: %v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("'tk new -t' hung on idle pipe: %v", time.Since(start))
	}
	if !strings.Contains(out, "T-1") {
		t.Fatalf("expected T-1 in output, got: %s", out)
	}

	// 3. 'tk edit T-1' with no modification flags in non-interactive terminal must NOT launch $EDITOR
	err = editCmd.RunE(editCmd, []string{"T-1"})
	if err == nil || !strings.Contains(err.Error(), "non-interactive terminal") {
		t.Fatalf("expected non-interactive error from 'tk edit T-1' with no flags, got: %v", err)
	}

	// 4. 'tk review T-1 -R reviewer-bot' with an open idle stdin pipe must not hang
	reviewReviewer = "reviewer-bot"
	reviewNote = ""
	reviewNoteAlt = ""
	defer func() { reviewReviewer = "" }()
	_, err = captureStdout(t, func() error {
		return reviewCmd.RunE(reviewCmd, []string{"T-1"})
	})
	if err != nil {
		t.Fatalf("expected 'tk review' to succeed with open idle stdin pipe, got: %v", err)
	}
}

func TestQuietFlagAndPositionalTitleScripting(t *testing.T) {
	setupRealDaemonForCLITest(t)
	t.Setenv("TERM", "dumb")

	// Reset flags after test
	defer func() {
		newQuiet = false
		newTitle = ""
		postQuiet = false
		postTitle = ""
		promoteQuiet = false
		nextQuiet = false
		claimQuiet = false
		editQuiet = false
		replyQuiet = false
		replyMessage = ""
		reviewQuiet = false
		reviewReviewer = ""
		doneQuiet = false
		doneNoCommit = false
		listIDsOnly = false
	}()

	// 1. tk new -q "First Task" (positional title + quiet ID output)
	newQuiet = true
	out, err := captureStdout(t, func() error {
		return newCmd.RunE(newCmd, []string{"First", "Task"})
	})
	if err != nil {
		t.Fatalf("tk new -q failed: %v", err)
	}
	if strings.TrimSpace(out) != "T-1" {
		t.Fatalf("expected exact 'T-1' from tk new -q, got %q", out)
	}

	// 2. tk post -q "First Bug Report" (positional title + quiet ID output)
	postQuiet = true
	out, err = captureStdout(t, func() error {
		return postCmd.RunE(postCmd, []string{"First Bug Report"})
	})
	if err != nil {
		t.Fatalf("tk post -q failed: %v", err)
	}
	if strings.TrimSpace(out) != "P-1" {
		t.Fatalf("expected exact 'P-1' from tk post -q, got %q", out)
	}

	// 3. tk promote -q P-1
	promoteQuiet = true
	out, err = captureStdout(t, func() error {
		return promoteCmd.RunE(promoteCmd, []string{"P-1"})
	})
	if err != nil {
		t.Fatalf("tk promote -q failed: %v", err)
	}
	if strings.TrimSpace(out) != "T-2" {
		t.Fatalf("expected exact 'T-2' from tk promote -q, got %q", out)
	}

	// 4. tk next -q
	nextQuiet = true
	out, err = captureStdout(t, func() error {
		return nextCmd.RunE(nextCmd, nil)
	})
	if err != nil {
		t.Fatalf("tk next -q failed: %v", err)
	}
	if strings.TrimSpace(out) != "T-1" {
		t.Fatalf("expected 'T-1' from tk next -q, got %q", out)
	}

	// 5. tk claim -q T-1
	claimQuiet = true
	out, err = captureStdout(t, func() error {
		return claimCmd.RunE(claimCmd, []string{"T-1"})
	})
	if err != nil {
		t.Fatalf("tk claim -q failed: %v", err)
	}
	if strings.TrimSpace(out) != "T-1" {
		t.Fatalf("expected 'T-1' from tk claim -q, got %q", out)
	}

	// 6. tk reply -q T-1 -m "progress note"
	replyQuiet = true
	replyMessage = "progress note"
	out, err = captureStdout(t, func() error {
		return replyCmd.RunE(replyCmd, []string{"T-1"})
	})
	if err != nil {
		t.Fatalf("tk reply -q failed: %v", err)
	}
	if strings.TrimSpace(out) != "T-1" {
		t.Fatalf("expected 'T-1' from tk reply -q, got %q", out)
	}

	// 7. tk review -q T-1 -R qa-bot
	reviewQuiet = true
	reviewReviewer = "qa-bot"
	out, err = captureStdout(t, func() error {
		return reviewCmd.RunE(reviewCmd, []string{"T-1"})
	})
	if err != nil {
		t.Fatalf("tk review -q failed: %v", err)
	}
	if strings.TrimSpace(out) != "T-1" {
		t.Fatalf("expected 'T-1' from tk review -q, got %q", out)
	}

	// 8. tk done -q T-1 --no-commit
	doneQuiet = true
	doneNoCommit = true
	out, err = captureStdout(t, func() error {
		return doneCmd.RunE(doneCmd, []string{"T-1"})
	})
	if err != nil {
		t.Fatalf("tk done -q failed: %v", err)
	}
	if strings.TrimSpace(out) != "T-1" {
		t.Fatalf("expected 'T-1' from tk done -q, got %q", out)
	}

	// 9. tk list --ids
	listIDsOnly = true
	out, err = captureStdout(t, func() error {
		return listCmd.RunE(listCmd, nil)
	})
	if err != nil {
		t.Fatalf("tk list --ids failed: %v", err)
	}
	lines := strings.Fields(out)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 IDs from tk list --ids, got: %q", out)
	}
}

func TestSnapshotAndExportCommands(t *testing.T) {
	setupRealDaemonForCLITest(t)
	t.Setenv("TERM", "dumb")

	defer func() {
		newQuiet = false
		snapshotOutput = ""
		snapshotFormat = ""
		snapshotQuiet = false
	}()

	newQuiet = true
	_, err := captureStdout(t, func() error {
		return newCmd.RunE(newCmd, []string{"Snapshot Test Task"})
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	// 1. Export portable SQLite snapshot via 'tk snapshot <path.db> -q'
	outDir := t.TempDir()
	dbSnapPath := filepath.Join(outDir, "portable.db")
	snapshotQuiet = true
	out, err := captureStdout(t, func() error {
		return snapshotCmd.RunE(snapshotCmd, []string{dbSnapPath})
	})
	if err != nil {
		t.Fatalf("tk snapshot sqlite failed: %v", err)
	}
	if strings.TrimSpace(out) != dbSnapPath {
		t.Fatalf("expected snapshot path %q, got %q", dbSnapPath, strings.TrimSpace(out))
	}
	if err := sqlite.VerifyBackupFile(dbSnapPath); err != nil {
		t.Fatalf("VerifyBackupFile on exported snapshot failed: %v", err)
	}

	// 2. Export JSON dump via 'tk export <path.json>'
	jsonSnapPath := filepath.Join(outDir, "portable.json")
	snapshotQuiet = true
	outJSONPath, err := captureStdout(t, func() error {
		return exportCmd.RunE(exportCmd, []string{jsonSnapPath})
	})
	if err != nil {
		t.Fatalf("tk export json failed: %v", err)
	}
	if strings.TrimSpace(outJSONPath) != jsonSnapPath {
		t.Fatalf("expected json path %q, got %q", jsonSnapPath, strings.TrimSpace(outJSONPath))
	}
	rawJSON, err := os.ReadFile(jsonSnapPath)
	if err != nil {
		t.Fatalf("read exported json failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(rawJSON, &parsed); err != nil {
		t.Fatalf("unmarshal exported json failed: %v", err)
	}
	items, ok := parsed["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 exported item in JSON dump, got: %+v", parsed["items"])
	}
}

func TestNonInteractiveConfirmationGuards(t *testing.T) {
	setupRealDaemonForCLITest(t)
	t.Setenv("TERM", "dumb")

	newQuiet = true
	_, err := captureStdout(t, func() error {
		return newCmd.RunE(newCmd, []string{"Task to guard delete"})
	})
	newQuiet = false
	if err != nil {
		t.Fatal(err)
	}

	deleteForce = false
	err = deleteCmd.RunE(deleteCmd, []string{"T-1"})
	if err == nil || !strings.Contains(err.Error(), "non-interactive terminal") {
		t.Fatalf("expected non-interactive guard error from deleteCmd, got: %v", err)
	}

	topicRmForce = false
	err = topicRmCmd.RunE(topicRmCmd, []string{"some-topic"})
	if err == nil || !strings.Contains(err.Error(), "non-interactive terminal") {
		t.Fatalf("expected non-interactive guard error from topicRmCmd, got: %v", err)
	}
}
