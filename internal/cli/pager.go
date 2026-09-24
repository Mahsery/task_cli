package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

var noPagerFlag bool

// IsInteractiveTerminal reports whether execution is attached to a real, interactive
// terminal (both stdout and stdin are TTYs), is not a dumb terminal, and is not running
// in an automated AI agent runner, CI workflow, or non-interactive batch environment.
func IsInteractiveTerminal() bool {
	// 1. Dumb or empty terminal capability checks
	termEnv := strings.ToLower(os.Getenv("TERM"))
	if termEnv == "dumb" || termEnv == "" {
		return false
	}

	// 2. Automated AI agent, CI pipelines, and batch flags
	if os.Getenv("CI") != "" ||
		os.Getenv("CONTINUOUS_INTEGRATION") != "" ||
		os.Getenv("DEBIAN_FRONTEND") == "noninteractive" ||
		os.Getenv("NONINTERACTIVE") != "" ||
		os.Getenv("TK_NONINTERACTIVE") != "" ||
		os.Getenv("AGENT") != "" ||
		os.Getenv("TK_AGENT") != "" {
		return false
	}

	// 3. Both stdout and stdin must be interactive character devices
	stdoutFd := os.Stdout.Fd()
	stdinFd := os.Stdin.Fd()

	if !term.IsTerminal(stdoutFd) || !term.IsTerminal(stdinFd) {
		return false
	}

	stdoutStat, err := os.Stdout.Stat()
	if err != nil || (stdoutStat.Mode()&os.ModeCharDevice) == 0 {
		return false
	}
	stdinStat, err := os.Stdin.Stat()
	if err != nil || (stdinStat.Mode()&os.ModeCharDevice) == 0 {
		return false
	}

	return true
}

// IsASCIIOnly returns true if the current locale is strictly 7-bit ASCII (e.g. C or POSIX) or TERM=dumb.
func IsASCIIOnly() bool {
	if strings.ToLower(os.Getenv("TERM")) == "dumb" {
		return true
	}
	lc := os.Getenv("LC_ALL")
	if lc == "" {
		lc = os.Getenv("LANG")
	}
	return lc == "C" || lc == "POSIX"
}

// PrintWithPager outputs text to stdout, automatically invoking a pager
// (TK_PAGER / BAT_PAGER / PAGER / less -RFX) when output exceeds terminal height.
// Bypasses the pager completely in non-interactive terminals, dumb terminals,
// automated agent harnesses, or when explicitly requested.
func PrintWithPager(content string) error {
	if !IsInteractiveTerminal() || noPagerFlag || os.Getenv("TK_NO_PAGER") != "" || os.Getenv("PAGER") == "cat" {
		_, err := fmt.Print(content)
		return err
	}

	fd := os.Stdout.Fd()
	termWidth, termHeight, err := term.GetSize(fd)
	if err != nil || termHeight <= 0 {
		_, err := fmt.Print(content)
		return err
	}

	// Calculate visual line count accounting for wrapping
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	totalVisualLines := 0
	for _, l := range lines {
		totalVisualLines++
		w := lipgloss.Width(l)
		if termWidth > 0 && w > termWidth {
			totalVisualLines += (w - 1) / termWidth
		}
	}

	// If content fits cleanly within screen, never open pager
	if totalVisualLines < termHeight-1 {
		_, err := fmt.Print(content)
		return err
	}

	// Determine pager command
	pagerCmd := os.Getenv("TK_PAGER")
	if pagerCmd == "" {
		pagerCmd = os.Getenv("BAT_PAGER")
	}
	if pagerCmd == "" {
		pagerCmd = os.Getenv("PAGER")
	}
	if pagerCmd == "" {
		pagerCmd = "less -RFX"
	}

	parts := strings.Fields(pagerCmd)
	if len(parts) == 0 {
		_, err := fmt.Print(content)
		return err
	}

	binPath, err := exec.LookPath(parts[0])
	if err != nil {
		_, err := fmt.Print(content)
		return err
	}

	cmd := exec.Command(binPath, parts[1:]...)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Ignore broken pipe or non-zero exit from user pressing 'q'
		if strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "exit status") {
			return nil
		}
		_, err = fmt.Print(content)
		return err
	}

	return nil
}
