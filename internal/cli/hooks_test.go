package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookInstallAndUninstallPreservesUserHooks(t *testing.T) {
	dir := t.TempDir()
	hookPath := filepath.Join(dir, "prepare-commit-msg")

	block := strings.Join([]string{
		tkHookBegin,
		"tk hooks run prepare-commit-msg \"$1\"",
		tkHookEnd,
	}, "\n")

	// 1. Fresh install creates executable script with shebang
	if err := installHookBlock(hookPath, block); err != nil {
		t.Fatalf("installHookBlock failed: %v", err)
	}
	if !hasHookBlock(hookPath) {
		t.Fatal("expected hasHookBlock=true after install")
	}
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("expected hook script to be executable, got mode %v", info.Mode())
	}

	// 2. Uninstall on a file with only tk hook removes the file cleanly
	if err := uninstallHookBlock(hookPath); err != nil {
		t.Fatalf("uninstallHookBlock failed: %v", err)
	}
	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Errorf("expected empty hook file to be removed on uninstall, stat err=%v", err)
	}

	// 3. Install alongside pre-existing user hook content preserves user content
	userScript := "#!/usr/bin/env sh\necho 'custom user hook'\n"
	if err := os.WriteFile(hookPath, []byte(userScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installHookBlock(hookPath, block); err != nil {
		t.Fatal(err)
	}
	// Re-installing is idempotent (does not duplicate block)
	if err := installHookBlock(hookPath, block); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), tkHookBegin) != 1 {
		t.Fatalf("expected exactly 1 tk hook block after idempotent install, got:\n%s", string(raw))
	}
	if !strings.Contains(string(raw), "custom user hook") {
		t.Fatalf("expected user hook content to be preserved, got:\n%s", string(raw))
	}

	// 4. Uninstall strips tk block while leaving user hook intact
	if err := uninstallHookBlock(hookPath); err != nil {
		t.Fatal(err)
	}
	afterRaw, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("expected user hook file to remain after uninstall: %v", err)
	}
	if strings.Contains(string(afterRaw), tkHookBegin) || !strings.Contains(string(afterRaw), "custom user hook") {
		t.Fatalf("unexpected content after uninstall:\n%s", string(afterRaw))
	}
}

func TestInjectTaskIDIntoCommitMessage(t *testing.T) {
	msg, changed := injectTaskIDIntoCommitMessage("feat: add branch inference\n\n# Please enter commit message\n", "T-84")
	if !changed || !strings.HasPrefix(msg, "feat: add branch inference [T-84]\n") {
		t.Fatalf("expected [T-84] suffix on subject line, got changed=%v msg=%q", changed, msg)
	}

	// Already tagged (case-insensitive) -> unchanged
	_, changed = injectTaskIDIntoCommitMessage("feat(git): support t-84 branch binding\n", "T-84")
	if changed {
		t.Fatal("expected already-referenced task ID not to be injected twice")
	}

	// Empty message with comments -> inserts "[T-84] " at first non-comment line
	msg, changed = injectTaskIDIntoCommitMessage("\n# comment line\n", "T-84")
	if !changed || !strings.HasPrefix(msg, "[T-84] \n# comment line") {
		t.Fatalf("unexpected empty message injection: changed=%v msg=%q", changed, msg)
	}
}
