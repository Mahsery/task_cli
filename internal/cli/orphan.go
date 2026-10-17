package cli

import (
	"fmt"
	"strings"
	"time"

	"task_cli/internal/domain"

	"github.com/spf13/cobra"
)

var (
	orphanOlderThan string
	orphanMessage   string
	orphanActor     string
	orphanDryRun    bool
	orphanAll       bool
	orphanList      bool
	orphanBackport  bool
	orphanForce     bool
)

var orphanCmd = &cobra.Command{
	Use:   "orphan [task-id] [flags]",
	Short: "Detect and mark abandoned tasks as orphaned, dropping assignees",
	Long: `Manually orphan an assigned task or automatically scan and reap stale tasks
that have been abandoned for longer than a specified duration without updates,
commits, or activity on any of their dependencies.

When a task is orphaned:
- Its assignee is dropped (unassigned).
- Its status is reset to 'open' (returned to the ready backlog).
- An 'orphaned' tag is attached to the task.
- An attribution note is recorded with the duration of inactivity.`,
	Example: `  # Manually orphan a specific task
  tk orphan T-1 -m "Assignee unresponsive"

  # Preview stale tasks inactive for longer than 1 hour
  tk orphan --all --dry-run
  tk orphan reap --dry-run

  # Reap all stale tasks inactive for longer than 2 hours
  tk orphan --all --older-than 2h
  tk orphan reap --older-than 30m

  # List currently orphaned tasks and stale candidates
  tk orphan --list
  tk orphan list`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer func() { _ = taskClient.Close() }()

		actor := resolveActor(orphanActor)

		// 1. Manually orphan a specific task by ID
		if len(args) == 1 {
			taskID := normID(args[0])
			task, err := taskClient.OrphanTask(taskID, actor, orphanMessage)
			if err != nil {
				return err
			}
			if jsonFlag {
				return outputJSON(task)
			}
			fmt.Printf("✓ Orphaned %s: %q (dropped assignee, status: open)\n", task.ID, task.Title)
			return nil
		}

		threshold := 1 * time.Hour
		if orphanOlderThan != "" {
			d, err := time.ParseDuration(orphanOlderThan)
			if err != nil {
				return fmt.Errorf("invalid --older-than duration %q: %w (e.g. 30m, 1h, 4h, 24h)", orphanOlderThan, err)
			}
			threshold = d
		}

		// 2. List currently orphaned tasks and stale candidates
		if orphanList {
			return runOrphanList(threshold, actor)
		}

		// 3. Backport historical stale tasks
		if orphanBackport {
			return runOrphanBackport(threshold, orphanDryRun, actor, orphanMessage)
		}

		// 4. Reap stale tasks
		if orphanAll {
			return runOrphanReap(threshold, orphanDryRun, actor, orphanMessage)
		}

		// If no argument and neither flag was passed, display help
		return cmd.Help()
	},
}

func runOrphanBackport(threshold time.Duration, dryRun bool, actor, reason string) error {
	res, err := taskClient.ReapStaleTasks(scope, repoRoot, threshold, dryRun, actor, reason)
	if err != nil {
		return err
	}

	if jsonFlag {
		return outputJSON(res)
	}

	if dryRun {
		if len(res.Candidates) == 0 {
			fmt.Printf("✓ No historical stale tasks found older than %s.\n", threshold)
			return nil
		}
		fmt.Printf("[DRY RUN] Found %d historical stale task(s) older than %s:\n", len(res.Candidates), threshold)
		for _, c := range res.Candidates {
			if c.Assignee != "" {
				fmt.Printf("  • %-6s [%-11s] (assigned to %q, inactive for %s) %s\n",
					c.TaskID, c.Status, c.Assignee, formatAge(c.LastActivity), c.Title)
			} else {
				fmt.Printf("  • %-6s [%-11s] (unassigned, inactive for %s) %s\n",
					c.TaskID, c.Status, formatAge(c.LastActivity), c.Title)
			}
		}
		fmt.Println("\nRun 'tk orphan backport' (or 'tk orphan --backport') to tag matching open tasks as orphaned.")
		return nil
	}

	if len(res.Orphaned) == 0 {
		fmt.Printf("✓ No historical stale tasks found older than %s.\n", threshold)
		return nil
	}

	fmt.Printf("✓ Backported and marked %d stale task(s) as orphaned (threshold: %s):\n", len(res.Orphaned), threshold)
	orphanedSet := make(map[string]bool)
	for _, id := range res.Orphaned {
		orphanedSet[id] = true
	}
	for _, c := range res.Candidates {
		if !orphanedSet[c.TaskID] {
			continue
		}
		if c.Assignee != "" {
			fmt.Printf("  • %-6s (dropped assignee %q, inactive for %s): %s\n",
				c.TaskID, c.Assignee, formatAge(c.LastActivity), c.Title)
		} else {
			fmt.Printf("  • %-6s (unassigned, inactive for %s): %s\n",
				c.TaskID, formatAge(c.LastActivity), c.Title)
		}
	}
	return nil
}

