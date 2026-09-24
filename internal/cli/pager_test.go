package cli

import (
	"os"
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
	oldEnv := os.Getenv("TK_NO_PAGER")
	defer os.Setenv("TK_NO_PAGER", oldEnv)

	os.Setenv("TK_NO_PAGER", "1")
	content := strings.Repeat("Long output line\n", 100)
	err := PrintWithPager(content)
	if err != nil {
		t.Fatalf("PrintWithPager failed with TK_NO_PAGER=1: %v", err)
	}
}
