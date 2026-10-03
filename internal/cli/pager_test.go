package cli

import (
	"strings"
	"testing"
)

func TestPrintWithPagerNonTerminal(t *testing.T) {
	// In test execution, os.Stdout is usually piped or non-terminal
	content := "Line 1\nLine 2\nLine 3\n"
	err := PrintWithPager(content)
	if err != nil {
		t.Fatalf("PrintWithPager failed on non-terminal stdout: %v", err)
	}
}

func TestPrintWithPagerNoPagerFlag(t *testing.T) {
	oldNoPager := noPagerFlag
	defer func() { noPagerFlag = oldNoPager }()

	noPagerFlag = true
	content := strings.Repeat("Long output line\n", 100)
	err := PrintWithPager(content)
	if err != nil {
		t.Fatalf("PrintWithPager failed with noPagerFlag: %v", err)
	}
}

func TestPrintWithPagerEnvOverride(t *testing.T) {
	t.Setenv("TK_NO_PAGER", "1")
	content := strings.Repeat("Long output line\n", 100)
	err := PrintWithPager(content)
	if err != nil {
		t.Fatalf("PrintWithPager failed with TK_NO_PAGER=1: %v", err)
	}
}

func TestIsInteractiveTerminal(t *testing.T) {
	// Test dumb terminal rejection
	t.Setenv("TERM", "dumb")
	if IsInteractiveTerminal() {
		t.Errorf("Expected IsInteractiveTerminal() to be false for TERM=dumb")
	}

	t.Setenv("TERM", "")
	if IsInteractiveTerminal() {
		t.Errorf("Expected IsInteractiveTerminal() to be false for empty TERM")
	}

	// Test CI environment rejection
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("CI", "true")
	if IsInteractiveTerminal() {
		t.Errorf("Expected IsInteractiveTerminal() to be false for CI=true")
	}

	// Test Agent environment rejection
	t.Setenv("CI", "")
	t.Setenv("AGENT", "1")
	if IsInteractiveTerminal() {
		t.Errorf("Expected IsInteractiveTerminal() to be false for AGENT=1")
	}
}

func TestIsASCIIOnly(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	t.Setenv("LC_ALL", "C")
	if !IsASCIIOnly() {
		t.Errorf("Expected IsASCIIOnly() to be true for LC_ALL=C")
	}

	t.Setenv("LC_ALL", "POSIX")
	if !IsASCIIOnly() {
		t.Errorf("Expected IsASCIIOnly() to be true for LC_ALL=POSIX")
	}

	t.Setenv("LC_ALL", "en_US.UTF-8")
	if IsASCIIOnly() {
		t.Errorf("Expected IsASCIIOnly() to be false for en_US.UTF-8")
	}

	// Test TERM=dumb forces ASCII fallback regardless of locale
	t.Setenv("TERM", "dumb")
	if !IsASCIIOnly() {
		t.Errorf("Expected IsASCIIOnly() to be true when TERM=dumb")
	}
}