func runOrphanReap(threshold time.Duration, dryRun bool, actor, reason string) error {
	if !dryRun && !orphanForce && !jsonFlag {
		preview, err := taskClient.ReapStaleTasks(scope, repoRoot, threshold, true, actor, reason)
		if err != nil {
			return err
		}
		if len(preview.Candidates) == 0 {
			fmt.Printf("✓ No stale tasks found exceeding %s of inactivity.\n", threshold)
			return nil
		}
		if !IsInteractiveTerminal() {
			return fmt.Errorf("refusing to reap %d stale task(s) without confirmation in non-interactive terminal: pass -y or -f (or --dry-run to preview)", len(preview.Candidates))
		}
		fmt.Printf("Reap %d stale tasks? [y/N]: ", len(preview.Candidates))
		var resp string
		_, _ = fmt.Scanln(&resp)
		resp = strings.ToLower(strings.TrimSpace(resp))
		if resp != "y" && resp != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	res, err := taskClient.ReapStaleTasks(scope, repoRoot, threshold, dryRun, actor, reason)
	if err != nil {
		return err
	}

	if jsonFlag {
		return outputJSON(res)
	}

	if dryRun {
		if len(res.Candidates) == 0 {
			fmt.Printf("✓ No stale tasks found exceeding %s of inactivity.\n", threshold)
			return nil
		}
		fmt.Printf("[DRY RUN] Found %d stale task(s) inactive for > %s:\n", len(res.Candidates), threshold)
		for _, c := range res.Candidates {
			if c.Assignee != "" {
				fmt.Printf("  • %-6s [%-11s] (assigned to %q, inactive for %s) %s\n",
					c.TaskID, c.Status, c.Assignee, formatAge(c.LastActivity), c.Title)
			} else {
				fmt.Printf("  • %-6s [%-11s] (unassigned, inactive for %s) %s\n",
					c.TaskID, c.Status, formatAge(c.LastActivity), c.Title)
			}
		}
		fmt.Println("\nRun 'tk orphan --all' (or 'tk orphan reap') to drop assignees and return to open backlog.")
		return nil
	}

	if len(res.Orphaned) == 0 {
		fmt.Printf("✓ No stale tasks found exceeding %s of inactivity.\n", threshold)
		return nil
	}

	fmt.Printf("✓ Orphaned %d stale task(s) (threshold: %s):\n", len(res.Orphaned), threshold)
	orphanedSet := make(map[string]bool)
	for _, id := range res.Orphaned {
		orphanedSet[id] = true
	}
	for _, c := range res.Candidates {
		if !orphanedSet[c.TaskID] {
			continue
		}
		if c.Assignee != "" {
			fmt.Printf("  • %-6s (dropped assignee %q, inactive for %s): %s\n",
				c.TaskID, c.Assignee, formatAge(c.LastActivity), c.Title)
		} else {
			fmt.Printf("  • %-6s (unassigned, inactive for %s): %s\n",
				c.TaskID, formatAge(c.LastActivity), c.Title)
		}
	}
	return nil
}

func runOrphanList(threshold time.Duration, actor string) error {
	orphanedTasks, err := taskClient.ListTasks(domain.TaskFilter{
		Scope:        scope,
		RepoPath:     repoRoot,
		OnlyOrphaned: true,
	})
	if err != nil {
		return err
	}

	staleRes, err := taskClient.ReapStaleTasks(scope, repoRoot, threshold, true, actor, "")
	if err != nil {
		return err
	}

	if jsonFlag {
		type listData struct {
			Orphaned   []*domain.Task              `json:"orphaned"`
			Candidates []domain.StaleTaskCandidate `json:"candidates"`
		}
		return outputJSON(listData{
			Orphaned:   orphanedTasks,
			Candidates: staleRes.Candidates,
		})
	}

	if len(orphanedTasks) == 0 && len(staleRes.Candidates) == 0 {
		fmt.Printf("No orphaned or stale tasks found (threshold: %s).\n", threshold)
		return nil
	}

	if len(orphanedTasks) > 0 {
		fmt.Printf("Currently Orphaned Tasks (%d):\n", len(orphanedTasks))
		for _, t := range orphanedTasks {
			fmt.Printf("  %-6s [%-11s] %-6s  %s (age: %s)\n",
				t.ID, t.Status, t.Priority, t.Title, formatAge(t.CreatedAt))
		}
		fmt.Println()
	}

	if len(staleRes.Candidates) > 0 {
		fmt.Printf("Stale Candidate Tasks (inactive > %s, unworked: %d):\n", threshold, len(staleRes.Candidates))
		for _, c := range staleRes.Candidates {
			if c.Assignee != "" {
				fmt.Printf("  %-6s [%-11s] (assigned: %q, inactive: %s)  %s\n",
					c.TaskID, c.Status, c.Assignee, formatAge(c.LastActivity), c.Title)
			} else {
				fmt.Printf("  %-6s [%-11s] (unassigned, inactive: %s)  %s\n",
					c.TaskID, c.Status, formatAge(c.LastActivity), c.Title)
			}
		}
		fmt.Println("\nRun 'tk orphan --all' to reap stale candidates.")
	}

	return nil
}

var orphanBackportCmd = &cobra.Command{
	Use:   "backport [flags]",
	Short: "Scan historical tasks and tag abandoned/stale open tasks as orphaned",
	Long: `Scans all historical tasks in the repository created before --older-than (default: 1h),
and tags abandoned or stale open tasks (including unassigned tasks with zero activity) as 'orphaned'.`,
	Example: `  # Preview historical stale open tasks older than 1 hour
  tk orphan backport --dry-run

  # Tag historical stale open tasks older than 1 hour as orphaned
  tk orphan backport

  # Custom inactivity threshold with custom reason
  tk orphan backport --older-than 24h -m "Backporting legacy tasks"

  # Or using flag on tk orphan:
  tk orphan --backport --dry-run
  tk orphan --backport --older-than 24h`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer func() { _ = taskClient.Close() }()

		actor := resolveActor(orphanActor)

		threshold := 1 * time.Hour
		if orphanOlderThan != "" {
			d, err := time.ParseDuration(orphanOlderThan)
			if err != nil {
				return fmt.Errorf("invalid --older-than duration %q: %w (e.g. 30m, 1h, 4h, 24h)", orphanOlderThan, err)
			}
			threshold = d
		}

		return runOrphanBackport(threshold, orphanDryRun, actor, orphanMessage)
	},
}

