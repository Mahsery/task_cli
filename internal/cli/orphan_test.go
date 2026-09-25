package cli

import (
	"bytes"
	"testing"
)

func TestOrphanCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"orphan"})
	if err != nil || cmd == nil {
		t.Fatalf("orphan command not registered: %v", err)
	}

	if cmd.Name() != "orphan" {
		t.Errorf("expected command name orphan, got %s", cmd.Name())
	}

	flags := []string{"older-than", "message", "actor", "dry-run", "all", "list", "backport"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on orphan command", f)
		}
	}

	olderThanFlag := cmd.Flags().Lookup("older-than")
	if olderThanFlag == nil || olderThanFlag.DefValue != "1h" {
		t.Errorf("expected --older-than default '1h', got %v", olderThanFlag)
	}

	if cmd.Flags().ShorthandLookup("m") == nil {
		t.Error("expected -m shorthand on orphan command")
	}
	if cmd.Flags().ShorthandLookup("l") == nil {
		t.Error("expected -l shorthand on orphan command")
	}

	// Verify subcommands
	reapCmd, _, err := RootCmd.Find([]string{"orphan", "reap"})
	if err != nil || reapCmd == nil || reapCmd.Name() != "reap" {
		t.Errorf("expected 'orphan reap' subcommand, got %v", reapCmd)
	}

	listCmdSub, _, err := RootCmd.Find([]string{"orphan", "list"})
	if err != nil || listCmdSub == nil || listCmdSub.Name() != "list" {
		t.Errorf("expected 'orphan list' subcommand, got %v", listCmdSub)
	}

	backportCmdSub, _, err := RootCmd.Find([]string{"orphan", "backport"})
	if err != nil || backportCmdSub == nil || backportCmdSub.Name() != "backport" {
		t.Errorf("expected 'orphan backport' subcommand, got %v", backportCmdSub)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("failed to render orphan help: %v", err)
	}
}

func TestOrphanBackportCommand(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"orphan", "backport"})
	if err != nil || cmd == nil {
		t.Fatalf("orphan backport command not registered: %v", err)
	}

	if cmd.Name() != "backport" {
		t.Errorf("expected command name backport, got %s", cmd.Name())
	}

	flags := []string{"older-than", "message", "actor", "dry-run"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on orphan backport command", f)
		}
	}

	olderThanFlag := cmd.Flags().Lookup("older-than")
	if olderThanFlag == nil || olderThanFlag.DefValue != "1h" {
		t.Errorf("expected --older-than default '1h', got %v", olderThanFlag)
	}

	if cmd.Flags().ShorthandLookup("m") == nil {
		t.Error("expected -m shorthand on orphan backport command")
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("failed to render orphan backport help: %v", err)
	}
}

func TestTakeoverCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"takeover"})
	if err != nil || cmd == nil {
		t.Fatalf("takeover command not registered: %v", err)
	}

	if cmd.Name() != "takeover" {
		t.Errorf("expected command name takeover, got %s", cmd.Name())
	}

	// Test alias 'take'
	aliasCmd, _, err := RootCmd.Find([]string{"take"})
	if err != nil || aliasCmd == nil || aliasCmd.Name() != "takeover" {
		t.Errorf("expected 'take' alias to find takeover command, got %v", aliasCmd)
	}

	flags := []string{"message", "actor", "force"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on takeover command", f)
		}
	}
	if cmd.Flags().ShorthandLookup("m") == nil {
		t.Error("expected -m shorthand on takeover command")
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("failed to render takeover help: %v", err)
	}
}

func TestTransferCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"transfer"})
	if err != nil || cmd == nil {
		t.Fatalf("transfer command not registered: %v", err)
	}

	if cmd.Name() != "transfer" {
		t.Errorf("expected command name transfer, got %s", cmd.Name())
	}

	flags := []string{"to", "message", "actor", "force"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag on transfer command", f)
		}
	}
	if cmd.Flags().ShorthandLookup("m") == nil {
		t.Error("expected -m shorthand on transfer command")
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("failed to render transfer help: %v", err)
	}
}

func TestListCommandOrphanedFlag(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"list"})
	if err != nil || cmd == nil {
		t.Fatalf("list command not registered: %v", err)
	}

	if cmd.Flags().Lookup("orphaned") == nil {
		t.Error("expected --orphaned flag on list command")
	}
}
