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

func daemonEndpoint() string {
	if cfg == nil {
		return ""
	}
	if cfg.TCPAddr != "" && !cfg.ExplicitSocket {
		return "tcp://" + cfg.TCPAddr
	}
	if cfg.TCPAddr != "" && cfg.ExplicitSocket {
		return fmt.Sprintf("%s (tcp: %s)", cfg.SocketPath, cfg.TCPAddr)
	}
	return cfg.SocketPath
}

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
  tk daemon start --socket /tmp/tk-custom.sock
  tk daemon start --tcp 127.0.0.1:4567 --db /tmp/isolated.db`,
	RunE: func(_ *cobra.Command, _ []string) error {
		ep := daemonEndpoint()
		if err := taskClient.PingPassive(); err == nil {
			if jsonFlag {
				return outputJSON(map[string]any{"running": true, "message": "Daemon is already running.", "socket": ep, "tcp": cfg.TCPAddr})
			}
			fmt.Printf("Daemon is already running (endpoint: %s)\n", ep)
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
		if cfg.ExplicitSocket && cfg.SocketPath != "" {
			spawnArgs = append(spawnArgs, "--socket", cfg.SocketPath)
		}
		if cfg.TCPAddr != "" {
			spawnArgs = append(spawnArgs, "--tcp", cfg.TCPAddr)
		}
		if cfg.DBPath != "" {
			spawnArgs = append(spawnArgs, "--db", cfg.DBPath)
		}

		subCmd := exec.Command(exe, spawnArgs...)
		client.ConfigureDetachedProcess(subCmd)
		subCmd.Stdin = nil
		subCmd.Stdout = logFile
		subCmd.Stderr = logFile

		if err := subCmd.Start(); err != nil {
			return fmt.Errorf("failed to start daemon process: %w", err)
		}

		// Wait for endpoint to become active
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
			return fmt.Errorf("daemon started (PID %d) but endpoint %s was not ready within timeout; check %s", subCmd.Process.Pid, ep, logPath)
		}

		if jsonFlag {
			return outputJSON(map[string]any{
				"started": true,
				"pid":     subCmd.Process.Pid,
				"socket":  ep,
				"tcp":     cfg.TCPAddr,
				"log":     logPath,
			})
		}

		fmt.Printf("✓ Started tk daemon in background (PID %d, endpoint: %s, log: %s)\n", subCmd.Process.Pid, ep, logPath)
		return nil
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:     "restart",
	Short:   "Restart the background daemon",
	Long:    `Stop any active daemon, wait for graceful shutdown, and launch a fresh daemon in the background.`,
	Example: `  tk daemon restart`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
  tk daemon run --socket /tmp/tk-custom.sock
  tk daemon run --tcp 127.0.0.1:4567`,
	RunE: func(_ *cobra.Command, _ []string) error {
		srv := daemon.NewServer(cfg)
		return srv.Start()
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running daemon",
	Long:  `Send a graceful shutdown signal to the active daemon, closing database connections, flushing WAL frames, and unlinking the socket.`,
	Example: `  tk daemon stop
  tk daemon stop --socket /tmp/tk-custom.sock
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
  tk daemon status --socket /tmp/tk-custom.sock
  tk daemon status --json`,
	RunE: func(_ *cobra.Command, _ []string) error {
		ep := daemonEndpoint()
		err := taskClient.PingPassive()
		if err != nil {
			if jsonFlag {
				return outputJSON(map[string]any{
					"running": false,
					"socket":  ep,
					"tcp":     cfg.TCPAddr,
				})
			}
			fmt.Printf("Daemon is NOT running (endpoint: %s)\n", ep)
			return nil
		}
		if jsonFlag {
			return outputJSON(map[string]any{
				"running": true,
				"socket":  ep,
				"tcp":     cfg.TCPAddr,
			})
		}
		fmt.Printf("Daemon is running and healthy (endpoint: %s)\n", ep)
		return nil
	},
}

func bindDaemonFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&flagSocket, "socket", "", "Custom unix socket path for daemon")
	cmd.Flags().StringVar(&flagTCP, "tcp", "", "Optional TCP address to listen on / connect to (e.g. 127.0.0.1:4567)")
}

func init() {
	bindDaemonFlags(daemonStartCmd)
	bindDaemonFlags(daemonRestartCmd)
	bindDaemonFlags(daemonRunCmd)
	bindDaemonFlags(daemonStopCmd)
	bindDaemonFlags(daemonStatusCmd)

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonRunCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
}
