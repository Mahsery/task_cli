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

func TestTruncateOutput(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 75; i++ {
		b.WriteString("Line ")
		b.WriteString(strings.Repeat("x", 5))
		b.WriteString("\n")
	}
	raw := b.String()

	// 1. Truncate to 50 lines
	truncated, didTruncate := TruncateOutput(raw, 50)
	if !didTruncate {
		t.Fatalf("expected TruncateOutput to report truncation for 75 lines with maxLines=50")
	}
	if !strings.Contains(truncated, "output truncated: showing 50 of 75 lines (25 omitted)") {
		t.Fatalf("expected truncation notice in output, got:\n%s", truncated)
	}
	if !strings.Contains(truncated, "--all") || !strings.Contains(truncated, "--max-lines") {
		t.Fatalf("expected override instructions (--all / --max-lines) in truncation notice, got:\n%s", truncated)
	}

	// 2. Output under limit is untouched
	short := "Line 1\nLine 2\nLine 3\n"
	outShort, didTruncShort := TruncateOutput(short, 50)
	if didTruncShort || outShort != short {
		t.Fatalf("expected short output to be untouched, got (%q, %v)", outShort, didTruncShort)
	}

	// 3. Unlimited (maxLines <= 0) returns full content untouched
	outAll, didTruncAll := TruncateOutput(raw, 0)
	if didTruncAll || outAll != raw {
		t.Fatalf("expected maxLines=0 to return full output untouched")
	}
}

func TestResolveMaxLinesOverrides(t *testing.T) {
	origMax := maxLinesFlag
	origAll := showAllFlag
	defer func() {
		maxLinesFlag = origMax
		showAllFlag = origAll
	}()

	maxLinesFlag = DefaultMaxLines
	showAllFlag = false
	t.Setenv("TK_MAX_LINES", "")

	// Default is 50
	if got := ResolveMaxLines(); got != DefaultMaxLines {
		t.Fatalf("expected default ResolveMaxLines() = %d, got %d", DefaultMaxLines, got)
	}

	// Explicit --max-lines X
	maxLinesFlag = 20
	if got := ResolveMaxLines(); got != 20 {
		t.Fatalf("expected ResolveMaxLines() = 20 when maxLinesFlag=20, got %d", got)
	}

	// Explicit --all / --full
	showAllFlag = true
	if got := ResolveMaxLines(); got != 0 {
		t.Fatalf("expected ResolveMaxLines() = 0 when showAllFlag=true, got %d", got)
	}
	showAllFlag = false
	maxLinesFlag = DefaultMaxLines

	// Environment variable TK_MAX_LINES=15
	t.Setenv("TK_MAX_LINES", "15")
	if got := ResolveMaxLines(); got != 15 {
		t.Fatalf("expected ResolveMaxLines() = 15 from TK_MAX_LINES=15, got %d", got)
	}

	// Environment variable TK_MAX_LINES=all
	t.Setenv("TK_MAX_LINES", "all")
	if got := ResolveMaxLines(); got != 0 {
		t.Fatalf("expected ResolveMaxLines() = 0 from TK_MAX_LINES=all, got %d", got)
	}
}
