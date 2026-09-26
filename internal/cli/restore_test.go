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

func TestRestoreDBFlags(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"restore"})
	if err != nil || cmd == nil {
		t.Fatalf("restore command not registered: %v", err)
	}

	dbFlag := cmd.Flags().Lookup("db")
	if dbFlag == nil {
		t.Error("expected --db flag on restore command")
	}
	dbShort := cmd.Flags().ShorthandLookup("d")
	if dbShort == nil {
		t.Error("expected -d shorthand flag on restore command")
	}

	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Error("expected --force flag on restore command")
	}
}

func TestDBCommandRegistration(t *testing.T) {
	dbCmd, _, err := RootCmd.Find([]string{"db"})
	if err != nil || dbCmd == nil {
		t.Fatalf("tk db command not registered: %v", err)
	}

	for _, sub := range []string{"restore", "backup", "list", "verify"} {
		subCmd, _, err := RootCmd.Find([]string{"db", sub})
		if err != nil || subCmd == nil {
			t.Errorf("expected tk db %s subcommand to be registered", sub)
		}
	}
}

func TestDaemonStartAndRestartCommands(t *testing.T) {
	startCmd, _, err := RootCmd.Find([]string{"daemon", "start"})
	if err != nil || startCmd == nil {
		t.Fatalf("tk daemon start not registered: %v", err)
	}

	restartCmd, _, err := RootCmd.Find([]string{"daemon", "restart"})
	if err != nil || restartCmd == nil {
		t.Fatalf("tk daemon restart not registered: %v", err)
	}

	if startCmd.Flags().Lookup("socket") == nil {
		t.Error("expected --socket flag on daemon start")
	}
	if restartCmd.Flags().Lookup("socket") == nil {
		t.Error("expected --socket flag on daemon restart")
	}
}

