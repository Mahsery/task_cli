package cli

import (
	"fmt"

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
		if err := taskClient.Connect(); err != nil {
			fmt.Println("Daemon is not running.")
			return nil
		}
		defer taskClient.Close()

		if err := taskClient.ShutdownDaemon(); err != nil {
			return err
		}
		fmt.Println("✓ Sent shutdown signal to daemon.")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := taskClient.Ping()
		if err != nil {
			fmt.Printf("Daemon is NOT running (socket: %s)\n", cfg.SocketPath)
			return nil
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
