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
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage task database backups and disaster recovery",
	Long: `Inspect, create, verify, and restore atomic SQLite snapshots of the tasklog.
Backups use SQLite's native VACUUM INTO for non-blocking point-in-time recovery.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackupList()
	},
}

var backupListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List available database backups",
	Aliases: []string{"ls"},
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

	fmt.Printf("%-32s %-10s %-16s %s\n", "BACKUP NAME", "SIZE", "CREATED", "PATH")
	fmt.Println(strings.Repeat("─", 88))
	for _, b := range backups {
		sizeStr := formatFileSize(b.Size)
		ageStr := formatAge(b.ModTime)
		fmt.Printf("%-32s %-10s %-16s %s\n", b.Name, sizeStr, ageStr, b.Path)
	}
	return nil
}

var backupCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create an atomic point-in-time backup snapshot",
	Args:  cobra.MaximumNArgs(1),
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
	Args:  cobra.MaximumNArgs(1),
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
	Use:   "restore <backup-file>",
	Short: "Restore task database from a backup snapshot",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backupPath := args[0]
		if !filepath.IsAbs(backupPath) {
			abs, err := filepath.Abs(backupPath)
			if err == nil {
				backupPath = abs
			}
		}

		if !backupForceRestore {
			fmt.Printf("WARNING: Restoring will overwrite the current %s database with:\n  %s\n", taskClient.Scope, backupPath)
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
	},
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
	backupCreateCmd.Flags().IntVar(&backupMaxSnapshots, "keep", 5, "Number of snapshots to retain before pruning")
	backupRestoreCmd.Flags().BoolVarP(&backupForceRestore, "force", "f", false, "Force restore without interactive confirmation")

	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupCreateCmd)
	backupCmd.AddCommand(backupVerifyCmd)
	backupCmd.AddCommand(backupRestoreCmd)
}
