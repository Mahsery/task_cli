package cli

import (
	"fmt"
	"time"

	"task_cli/internal/daemon"

	"github.com/spf13/cobra"
)

var (
	daemonSocketPath string
	daemonTCPAddr    string
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the background task daemon",
}

var daemonRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the daemon in the foreground",
	RunE: func(cmd *cobra.Command, args []string) error {
		if daemonSocketPath != "" {
			cfg.SocketPath = daemonSocketPath
		}
		if daemonTCPAddr != "" {
			cfg.TCPAddr = daemonTCPAddr
		}
		srv := daemon.NewServer(cfg)
		return srv.Start()
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := taskClient.PingPassive(); err != nil {
			if jsonFlag {
				return outputJSON(map[string]any{"running": false, "message": "Daemon is not running."})
			}
			fmt.Println("Daemon is not running.")
			return nil
		}
		defer taskClient.Close()

		if err := taskClient.ShutdownDaemon(); err != nil {
			return err
		}

		// Wait briefly for daemon to exit and release socket
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if err := taskClient.PingPassive(); err != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		if jsonFlag {
			return outputJSON(map[string]any{"stopped": true, "message": "Sent shutdown signal to daemon."})
		}
		fmt.Println("✓ Sent shutdown signal to daemon.")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := taskClient.PingPassive()
		if err != nil {
			if jsonFlag {
				return outputJSON(map[string]any{
					"running": false,
					"socket":  cfg.SocketPath,
				})
			}
			fmt.Printf("Daemon is NOT running (socket: %s)\n", cfg.SocketPath)
			return nil
		}
		if jsonFlag {
			return outputJSON(map[string]any{
				"running": true,
				"socket":  cfg.SocketPath,
			})
		}
		fmt.Printf("Daemon is running and healthy (socket: %s)\n", cfg.SocketPath)
		return nil
	},
}

func init() {
	daemonRunCmd.Flags().StringVar(&daemonSocketPath, "socket", "", "Custom unix socket path")
	daemonRunCmd.Flags().StringVar(&daemonTCPAddr, "tcp", "", "Optional TCP address to listen on (e.g. 127.0.0.1:4567)")

	daemonCmd.AddCommand(daemonRunCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
}
