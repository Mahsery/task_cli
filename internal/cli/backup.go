package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var (
	backupMaxSnapshots int
	backupForceRestore bool
	backupListFlag     bool
	backupQuiet        bool
	snapshotOutput     string
	snapshotFormat     string
	snapshotQuiet      bool
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage task database backups and disaster recovery",
	Long: `Inspect, create, verify, export, and restore atomic SQLite snapshots of the tasklog.
Backups use SQLite's native VACUUM INTO for non-blocking point-in-time recovery and WAL-free portability.`,
	Example: `  tk backup                     # List available snapshots
  tk backup -l                  # List available snapshots
  tk backup create              # Create atomic rolling snapshot
  tk backup create my-milestone # Create named snapshot
  tk backup create ./tasks.db   # Export standalone WAL-free SQLite file to path
  tk backup verify              # Run SQLite integrity_check on active database
  tk backup restore <file> -f   # Restore snapshot without confirmation`,
	RunE: func(_ *cobra.Command, _ []string) error {
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
	RunE: func(_ *cobra.Command, _ []string) error {
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

	var sb strings.Builder
	fmt.Fprintf(&sb, "%-32s %-10s %-18s %-10s %s\n", "BACKUP NAME", "SIZE", "CREATED", "LOCATION", "PATH")
	fmt.Fprintln(&sb, strings.Repeat("─", 98))
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
		fmt.Fprintf(&sb, "%-32s %-10s %-18s %-10s %s\n", b.Name, sizeStr, ageStr, locStr, b.Path)
	}
	return PrintWithPager(sb.String())
}

func looksLikeFilePath(arg string) bool {
	if arg == "" {
		return false
	}
	if strings.ContainsRune(arg, '/') || strings.ContainsRune(arg, filepath.Separator) {
		return true
	}
	lower := strings.ToLower(arg)
	return strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".sqlite3")
}

var backupCreateCmd = &cobra.Command{
	Use:   "create [name-or-path]",
	Short: "Create an atomic point-in-time backup snapshot",
	Long: `Create a clean, consistent SQLite snapshot of the active database using VACUUM INTO without interrupting concurrent daemon queries.
If a file path (containing '/' or ending in .db/.sqlite/.sqlite3) or -o/--output is provided, writes a standalone WAL-free SQLite file directly to that path.`,
	Example: `  tk backup create
  tk backup create pre-refactor --keep 10
  tk backup create ./portable-tasks.db`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		if snapshotOutput != "" {
			return runSnapshotExport(snapshotOutput, "sqlite", backupQuiet)
		}
		if looksLikeFilePath(arg) {
			return runSnapshotExport(arg, "sqlite", backupQuiet)
		}

		path, err := taskClient.CreateBackup(arg, backupMaxSnapshots)
		if err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}

		if backupQuiet {
			fmt.Println(path)
			return nil
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

func runSnapshotExport(dest, format string, quiet bool) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		if strings.HasSuffix(strings.ToLower(dest), ".json") {
			format = "json"
		} else {
			format = "sqlite"
		}
	}

	if format == "json" {
		return exportJSONSnapshot(dest, quiet)
	}
	if format != "sqlite" && format != "db" {
		return fmt.Errorf("unsupported export format %q (use 'sqlite' or 'json')", format)
	}

	var path string
	var err error
	if strings.TrimSpace(dest) == "" {
		path, err = taskClient.CreateBackup("", backupMaxSnapshots)
	} else {
		absDest, absErr := filepath.Abs(dest)
		if absErr != nil {
			return fmt.Errorf("failed to resolve output path %q: %w", dest, absErr)
		}
		path, err = taskClient.ExportSnapshot(absDest)
	}
	if err != nil {
		return fmt.Errorf("failed to export snapshot: %w", err)
	}

	if quiet {
		fmt.Println(path)
		return nil
	}
	if jsonFlag {
		return outputJSON(map[string]any{
			"exported": true,
			"format":   "sqlite",
			"path":     path,
			"scope":    taskClient.Scope,
		})
	}
	fmt.Printf("✓ Exported standalone SQLite snapshot: %s\n", path)
	return nil
}

