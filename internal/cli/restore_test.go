package cli

import (
	"bytes"
	"testing"
)

func TestRestoreCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"restore"})
	if err != nil || cmd == nil {
		t.Fatalf("restore command not registered: %v", err)
	}

	if cmd.Name() != "restore" {
		t.Errorf("expected command name restore, got %s", cmd.Name())
	}

	flag := cmd.Flags().Lookup("list")
	if flag == nil {
		t.Error("expected --list flag on restore command")
	}

	shorthand := cmd.Flags().ShorthandLookup("l")
	if shorthand == nil {
		t.Error("expected -l shorthand flag on restore command")
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("failed to render restore help: %v", err)
	}
}

func TestBackupCommandFlags(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"backup"})
	if err != nil || cmd == nil {
		t.Fatalf("backup command not registered: %v", err)
	}

	flag := cmd.Flags().Lookup("list")
	if flag == nil {
		t.Error("expected --list flag on backup command")
	}

	shorthand := cmd.Flags().ShorthandLookup("l")
	if shorthand == nil {
		t.Error("expected -l shorthand flag on backup command")
	}
}
