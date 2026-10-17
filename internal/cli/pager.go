package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"task_cli/internal/termutil"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

var noPagerFlag bool

// IsInteractiveTerminal reports whether execution is attached to a real, interactive
// terminal (both stdout and stdin are TTYs), is not a dumb terminal, and is not running
// in an automated AI agent runner, CI workflow, or non-interactive batch environment.
func IsInteractiveTerminal() bool {
	// 1. Dumb or empty terminal capability checks (on Windows, TERM is typically unset in native consoles)
	termEnv := strings.ToLower(os.Getenv("TERM"))
	if termEnv == "dumb" || (termEnv == "" && runtime.GOOS != "windows") {
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

// ReadPipedStdin reads piped or file-redirected stdin without hanging when invoked inside
// a non-interactive shell whose ambient stdin is an unclosed, idle pipe (e.g. an AI agent
// tool runner, SSH command without </dev/null, or CI subprocess).
//
// Returns (trimmedContent, true) when stdin was piped/redirected and either yielded data or
// reached immediate EOF; returns ("", false) when stdin is a character device (TTY or /dev/null)
// or an idle open pipe with no incoming data.
func ReadPipedStdin(hasExplicitFlags bool) (string, bool) {
	if term.IsTerminal(os.Stdin.Fd()) {
		return "", false
	}
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", false
	}
	// Character devices (TTYs, /dev/null, NUL) are not pipes or redirected files.
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", false
	}
	// Redirected regular files (< file.txt or <<EOF) never block on ReadAll.
	if stat.Mode().IsRegular() {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(b)), true
	}

	// For pipes/FIFOs/sockets, probe the first read with a short timeout so an open,
	// idle ambient stdin pipe from a non-interactive shell runner never blocks forever.
	timeout := 100 * time.Millisecond
	if hasExplicitFlags {
		timeout = 50 * time.Millisecond
	}

	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func(r io.Reader) {
		var first [4096]byte
		n, rErr := r.Read(first[:])
		if n == 0 && rErr != nil {
			ch <- readResult{nil, rErr}
			return
		}
		var buf bytes.Buffer
		if n > 0 {
			buf.Write(first[:n])
		}
		if rErr == nil {
			_, rErr = io.Copy(&buf, r)
		}
		ch <- readResult{buf.Bytes(), rErr}
	}(os.Stdin)

	select {
	case res := <-ch:
		if res.err != nil && !errors.Is(res.err, io.EOF) && len(res.data) == 0 {
			return "", false
		}
		return strings.TrimSpace(string(res.data)), true
	case <-time.After(timeout):
		return "", false
	}
}

// IsASCIIOnly returns true if the current locale is strictly 7-bit ASCII (e.g. C or POSIX) or TERM=dumb.
func IsASCIIOnly() bool {
	return termutil.IsASCIIOnly()
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

	cmd := exec.Command(binPath, parts[1:]...) //nolint:gosec // G702: executing the user-configured $PAGER is the intended behavior
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
