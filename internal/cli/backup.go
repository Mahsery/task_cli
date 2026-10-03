package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	backupMaxSnapshots int
	backupForceRestore bool
	backupListFlag     bool
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage task database backups and disaster recovery",
	Long: `Inspect, create, verify, and restore atomic SQLite snapshots of the tasklog.
Backups use SQLite's native VACUUM INTO for non-blocking point-in-time recovery.`,
	Example: `  tk backup                     # List available snapshots
  tk backup -l                  # List available snapshots
  tk backup create              # Create atomic rolling snapshot
  tk backup create my-milestone # Create named snapshot
  tk backup verify              # Run SQLite integrity_check on active database
  tk backup restore <file> -f   # Restore snapshot without confirmation`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackupList()
	},
}

var backupListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List available database backups",
	Aliases: []string{"ls"},
	Long:    `Display all point-in-time backup snapshots in the .tasks/backups directory with size and age.`,
	Example: `  tk backup list
  tk backup list --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackupList()
	},
}

func runBackupList() error {
	backups, err := taskClient.ListBackups()
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	if jsonFlag {
		return outputJSON(backups)
	}

	if len(backups) == 0 {
		fmt.Printf("No backups found for %s scope.\n", taskClient.Scope)
		return nil
	}

	fmt.Printf("%-32s %-10s %-18s %-10s %s\n", "BACKUP NAME", "SIZE", "CREATED", "LOCATION", "PATH")
	fmt.Println(strings.Repeat("─", 98))
	for i, b := range backups {
		sizeStr := formatFileSize(b.Size)
		ageStr := formatAge(b.ModTime)
		if i == 0 {
			ageStr += " (latest)"
		}
		locStr := b.Location
		if locStr == "" {
			locStr = "local"
		}
		fmt.Printf("%-32s %-10s %-18s %-10s %s\n", b.Name, sizeStr, ageStr, locStr, b.Path)
	}
	return nil
}

var backupCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create an atomic point-in-time backup snapshot",
	Long:  `Create a clean, consistent SQLite snapshot of the active database using VACUUM INTO without interrupting concurrent daemon queries.`,
	Example: `  tk backup create
  tk backup create pre-refactor --keep 10`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}

		path, err := taskClient.CreateBackup(name, backupMaxSnapshots)
		if err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}

		if jsonFlag {
			return outputJSON(map[string]any{
				"created": true,
				"path":    path,
				"scope":   taskClient.Scope,
			})
		}

		fmt.Printf("✓ Created backup snapshot: %s\n", path)
		return nil
	},
}

var backupVerifyCmd = &cobra.Command{
	Use:   "verify [file]",
	Short: "Verify SQLite database or backup file integrity",
	Long:  `Run a full SQLite integrity_check on the active repository database or a specified backup file.`,
	Example: `  tk backup verify
  tk backup verify .tasks/backups/snapshot-20260924-120000.db`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) > 0 {
			target = args[0]
		}

		msg, err := taskClient.VerifyBackup(target)
		if err != nil {
			return fmt.Errorf("integrity check failed: %w", err)
		}

		if jsonFlag {
			return outputJSON(map[string]any{
				"status":  "ok",
				"message": msg,
			})
		}

		fmt.Println(msg)
		return nil
	},
}

var backupRestoreCmd = &cobra.Command{
	Use:   "restore [backup-file-or-name|latest]",
	Short: "Restore task database from a backup snapshot (central or local)",
	Long: `Safely overwrite the active database with a verified backup snapshot.
If no argument or 'latest' is given, the most recent snapshot across central and local vaults is restored.
In interactive mode, a confirmation prompt is displayed unless --force is supplied.`,
	Example: `  tk backup restore latest
  tk backup restore latest -f
  tk backup restore snapshot-20260926-074900.db --force
  tk backup restore .tasks/backups/snapshot-20260924-120000.db`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := "latest"
		if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
			target = args[0]
		}
		return RunBackupRestore(target, backupForceRestore)
	},
}

func RunBackupRestore(backupPath string, force bool) error {
	if backupPath == "" {
		backupPath = "latest"
	}

	if !filepath.IsAbs(backupPath) && backupPath != "latest" {
		abs, err := filepath.Abs(backupPath)
		if err == nil {
			if stat, err := os.Stat(abs); err == nil && !stat.IsDir() {
				backupPath = abs
			}
		}
	}

	if !force {
		if !IsInteractiveTerminal() {
			return fmt.Errorf("refusing to restore without confirmation in non-interactive environment (use --force)")
		}
		targetDesc := backupPath
		if backupPath == "latest" {
			targetDesc = "the most recent available snapshot (central or local vault)"
		}
		fmt.Printf("WARNING: Restoring will overwrite the current %s database with:\n  %s\n", taskClient.Scope, targetDesc)
		fmt.Print("Proceed with restore? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		text, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		text = strings.ToLower(strings.TrimSpace(text))
		if text != "y" && text != "yes" {
			fmt.Println("Restore cancelled.")
			return nil
		}
	}

	msg, err := taskClient.RestoreBackup(backupPath)
	if err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	if jsonFlag {
		return outputJSON(map[string]any{
			"restored": true,
			"message":  msg,
		})
	}

	fmt.Println(msg)
	return nil
}

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management, backups, and disaster recovery (alias for tk backup)",
	Long:  `Inspect, verify, snapshot, and restore the task database from local and central vaults.`,
	Example: `  tk db restore latest    # Restore database from newest backup
  tk db list              # List all available snapshots
  tk db backup            # Create an atomic snapshot`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackupList()
	},
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore [backup-file-or-name|latest]",
	Short: "Restore task database from a backup snapshot (alias for tk backup restore)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  backupRestoreCmd.RunE,
}

var dbBackupCmd = &cobra.Command{
	Use:   "backup [name]",
	Short: "Create an atomic point-in-time backup snapshot (alias for tk backup create)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  backupCreateCmd.RunE,
}

var dbListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available database backups (alias for tk backup list)",
	RunE:  backupListCmd.RunE,
}

var dbVerifyCmd = &cobra.Command{
	Use:   "verify [file]",
	Short: "Verify SQLite database or backup file integrity (alias for tk backup verify)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  backupVerifyCmd.RunE,
}

func formatFileSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}

func init() {
	backupCmd.Flags().BoolVarP(&backupListFlag, "list", "l", false, "List available backups")
	backupCreateCmd.Flags().IntVar(&backupMaxSnapshots, "keep", 5, "Number of snapshots to retain before pruning")
	backupRestoreCmd.Flags().BoolVarP(&backupForceRestore, "force", "f", false, "Force restore without interactive confirmation")

	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupCreateCmd)
	backupCmd.AddCommand(backupVerifyCmd)
	backupCmd.AddCommand(backupRestoreCmd)

	dbRestoreCmd.Flags().BoolVarP(&backupForceRestore, "force", "f", false, "Force restore without interactive confirmation")
	dbBackupCmd.Flags().IntVar(&backupMaxSnapshots, "keep", 5, "Number of snapshots to retain before pruning")

	dbCmd.AddCommand(dbListCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbVerifyCmd)
	dbCmd.AddCommand(dbRestoreCmd)
	RootCmd.AddCommand(dbCmd)
}
