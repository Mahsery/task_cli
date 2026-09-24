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

		// Fetch all tasks for current scope/repo
		filter := domain.TaskFilter{
			Scope:    scope,
			RepoPath: repoRoot,
		}
		tasks, _ := taskClient.ListTasks(filter)
		var inProgTasks []*domain.Task
		var openTasks []*domain.Task
		var latestTask *domain.Task
		var latestTime time.Time

		for _, t := range tasks {
			switch t.Status {
			case domain.StatusInProgress:
				inProgTasks = append(inProgTasks, t)
			case domain.StatusOpen:
				openTasks = append(openTasks, t)
			}

			ts := t.UpdatedAt
			if ts.IsZero() {
				ts = t.CreatedAt
			}
			if ts.After(latestTime) {
				latestTime = ts
				latestTask = t
			}
		}

		if jsonFlag {
			for _, t := range append(inProgTasks, openTasks...) {
				_ = outputJSON(protocol.Event{
					Type:      protocol.EventSnapshot,
					Timestamp: time.Now(),
					Task:      t,
					Message:   fmt.Sprintf("Active task %s: %s", t.ID, t.Title),
				})
			}
		} else {
			if len(inProgTasks) == 0 && len(openTasks) == 0 {
				fmt.Fprintln(os.Stderr, "Active Tasks: 0 in progress (0 open in backlog)")
			} else {
				if len(inProgTasks) > 0 {
					fmt.Fprintf(os.Stderr, "Active Tasks (in progress: %d):\n", len(inProgTasks))
					for _, t := range inProgTasks {
						assigneeStr := ""
						if t.Assignee != "" {
							assigneeStr = fmt.Sprintf(" (claimed by %s)", t.Assignee)
						}
						tagsStr := ""
						if len(t.Tags) > 0 {
							tagsStr = fmt.Sprintf(" [%s]", strings.Join(t.Tags, ", "))
						}
						fmt.Fprintf(os.Stderr, "  %-6s [%-11s]  %-6s  %s%s%s\n",
							t.ID, t.Status, t.Priority, t.Title, tagsStr, assigneeStr)
					}
				}
				if len(openTasks) > 0 {
					fmt.Fprintf(os.Stderr, "Backlog Tasks (open: %d):\n", len(openTasks))
					for _, t := range openTasks {
						tagsStr := ""
						if len(t.Tags) > 0 {
							tagsStr = fmt.Sprintf(" [%s]", strings.Join(t.Tags, ", "))
						}
						fmt.Fprintf(os.Stderr, "  %-6s [%-11s]  %-6s  %s%s\n",
							t.ID, t.Status, t.Priority, t.Title, tagsStr)
					}
				}
			}

			if latestTask != nil {
				ago := timeAgo(latestTime)
				updateDetail := ""
				if len(latestTask.Notes) > 0 {
					lastNote := latestTask.Notes[len(latestTask.Notes)-1]
					author := lastNote.Author
					if author == "" {
						author = "actor"
					}
					updateDetail = fmt.Sprintf(" • note by %s: %q", author, lastNote.Content)
				} else if latestTask.CommitSHA != "" {
					updateDetail = fmt.Sprintf(" • resolved by commit %s: %s", latestTask.CommitSHA, latestTask.CommitMsg)
				} else if latestTask.Assignee != "" {
					updateDetail = fmt.Sprintf(" • claimed by %s", latestTask.Assignee)
				}
				fmt.Fprintf(os.Stderr, "\nLatest Update: %s [%s] %s (%s)%s\n",
					latestTask.ID, latestTask.Status, latestTask.Title, ago, updateDetail)
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

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
