package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRepoRoot(t *testing.T) {
	root, err := FindRepoRoot()
	if err != nil {
		t.Fatalf("unexpected error finding repo root: %v", err)
	}
	if root == "" {
		t.Fatal("expected non-empty repo root")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected root %q to be an existing directory", root)
	}
}

func TestWorktreeSharedRepoRootAndDiff(t *testing.T) {
	baseDir := t.TempDir()
	mainRepo := filepath.Join(baseDir, "main-repo")
	wtRepo := filepath.Join(baseDir, "wt-feature")

	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}

	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(mainRepo, "init", "-b", "main")
	runGit(mainRepo, "config", "user.name", "Test User")
	runGit(mainRepo, "config", "user.email", "test@example.com")

	if err := os.WriteFile(filepath.Join(mainRepo, "file.txt"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(mainRepo, "add", "file.txt")
	runGit(mainRepo, "commit", "-m", "initial commit")

	// Add a linked worktree on branch feature/wt
	runGit(mainRepo, "worktree", "add", "-b", "feature/wt", wtRepo)

	// 1. FindRepoRootInDir on the linked worktree must return mainRepo!
	gotCommonRoot, err := FindRepoRootInDir(wtRepo)
	if err != nil {
		t.Fatalf("FindRepoRootInDir(wtRepo) failed: %v", err)
	}
	if gotCommonRoot != filepath.Clean(mainRepo) {
		t.Fatalf("expected shared repo root %q for linked worktree, got %q", mainRepo, gotCommonRoot)
	}

	// 2. FindWorktreeRootInDir on the linked worktree must return wtRepo!
	gotWtRoot, err := FindWorktreeRootInDir(wtRepo)
	if err != nil {
		t.Fatalf("FindWorktreeRootInDir(wtRepo) failed: %v", err)
	}
	if gotWtRoot != filepath.Clean(wtRepo) {
		t.Fatalf("expected worktree root %q, got %q", wtRepo, gotWtRoot)
	}

	// 3. Commit in the linked worktree and verify CurrentBranch + GetLatestCommit when CWD is wtRepo
	if err := os.WriteFile(filepath.Join(wtRepo, "file.txt"), []byte("v2 in worktree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(wtRepo, "commit", "-am", "worktree commit")

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()
	if err := os.Chdir(wtRepo); err != nil {
		t.Fatal(err)
	}

	branch, err := CurrentBranch(mainRepo)
	if err != nil || branch != "feature/wt" {
		t.Fatalf("expected CurrentBranch='feature/wt' from worktree, got %q (err=%v)", branch, err)
	}

	latest, err := GetLatestCommit(mainRepo)
	if err != nil {
		t.Fatalf("GetLatestCommit(mainRepo) from worktree failed: %v", err)
	}
	if latest.Subject != "worktree commit" {
		t.Fatalf("expected worktree HEAD subject 'worktree commit', got %q", latest.Subject)
	}

	// 4. Dirty working tree diff inside linked worktree
	if err := os.WriteFile(filepath.Join(wtRepo, "file.txt"), []byte("v3 dirty in worktree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wDiff, err := GetWorkingDiff(mainRepo)
	if err != nil {
		t.Fatalf("GetWorkingDiff(mainRepo) from worktree failed: %v", err)
	}
	if !strings.Contains(wDiff.DiffContent, "v3 dirty in worktree") {
		t.Fatalf("expected working diff from linked worktree, got:\n%s", wDiff.DiffContent)
	}
}

func TestGetAuthor(t *testing.T) {
	author := GetAuthor()
	if author == "" {
		t.Fatal("expected author to be non-empty")
	}
}

func TestFormatKernelPatch(t *testing.T) {
	info := &CommitInfo{
		SHA:         "136825d192837482",
		Author:      "Mehmet Koseoglu <mehmet.mkoseoglu@gmail.com>",
		Date:        "Thu Sep 24 07:10:00 2026 +0300",
		Subject:     "feat: implement daemon and socket messaging",
		Body:        "Add unix socket server and client auto-spawning.",
		DiffStat:    "1 file changed, 10 insertions(+)",
		DiffContent: "diff --git a/main.go b/main.go...",
	}

	formatted := FormatKernelPatch(info)
	if !strings.Contains(formatted, "commit 136825d192837482") {
		t.Errorf("expected commit SHA in output, got: %s", formatted)
	}
	if !strings.Contains(formatted, "Author: Mehmet Koseoglu") {
		t.Errorf("expected author in output, got: %s", formatted)
	}
	if !strings.Contains(formatted, "feat: implement daemon and socket messaging") {
		t.Errorf("expected subject in output, got: %s", formatted)
	}
	if !strings.Contains(formatted, "1 file changed, 10 insertions(+)") {
		t.Errorf("expected diffstat in output, got: %s", formatted)
	}
}

func TestSanitizeDiffContent(t *testing.T) {
	smallDiff := []byte("diff --git a/test.txt b/test.txt\n+hello")
	sanitized := sanitizeDiffContent(smallDiff)
	if sanitized != string(smallDiff) {
		t.Errorf("expected small diff to remain intact, got: %q", sanitized)
	}

	largeDiff := make([]byte, MaxDiffContentBytes+1000)
	for i := range largeDiff {
		largeDiff[i] = 'a'
	}
	sanitizedLarge := sanitizeDiffContent(largeDiff)
	if !strings.Contains(sanitizedLarge, "diff truncated by tk at 256KB") {
		t.Errorf("expected large diff to be truncated, got length: %d", len(sanitizedLarge))
	}
	if len(sanitizedLarge) > MaxDiffContentBytes+200 {
		t.Errorf("truncated diff exceeded expected bounds: %d", len(sanitizedLarge))
	}
}

func TestSanitizeDiffContentMultiByteUTF8(t *testing.T) {
	// Pad with 'x' until 1 byte before MaxDiffContentBytes, then append a 4-byte rune (e.g. rocket emoji 🚀: 0xF0 0x9F 0x9A 0x80)
	pad := strings.Repeat("x", MaxDiffContentBytes-1)
	diff := []byte(pad + "🚀 extra content")
	sanitized := sanitizeDiffContent(diff)

	// Verify that sanitized is valid UTF-8
	for i, r := range sanitized {
		if r == '\uFFFD' {
			t.Fatalf("sanitized diff contains replacement character at index %d, indicating split UTF-8 rune", i)
		}
	}
}