func exportJSONSnapshot(dest string, quiet bool) error {
	topics, err := taskClient.ListTopics()
	if err != nil {
		return fmt.Errorf("failed to list topics: %w", err)
	}
	items, err := taskClient.ListTasks(domain.TaskFilter{
		Scope:           scope,
		RepoPath:        repoRoot,
		IncludeArchived: true,
	})
	if err != nil {
		return fmt.Errorf("failed to list items: %w", err)
	}
	if topics == nil {
		topics = []*domain.Topic{}
	}
	if items == nil {
		items = []*domain.Task{}
	}
	dump := map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"scope":       scope,
		"repo_path":   repoRoot,
		"topics":      topics,
		"items":       items,
	}

	if dest == "" || dest == "-" {
		return outputJSON(dump)
	}

	absDest, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("failed to resolve output path %q: %w", dest, err)
	}
	if err := os.MkdirAll(filepath.Dir(absDest), 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	data, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON snapshot: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(absDest, data, 0o600); err != nil {
		return fmt.Errorf("failed to write JSON snapshot: %w", err)
	}

	if quiet {
		fmt.Println(absDest)
		return nil
	}
	if jsonFlag {
		return outputJSON(map[string]any{
			"exported": true,
			"format":   "json",
			"path":     absDest,
			"scope":    taskClient.Scope,
		})
	}
	fmt.Printf("✓ Exported JSON snapshot: %s\n", absDest)
	return nil
}

var snapshotCmd = &cobra.Command{
	Use:   "snapshot [output-path]",
	Short: "Write a clean, standalone, WAL-free SQLite snapshot (or JSON dump) for safe copying",
	Long: `Writes a self-contained point-in-time snapshot of the live WAL database using SQLite's VACUUM INTO.
Because the live database runs in WAL mode, copying '.tasks/tasks.db' directly can miss uncommitted WAL frames.
'tk snapshot <path>' produces a single, WAL-free SQLite file (or JSON export with --format json) that can be
safely copied, archived, inspected with sqlite3, or restored later with 'tk db restore <path>'.

If no output path is given, a timestamped snapshot is created in .tasks/backups/ and its path is printed.`,
	Example: `  tk snapshot ./tasks-snapshot.db        # Write portable WAL-free SQLite file
  tk snapshot -o /tmp/tasks.db -q        # Write snapshot and print only the file path
  tk snapshot ./tasks.json               # Write full JSON export (topics + tasks + notes + commits)
  tk snapshot --format json              # Print full JSON export to stdout
  tk snapshot                            # Create timestamped snapshot in .tasks/backups/`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		dest := snapshotOutput
		if dest == "" && len(args) > 0 {
			dest = args[0]
		}
		return runSnapshotExport(dest, snapshotFormat, snapshotQuiet)
	},
}

var exportCmd = &cobra.Command{
	Use:   "export [output-path]",
	Short: "Export a standalone WAL-free SQLite database snapshot or JSON dump",
	Long: `Exports the task database to a standalone, WAL-free SQLite file (via VACUUM INTO) or a JSON document.
Ideal when you need to copy, archive, or inspect the database outside the live WAL environment.`,
	Example: `  tk export ./tasks-export.db            # Export standalone WAL-free SQLite file
  tk export ./tasks-export.json          # Export full JSON dump to file
  tk export --format json                # Export full JSON dump to stdout`,
	Args: cobra.MaximumNArgs(1),
	RunE: snapshotCmd.RunE,
}

