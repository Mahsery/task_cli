package cli

import (
	"fmt"
	"strings"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var (
	archiveMessage  string
	archiveActor    string
	archiveListFlag bool

	unarchiveMessage string
	unarchiveActor   string
)

var archiveCmd = &cobra.Command{
	Use:     "archive [flags] [id...]",
	Aliases: []string{"arc"},
	Short:   "Archive tasks or view archived tasks",
	Long: `Archive specified tasks to remove them from active views,
or list archived tasks with -l or 'tk archive list'.
Archived tasks can be restored anytime with 'tk unarchive <id...>' or 'tk archive unarchive <id...>'.`,
	Example: `  # Archive specific tasks
  tk archive T-1
  tk archive T-1 T-2 -m "Completed milestone"
  tk archive T-5 --actor "agent-007"

  # List archived tasks
  tk archive -l
  tk archive list
  tk archive list --json

  # Restore / unarchive tasks
  tk archive unarchive T-1
  tk archive restore T-1 T-2`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if archiveListFlag {
			return runArchiveList()
		}

		if len(args) == 0 {
			return cmd.Help()
		}

		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer taskClient.Close()

		actor := cfg.Actor
		if archiveActor != "" {
			actor = archiveActor
		}
		if actor == "" {
			actor = "anonymous"
		}

		var archivedTasks []*domain.Task
		for _, arg := range args {
			id := strings.TrimSpace(arg)
			if id == "" {
				continue
			}
			t, err := taskClient.ArchiveTask(id, actor, archiveMessage)
			if err != nil {
				return fmt.Errorf("failed to archive %s: %w", id, err)
			}
			archivedTasks = append(archivedTasks, t)
		}

		if jsonFlag {
			if len(archivedTasks) == 1 {
				return outputJSON(archivedTasks[0])
			}
			return outputJSON(archivedTasks)
		}

		for _, t := range archivedTasks {
			fmt.Printf("✓ Archived %s: %q\n", t.ID, t.Title)
		}
		return nil
	},
}

var archiveListCmd = &cobra.Command{
	Use:     "list [flags]",
	Aliases: []string{"ls"},
	Short:   "List archived tasks in formatted table or json",
	Long:    "Displays all archived tasks matching the current repository or global scope.",
	Example: `  tk archive list
  tk archive list --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runArchiveList()
	},
}

var archiveUnarchiveCmd = &cobra.Command{
	Use:     "unarchive <id...> [flags]",
	Aliases: []string{"restore"},
	Short:   "Restore/unarchive specified tasks",
	Long:    "Restores archived tasks back to their previous active or completed status.",
	Example: `  tk archive unarchive T-1
  tk archive restore T-1 T-2 -m "Resuming work"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUnarchive(args, unarchiveActor, unarchiveMessage)
	},
}

var unarchiveCmd = &cobra.Command{
	Use:   "unarchive <id...> [flags]",
	Short: "Restore/unarchive specified tasks",
	Long:  "Restores archived tasks back to their previous active or completed status.",
	Example: `  tk unarchive T-1
  tk unarchive T-1 T-2 -m "Resuming work"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUnarchive(args, unarchiveActor, unarchiveMessage)
	},
}

func runArchiveList() error {
	if err := taskClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer taskClient.Close()

	tasks, err := taskClient.ListTasks(domain.TaskFilter{
		Scope:           scope,
		RepoPath:        repoRoot,
		OnlyArchived:    true,
		IncludeArchived: true,
	})
	if err != nil {
		return fmt.Errorf("failed to list archived tasks: %w", err)
	}

	if jsonFlag {
		if tasks == nil {
			tasks = []*domain.Task{}
		}
		return outputJSON(tasks)
	}

	if len(tasks) == 0 {
		fmt.Printf("No archived tasks found in %s scope.\n", scope)
		return nil
	}

	renderTaskTable(tasks)
	return nil
}

func runUnarchive(args []string, explicitActor, message string) error {
	if err := taskClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer taskClient.Close()

	actor := cfg.Actor
	if explicitActor != "" {
		actor = explicitActor
	}
	if actor == "" {
		actor = "anonymous"
	}

	var unarchivedTasks []*domain.Task
	for _, arg := range args {
		id := strings.TrimSpace(arg)
		if id == "" {
			continue
		}
		t, err := taskClient.UnarchiveTask(id, actor, message)
		if err != nil {
			return fmt.Errorf("failed to unarchive %s: %w", id, err)
		}
		unarchivedTasks = append(unarchivedTasks, t)
	}

	if jsonFlag {
		if len(unarchivedTasks) == 1 {
			return outputJSON(unarchivedTasks[0])
		}
		return outputJSON(unarchivedTasks)
	}

	for _, t := range unarchivedTasks {
		fmt.Printf("✓ Unarchived %s: %q (status: %s)\n", t.ID, t.Title, t.Status)
	}
	return nil
}

func init() {
	archiveCmd.Flags().StringVarP(&archiveMessage, "message", "m", "", "Audit note or reason for archiving")
	archiveCmd.Flags().StringVar(&archiveActor, "actor", "", "Explicit actor identity")
	archiveCmd.Flags().BoolVarP(&archiveListFlag, "list", "l", false, "List archived tasks")

	archiveUnarchiveCmd.Flags().StringVarP(&unarchiveMessage, "message", "m", "", "Audit note or reason for unarchiving")
	archiveUnarchiveCmd.Flags().StringVar(&unarchiveActor, "actor", "", "Explicit actor identity")

	unarchiveCmd.Flags().StringVarP(&unarchiveMessage, "message", "m", "", "Audit note or reason for unarchiving")
	unarchiveCmd.Flags().StringVar(&unarchiveActor, "actor", "", "Explicit actor identity")

	archiveCmd.AddCommand(archiveListCmd)
	archiveCmd.AddCommand(archiveUnarchiveCmd)
}