var orphanReapCmd = &cobra.Command{
	Use:   "reap [flags]",
	Short: "Reap all stale tasks inactive past threshold duration (default: 1h)",
	Long: `Scans and orphans ongoing assigned tasks that have not had updates,
commits, or dependency activity within the threshold window.`,
	Example: `  # Preview tasks inactive for > 1 hour
  tk orphan reap --dry-run

  # Automatically orphan tasks inactive for > 1 hour
  tk orphan reap

  # Custom inactivity threshold with reason
  tk orphan reap --older-than 2h -m "Reaping inactive tasks"`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer func() { _ = taskClient.Close() }()

		actor := resolveActor(orphanActor)

		threshold := 1 * time.Hour
		if orphanOlderThan != "" {
			d, err := time.ParseDuration(orphanOlderThan)
			if err != nil {
				return fmt.Errorf("invalid --older-than duration %q: %w (e.g. 30m, 1h, 4h, 24h)", orphanOlderThan, err)
			}
			threshold = d
		}

		return runOrphanReap(threshold, orphanDryRun, actor, orphanMessage)
	},
}

var orphanListCmd = &cobra.Command{
	Use:   "list [flags]",
	Short: "List currently orphaned tasks and stale candidates",
	Long:  "Displays tasks that are currently tagged 'orphaned' along with stale candidate tasks.",
	Example: `  tk orphan list
  tk orphan list --older-than 30m`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer func() { _ = taskClient.Close() }()

		actor := cfg.Actor
		if orphanActor != "" {
			actor = orphanActor
		}

		threshold := 1 * time.Hour
		if orphanOlderThan != "" {
			d, err := time.ParseDuration(orphanOlderThan)
			if err != nil {
				return fmt.Errorf("invalid --older-than duration %q: %w (e.g. 30m, 1h, 4h, 24h)", orphanOlderThan, err)
			}
			threshold = d
		}

		return runOrphanList(threshold, actor)
	},
}

