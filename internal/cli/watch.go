package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Stream real-time task events from daemon (great over SSH)",
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
			os.Exit(0)
		}()

		fmt.Println("Streaming live task events from daemon... (Ctrl+C to quit)")

		return taskClient.StreamEvents(func(ev protocol.Event) {
			if jsonFlag {
				_ = outputJSON(ev)
				return
			}
			ts := ev.Timestamp.Format("15:04:05")
			fmt.Printf("[%s] %s\n", ts, ev.Message)
		})
	},
}