var backupVerifyCmd = &cobra.Command{
	Use:   "verify [file]",
	Short: "Verify SQLite database or backup file integrity",
	Long:  `Run a full SQLite integrity_check on the active repository database or a specified backup file.`,
	Example: `  tk backup verify
  tk backup verify .tasks/backups/snapshot-20260924-120000.db`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
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
	RunE: func(_ *cobra.Command, args []string) error {
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
	Short: "Database management, snapshots, exports, and disaster recovery (alias for tk backup)",
	Long:  `Inspect, verify, snapshot, export, and restore the task database from local and central vaults.`,
	Example: `  tk db snapshot ./tasks.db # Write standalone WAL-free SQLite snapshot
  tk db export ./tasks.json # Export full database to JSON
  tk db restore latest      # Restore database from newest backup
  tk db list                # List all available snapshots
  tk db backup              # Create an atomic snapshot`,
	RunE: func(_ *cobra.Command, _ []string) error {
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
	Use:   "backup [name-or-path]",
	Short: "Create an atomic point-in-time backup snapshot (alias for tk backup create)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  backupCreateCmd.RunE,
}

var dbSnapshotCmd = &cobra.Command{
	Use:   "snapshot [output-path]",
	Short: "Write a standalone WAL-free SQLite snapshot or JSON export (alias for tk snapshot)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  snapshotCmd.RunE,
}

var dbExportCmd = &cobra.Command{
	Use:   "export [output-path]",
	Short: "Export a standalone WAL-free SQLite snapshot or JSON dump (alias for tk export)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  exportCmd.RunE,
}

var backupSnapshotCmd = &cobra.Command{
	Use:   "snapshot [output-path]",
	Short: "Write a standalone WAL-free SQLite snapshot or JSON export",
	Args:  cobra.MaximumNArgs(1),
	RunE:  snapshotCmd.RunE,
}

var backupExportCmd = &cobra.Command{
	Use:   "export [output-path]",
	Short: "Export a standalone WAL-free SQLite snapshot or JSON dump",
	Args:  cobra.MaximumNArgs(1),
	RunE:  exportCmd.RunE,
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

func bindSnapshotFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&snapshotOutput, "output", "o", "", "Output file path for the snapshot")
	cmd.Flags().StringVarP(&snapshotFormat, "format", "f", "", "Export format: 'sqlite' (default) or 'json'")
	cmd.Flags().BoolVarP(&snapshotQuiet, "quiet", "q", false, "Print only the exported file path")
}

func init() {
	backupCmd.Flags().BoolVarP(&backupListFlag, "list", "l", false, "List available backups")
	backupCreateCmd.Flags().IntVar(&backupMaxSnapshots, "keep", 5, "Number of snapshots to retain before pruning")
	backupCreateCmd.Flags().StringVarP(&snapshotOutput, "output", "o", "", "Write snapshot directly to this file path")
	backupCreateCmd.Flags().BoolVarP(&backupQuiet, "quiet", "q", false, "Print only the created snapshot file path")
	backupRestoreCmd.Flags().BoolVarP(&backupForceRestore, "force", "f", false, "Force restore without interactive confirmation")

	bindSnapshotFlags(snapshotCmd)
	bindSnapshotFlags(exportCmd)
	bindSnapshotFlags(backupSnapshotCmd)
	bindSnapshotFlags(backupExportCmd)
	bindSnapshotFlags(dbSnapshotCmd)
	bindSnapshotFlags(dbExportCmd)

	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupCreateCmd)
	backupCmd.AddCommand(backupSnapshotCmd)
	backupCmd.AddCommand(backupExportCmd)
	backupCmd.AddCommand(backupVerifyCmd)
	backupCmd.AddCommand(backupRestoreCmd)

	dbRestoreCmd.Flags().BoolVarP(&backupForceRestore, "force", "f", false, "Force restore without interactive confirmation")
	dbBackupCmd.Flags().IntVar(&backupMaxSnapshots, "keep", 5, "Number of snapshots to retain before pruning")
	dbBackupCmd.Flags().StringVarP(&snapshotOutput, "output", "o", "", "Write snapshot directly to this file path")
	dbBackupCmd.Flags().BoolVarP(&backupQuiet, "quiet", "q", false, "Print only the created snapshot file path")

	dbCmd.AddCommand(dbListCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbSnapshotCmd)
	dbCmd.AddCommand(dbExportCmd)
	dbCmd.AddCommand(dbVerifyCmd)
	dbCmd.AddCommand(dbRestoreCmd)
	RootCmd.AddCommand(dbCmd, snapshotCmd, exportCmd)
}
