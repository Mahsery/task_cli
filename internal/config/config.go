package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	SocketPath string
	TCPAddr    string
	GlobalDir  string
	Editor     string
}

func Load() *Config {
	home, err := os.UserHomeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		if err == nil && home != "" {
			dataHome = filepath.Join(home, ".local", "share")
		} else {
			dataHome = filepath.Join(os.TempDir(), "task_cli_data")
		}
	}

	appDir := filepath.Join(dataHome, "task_cli")
	_ = os.MkdirAll(appDir, 0755)

	socketPath := os.Getenv("TK_SOCKET")
	if socketPath == "" {
		// Use /run/user/<uid>/tk.sock if available, otherwise appDir/tk.sock
		uid := os.Getuid()
		runUserDir := filepath.Join("/run", "user", strconv.Itoa(uid))
		if stat, err := os.Stat(runUserDir); err == nil && stat.IsDir() {
			socketPath = filepath.Join(runUserDir, "tk.sock")
		} else {
			socketPath = filepath.Join(appDir, "tk.sock")
		}
	}

	tcpAddr := os.Getenv("TK_ADDR") // e.g. "127.0.0.1:4567"

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "nvim"
	}

	return &Config{
		SocketPath: socketPath,
		TCPAddr:    tcpAddr,
		GlobalDir:  appDir,
		Editor:     editor,
	}
}

func (c *Config) GlobalDBPath() string {
	return filepath.Join(c.GlobalDir, "global.db")
}

func (c *Config) LocalDBPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".tasks", "tasks.db")
}