func init() {
	orphanCmd.Flags().StringVar(&orphanOlderThan, "older-than", "1h", "Inactivity duration threshold (e.g. 30m, 1h, 4h, 24h)")
	orphanCmd.Flags().StringVarP(&orphanMessage, "message", "m", "", "Audit note or reason for orphaning")
	orphanCmd.Flags().StringVar(&orphanActor, "actor", "", "Explicit actor identity")
	orphanCmd.Flags().BoolVar(&orphanDryRun, "dry-run", false, "Preview matching tasks without modifying them")
	orphanCmd.Flags().BoolVar(&orphanAll, "all", false, "Reap all stale tasks matching inactivity threshold")
	orphanCmd.Flags().BoolVarP(&orphanList, "list", "l", false, "List currently orphaned tasks and stale candidates")
	orphanCmd.Flags().BoolVar(&orphanBackport, "backport", false, "Scan historical tasks and tag stale/abandoned open tasks as orphaned")
	orphanCmd.Flags().BoolVarP(&orphanForce, "yes", "y", false, "Confirm reaping without interactive prompt")
	orphanCmd.Flags().BoolVarP(&orphanForce, "force", "f", false, "Confirm reaping without interactive prompt")

	orphanReapCmd.Flags().StringVar(&orphanOlderThan, "older-than", "1h", "Inactivity duration threshold (e.g. 30m, 1h, 4h, 24h)")
	orphanReapCmd.Flags().StringVarP(&orphanMessage, "message", "m", "", "Audit note or reason for orphaning")
	orphanReapCmd.Flags().StringVar(&orphanActor, "actor", "", "Explicit actor identity")
	orphanReapCmd.Flags().BoolVar(&orphanDryRun, "dry-run", false, "Preview matching tasks without modifying them")
	orphanReapCmd.Flags().BoolVarP(&orphanForce, "yes", "y", false, "Confirm reaping without interactive prompt")
	orphanReapCmd.Flags().BoolVarP(&orphanForce, "force", "f", false, "Confirm reaping without interactive prompt")

	orphanBackportCmd.Flags().StringVar(&orphanOlderThan, "older-than", "1h", "Inactivity duration threshold (e.g. 30m, 1h, 4h, 24h)")
	orphanBackportCmd.Flags().StringVarP(&orphanMessage, "message", "m", "", "Audit note or reason for orphaning")
	orphanBackportCmd.Flags().StringVar(&orphanActor, "actor", "", "Explicit actor identity")
	orphanBackportCmd.Flags().BoolVar(&orphanDryRun, "dry-run", false, "Preview matching tasks without modifying them")

	orphanListCmd.Flags().StringVar(&orphanOlderThan, "older-than", "1h", "Inactivity duration threshold (e.g. 30m, 1h, 4h, 24h)")
	orphanListCmd.Flags().StringVar(&orphanActor, "actor", "", "Explicit actor identity")

	orphanCmd.AddCommand(orphanReapCmd)
	orphanCmd.AddCommand(orphanBackportCmd)
	orphanCmd.AddCommand(orphanListCmd)

	RootCmd.AddCommand(orphanCmd)
}
