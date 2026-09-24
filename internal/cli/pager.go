package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/x/term"
)

var noPagerFlag bool

// PrintWithPager outputs text to stdout, automatically invoking a pager
// (TK_PAGER / BAT_PAGER / PAGER / less -RFX) when output exceeds terminal height.
func PrintWithPager(content string) error {
	fd := os.Stdout.Fd()
	if !term.IsTerminal(fd) || noPagerFlag || os.Getenv("TK_NO_PAGER") != "" || os.Getenv("PAGER") == "cat" {
		_, err := fmt.Print(content)
		return err
	}

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
		if termWidth > 0 && len(l) > termWidth {
			totalVisualLines += (len(l) - 1) / termWidth
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
