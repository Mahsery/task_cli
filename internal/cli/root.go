package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"task_cli/internal/client"
	"task_cli/internal/config"
	"task_cli/internal/git"
	"task_cli/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	globalFlag  bool
	jsonFlag    bool
	cfg         *config.Config
	taskClient  *client.Client
	scope       string
	repoRoot    string
)

var RootCmd = &cobra.Command{
	Use:   "tk",
	Short: "A fast, local task & patch management system with socket daemon and dumb-terminal TUI",
	Long: `tk is a lightweight, socket-based local task management system in Go.
It unifies task management with Linux kernel-style git diffs and commits,
operates over a background daemon socket, and supports dumb terminals and SSH.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg = config.Load()

		scope = "local"
		if globalFlag {
			scope = "global"
		} else {
			root, err := git.FindRepoRoot()
			if err != nil {
				// Outside git repo, automatically fall back to global
				scope = "global"
			} else {
				repoRoot = root
			}
		}

		taskClient = client.New(cfg, scope, repoRoot)
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// If invoked with no args and connected to a terminal, launch TUI
		if !isTerminal() || jsonFlag {
			return cmd.Help()
		}

		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer taskClient.Close()

		model := tui.NewModel(taskClient, cfg.Editor, scope, repoRoot)
		p := tea.NewProgram(model, tea.WithAltScreen())
		_, err := p.Run()
		return err
	},
}

func init() {
	RootCmd.PersistentFlags().BoolVarP(&globalFlag, "global", "g", false, "Operate on global tasks instead of current git repo")
	RootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Output machine-readable JSON for LLM / script automation")

	RootCmd.AddCommand(newCmd)
	RootCmd.AddCommand(editCmd)
	RootCmd.AddCommand(listCmd)
	RootCmd.AddCommand(showCmd)
	RootCmd.AddCommand(doneCmd)
	RootCmd.AddCommand(attachCmd)
	RootCmd.AddCommand(deleteCmd)
	RootCmd.AddCommand(watchCmd)
	RootCmd.AddCommand(daemonCmd)
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func outputJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func isTerminal() bool {
	stat, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}
