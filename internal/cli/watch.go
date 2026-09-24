package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Stream real-time task events from daemon (great over SSH)",
	Long: `Connect to the background daemon socket and stream live task lifecycle
events (created, updated, resolved, attached git diffs, deleted). Displays an
initial snapshot of currently active tasks on startup followed by real-time events.`,
	Example: `  tk watch
  tk watch --json | jq .`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := taskClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect to daemon: %w", err)
		}
		defer taskClient.Close()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			taskClient.Close()
		}()

		// Fetch active tasks for current scope/repo
		filter := domain.TaskFilter{
			Scope:    scope,
			RepoPath: repoRoot,
		}
		tasks, _ := taskClient.ListTasks(filter)
		var activeTasks []*domain.Task
		for _, t := range tasks {
			if t.Status == domain.StatusOpen || t.Status == domain.StatusInProgress {
				activeTasks = append(activeTasks, t)
			}
		}

		if jsonFlag {
			for _, t := range activeTasks {
				_ = outputJSON(protocol.Event{
					Type:      protocol.EventSnapshot,
					Timestamp: time.Now(),
					Task:      t,
					Message:   fmt.Sprintf("Active task %s: %s", t.ID, t.Title),
				})
			}
		} else {
			if len(activeTasks) == 0 {
				fmt.Fprintln(os.Stderr, "No active tasks currently.")
			} else {
				fmt.Fprintf(os.Stderr, "Active Tasks (%d):\n", len(activeTasks))
				for _, t := range activeTasks {
					tagsStr := ""
					if len(t.Tags) > 0 {
						tagsStr = fmt.Sprintf(" [%s]", strings.Join(t.Tags, ", "))
					}
					fmt.Fprintf(os.Stderr, "  %-6s [%-11s]  %-6s  %s%s\n",
						t.ID, t.Status, t.Priority, t.Title, tagsStr)
				}
			}
			fmt.Fprintln(os.Stderr, "\n── Streaming live task events (Ctrl+C to quit) ──")
		}

		err := taskClient.StreamEvents(func(ev protocol.Event) {
			if jsonFlag {
				_ = outputJSON(ev)
				return
			}
			ts := ev.Timestamp.Format("15:04:05")
			fmt.Printf("[%s] %s\n", ts, ev.Message)
		})

		if err != nil {
			if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		return nil
	},
}
