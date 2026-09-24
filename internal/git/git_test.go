package git

import (
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
	if !strings.Contains(root, "task_cli") {
		t.Errorf("expected root to contain task_cli, got %q", root)
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
