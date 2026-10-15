package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"task_cli/internal/client"
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
	Long: `Control and inspect the background tk daemon process (tkd).
The daemon manages concurrency-safe SQLite database access, in-memory event publishing,
and WAL checkpoints over Unix domain sockets or optional TCP listeners.`,
	Example: `  tk daemon status    # Check if daemon is active
  tk daemon start     # Start daemon in background
  tk daemon restart   # Restart background daemon
  tk daemon stop      # Gracefully stop background daemon
  tk daemon run       # Run daemon in foreground for debugging`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon in the background",
	Long:  `Launch the tk daemon in a detached background session and verify it is accepting connections.`,
	Example: `  tk daemon start
  tk daemon start --socket /tmp/tk-custom.sock`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if daemonSocketPath != "" {
			cfg.SocketPath = daemonSocketPath
		}
		if daemonTCPAddr != "" {
			cfg.TCPAddr = daemonTCPAddr
		}

		if err := taskClient.PingPassive(); err == nil {
			if jsonFlag {
				return outputJSON(map[string]any{"running": true, "message": "Daemon is already running.", "socket": cfg.SocketPath})
			}
			fmt.Printf("Daemon is already running (socket: %s)\n", cfg.SocketPath)
			return nil
		}

		exe, err := os.Executable()
		if err != nil {
			exe = "tk"
		}

		logPath := filepath.Join(cfg.GlobalDir, "daemon.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("failed to open daemon log file %s: %w", logPath, err)
		}
		defer func() { _ = logFile.Close() }()

		spawnArgs := []string{"daemon", "run"}
		if daemonSocketPath != "" {
			spawnArgs = append(spawnArgs, "--socket", daemonSocketPath)
		}
		if daemonTCPAddr != "" {
			spawnArgs = append(spawnArgs, "--tcp", daemonTCPAddr)
		}

		subCmd := exec.Command(exe, spawnArgs...)
		client.ConfigureDetachedProcess(subCmd)
		subCmd.Stdin = nil
		subCmd.Stdout = logFile
		subCmd.Stderr = logFile

		if err := subCmd.Start(); err != nil {
			return fmt.Errorf("failed to start daemon process: %w", err)
		}

		// Wait for socket to become active
		deadline := time.Now().Add(3 * time.Second)
		started := false
		for time.Now().Before(deadline) {
			if err := taskClient.PingPassive(); err == nil {
				started = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		if !started {
			return fmt.Errorf("daemon started (PID %d) but socket %s was not ready within timeout; check %s", subCmd.Process.Pid, cfg.SocketPath, logPath)
		}

		if jsonFlag {
			return outputJSON(map[string]any{
				"started": true,
				"pid":     subCmd.Process.Pid,
				"socket":  cfg.SocketPath,
				"log":     logPath,
			})
		}

		fmt.Printf("✓ Started tk daemon in background (PID %d, socket: %s, log: %s)\n", subCmd.Process.Pid, cfg.SocketPath, logPath)
		return nil
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:     "restart",
	Short:   "Restart the background daemon",
	Long:    `Stop any active daemon, wait for graceful shutdown, and launch a fresh daemon in the background.`,
	Example: `  tk daemon restart`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if daemonSocketPath != "" {
			cfg.SocketPath = daemonSocketPath
		}
		if daemonTCPAddr != "" {
			cfg.TCPAddr = daemonTCPAddr
		}

		if err := taskClient.PingPassive(); err == nil {
			_ = taskClient.ShutdownDaemon()
			_ = taskClient.Close()
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if err := taskClient.PingPassive(); err != nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}

		return daemonStartCmd.RunE(cmd, args)
	},
}

var daemonRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the daemon in the foreground",
	Long:  `Run the task daemon process in the foreground, logging requests and events to stdout. Useful for debugging or containerized services.`,
	Example: `  tk daemon run
  tk daemon run --socket /tmp/tk-custom.sock`,
	RunE: func(_ *cobra.Command, _ []string) error {
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
	Long:  `Send a graceful shutdown signal to the active daemon, closing database connections, flushing WAL frames, and unlinking the socket.`,
	Example: `  tk daemon stop
  tk daemon stop --json`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := taskClient.PingPassive(); err != nil {
			if jsonFlag {
				return outputJSON(map[string]any{"running": false, "message": "Daemon is not running."})
			}
			fmt.Println("Daemon is not running.")
			return nil
		}
		defer func() { _ = taskClient.Close() }()

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
	Long:  `Ping the daemon socket passively without triggering auto-spawn to verify whether the background process is running and healthy.`,
	Example: `  tk daemon status
  tk daemon status --json`,
	RunE: func(_ *cobra.Command, _ []string) error {
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
	daemonStartCmd.Flags().StringVar(&daemonSocketPath, "socket", "", "Custom unix socket path")
	daemonStartCmd.Flags().StringVar(&daemonTCPAddr, "tcp", "", "Optional TCP address to listen on (e.g. 127.0.0.1:4567)")
	daemonRestartCmd.Flags().StringVar(&daemonSocketPath, "socket", "", "Custom unix socket path")
	daemonRestartCmd.Flags().StringVar(&daemonTCPAddr, "tcp", "", "Optional TCP address to listen on (e.g. 127.0.0.1:4567)")
	daemonRunCmd.Flags().StringVar(&daemonSocketPath, "socket", "", "Custom unix socket path")
	daemonRunCmd.Flags().StringVar(&daemonTCPAddr, "tcp", "", "Optional TCP address to listen on (e.g. 127.0.0.1:4567)")

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonRunCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
}
