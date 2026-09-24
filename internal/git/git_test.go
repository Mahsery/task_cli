package git

import (
	"os"
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

