package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"task_cli/internal/git"
)

type Config struct {
	SocketPath     string
	ExplicitSocket bool
	TCPAddr        string
	GlobalDir      string
	DBPath         string
	Editor         string
	Actor          string
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
	_ = os.MkdirAll(appDir, 0o755) //nolint:gosec // G703: data dir is derived from the user's own XDG_DATA_HOME/HOME by design

	socketPath := strings.TrimSpace(os.Getenv("TK_SOCKET"))
	explicitSocket := socketPath != ""
	if socketPath == "" {
		// Use /run/user/<uid>/tk.sock if available on Unix, otherwise appDir/tk.sock
		if uid := os.Getuid(); uid >= 0 {
			runUserDir := "/run/user/" + strconv.Itoa(uid)
			if stat, err := os.Stat(runUserDir); err == nil && stat.IsDir() {
				socketPath = filepath.Join(runUserDir, "tk.sock")
			}
		}
		if socketPath == "" {
			socketPath = filepath.Join(appDir, "tk.sock")
		}
	}

	tcpAddr := strings.TrimSpace(os.Getenv("TK_ADDR")) // e.g. "127.0.0.1:4567"

	dbPath := strings.TrimSpace(os.Getenv("TK_DB"))
	if dbPath != "" && !filepath.IsAbs(dbPath) {
		if abs, err := filepath.Abs(dbPath); err == nil {
			dbPath = abs
		}
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = defaultEditor()
	}

	actor := os.Getenv("TK_ACTOR")
	if actor == "" {
		actor = os.Getenv("TK_AUTHOR")
	}
	if actor == "" {
		actor = git.GetAuthor()
	}
	if actor == "" {
		actor = os.Getenv("USER")
	}
	if actor == "" {
		actor = os.Getenv("USERNAME")
	}
	if actor == "" {
		actor = "anonymous"
	}

	return &Config{
		SocketPath:     socketPath,
		ExplicitSocket: explicitSocket,
		TCPAddr:        tcpAddr,
		GlobalDir:      appDir,
		DBPath:         dbPath,
		Editor:         editor,
		Actor:          actor,
	}
}

func defaultEditor() string {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("nvim"); err == nil {
			return "nvim"
		}
		return "notepad"
	}
	for _, ed := range []string{"nvim", "vim", "vi", "nano"} {
		if _, err := exec.LookPath(ed); err == nil {
			return ed
		}
	}
	return "nvim"
}

func (c *Config) GlobalDBPath() string {
	if c.DBPath != "" {
		return c.DBPath
	}
	return filepath.Join(c.GlobalDir, "global.db")
}

func (c *Config) LocalDBPath(repoRoot string) string {
	if c.DBPath != "" {
		return c.DBPath
	}
	return filepath.Join(repoRoot, ".tasks", "tasks.db")
}

// CentralBackupDir returns the repository-scoped or global backup directory in the central user data store.
func (c *Config) CentralBackupDir(repoPath string) string {
	if repoPath == "" {
		return filepath.Join(c.GlobalDir, "backups", "global")
	}
	clean := filepath.Clean(repoPath)
	base := filepath.Base(clean)
	if base == "" || base == "/" || base == `\` || base == "." || strings.ContainsRune(base, ':') {
		base = "root"
	}
	h := sha256.Sum256([]byte(clean))
	slug := fmt.Sprintf("%s-%x", base, h[:6])
	return filepath.Join(c.GlobalDir, "backups", "repos", slug)
}
