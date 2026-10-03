package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"task_cli/internal/client"
	"task_cli/internal/config"
	"task_cli/internal/git"
	"task_cli/internal/tui"
	"task_cli/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	globalFlag bool
	jsonFlag   bool
	flagSocket string
	flagTCP    string
	flagActor  string
	cfg        *config.Config
	taskClient *client.Client
	scope      string
	repoRoot   string
)

var RootCmd = &cobra.Command{
	Use:   "tk",
	Short: "A fast, local task & patch management system with socket daemon and dumb-terminal TUI",
	Long: `tk is a lightweight, socket-based local task management system in Go.
It unifies task management with Linux kernel-style git diffs and commits,
operates over a background daemon socket, and supports dumb terminals and SSH.

Running 'tk' without arguments in an interactive terminal launches the TUI.`,
	Example: `  tk                     # Launch interactive TUI
  tk list                # List tasks in current git repository
  tk search bug AND odin # Search tasks using boolean query DSL
  tk -g list             # List global tasks across all projects
  tk list --json         # Output tasks in JSON format for automation`,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		cfg = config.Load()

		if flagSocket != "" {
			cfg.SocketPath = flagSocket
		}
		if flagTCP != "" {
			cfg.TCPAddr = flagTCP
		}
		if flagActor != "" {
			cfg.Actor = flagActor
		}

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
	RunE: func(cmd *cobra.Command, _ []string) error {
		// If invoked with no args and connected to an interactive terminal, launch TUI
		if !IsInteractiveTerminal() || jsonFlag {
			return cmd.Help()
		}

		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer func() { _ = taskClient.Close() }()

		model := tui.NewModel(taskClient, cfg.Editor, scope, repoRoot)
		p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
		_, err := p.Run()
		return err
	},
}

func init() {
	RootCmd.SilenceUsage = true
	RootCmd.Version = version.String()
	RootCmd.SetVersionTemplate("{{.Version}}\n")
	RootCmd.PersistentFlags().BoolVarP(&globalFlag, "global", "g", false, "Operate on global tasks instead of current git repo")
	RootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Output machine-readable JSON for LLM / script automation")
	RootCmd.PersistentFlags().StringVar(&flagSocket, "socket", "", "Custom unix socket path for daemon IPC")
	RootCmd.PersistentFlags().StringVar(&flagTCP, "tcp", "", "Optional TCP address to connect to (e.g. 127.0.0.1:4567)")
	RootCmd.PersistentFlags().StringVar(&flagActor, "actor", "", "Explicit actor identity for task attribution (overrides TK_ACTOR/TK_AUTHOR/git)")
	RootCmd.PersistentFlags().BoolVar(&noPagerFlag, "no-pager", false, "Disable interactive pager for long output")

	RootCmd.AddCommand(newCmd)
	RootCmd.AddCommand(claimCmd)
	RootCmd.AddCommand(editCmd)
	RootCmd.AddCommand(listCmd)
	RootCmd.AddCommand(searchCmd)
	RootCmd.AddCommand(showCmd)
	RootCmd.AddCommand(doneCmd)
	RootCmd.AddCommand(attachCmd)
	RootCmd.AddCommand(deleteCmd)
	RootCmd.AddCommand(watchCmd)
	RootCmd.AddCommand(daemonCmd)
	RootCmd.AddCommand(backupCmd)
	RootCmd.AddCommand(archiveCmd)
	RootCmd.AddCommand(unarchiveCmd)
	RootCmd.AddCommand(statusCmd)
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

func resolveActor(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if cfg != nil && cfg.Actor != "" {
		return cfg.Actor
	}
	return "anonymous"
}

func resolveAuthor(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if cfg != nil && cfg.Actor != "" {
		return cfg.Actor
	}
	return git.GetAuthor()
}

func splitCommaList(s string) []string {
	var result []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
