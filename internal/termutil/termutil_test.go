package termutil

import (
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

func TestPadRight(t *testing.T) {
	tests := []struct {
		in     string
		width  int
		expect string
	}{
		{"abc", 5, "abc  "},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcdef"},
		{"界", 4, "界  "}, // "界" is 2 cells
	}
	for _, tt := range tests {
		got := PadRight(tt.in, tt.width)
		if got != tt.expect {
			t.Errorf("PadRight(%q, %d) = %q; want %q", tt.in, tt.width, got, tt.expect)
		}
		if runewidth.StringWidth(got) < tt.width {
			t.Errorf("PadRight(%q, %d) width = %d; want >= %d", tt.in, tt.width, runewidth.StringWidth(got), tt.width)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in     string
		maxW   int
		expect string
	}{
		{"hello", 10, "hello"},
		{"hello world", 7, "hello.."},
		{"hello\nworld", 10, "hello wo.."},
		{"hello", 0, ""},
		{"hello", 2, ".."},
		{"hello", 1, "."},
		{"界世你好", 5, "界.."}, // each CJK rune is 2 cells
	}
	for _, tt := range tests {
		got := Truncate(tt.in, tt.maxW)
		if got != tt.expect {
			t.Errorf("Truncate(%q, %d) = %q; want %q", tt.in, tt.maxW, got, tt.expect)
		}
		if runewidth.StringWidth(got) > tt.maxW {
			t.Errorf("Truncate(%q, %d) width = %d exceeds maxW %d", tt.in, tt.maxW, runewidth.StringWidth(got), tt.maxW)
		}
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Now()
	if got := FormatAge(time.Time{}); got != "-" {
		t.Errorf("FormatAge(zero) = %q; want '-'", got)
	}
	if got := FormatAge(now.Add(-10 * time.Second)); got != "just now" {
		t.Errorf("FormatAge(-10s) = %q; want 'just now'", got)
	}
	if got := FormatAge(now.Add(-5 * time.Minute)); got != "5m ago" {
		t.Errorf("FormatAge(-5m) = %q; want '5m ago'", got)
	}
	if got := FormatAge(now.Add(-3 * time.Hour)); got != "3h ago" {
		t.Errorf("FormatAge(-3h) = %q; want '3h ago'", got)
	}
	if got := FormatAge(now.Add(-48 * time.Hour)); got != "2d ago" {
		t.Errorf("FormatAge(-48h) = %q; want '2d ago'", got)
	}
	if got := FormatAge(now.Add(-65 * 24 * time.Hour)); got != "2mo ago" {
		t.Errorf("FormatAge(-65d) = %q; want '2mo ago'", got)
	}
}

func TestCompactTreeBranch(t *testing.T) {
	branch := "│   │   ├── "
	compacted := CompactTreeBranch(branch, 8)
	if runewidth.StringWidth(compacted) > 8 {
		t.Errorf("CompactTreeBranch(%q, 8) width = %d exceeds 8", branch, runewidth.StringWidth(compacted))
	}
	if !strings.HasSuffix(compacted, "├── ") {
		t.Errorf("CompactTreeBranch(%q, 8) = %q does not retain connector", branch, compacted)
	}
}

func TestWrapText(t *testing.T) {
	lines := WrapText("one two three four five", 10, "  ")
	for _, l := range lines {
		if runewidth.StringWidth(l) > 10 {
			t.Errorf("WrapText line %q exceeds width 10", l)
		}
		if !strings.HasPrefix(l, "  ") {
			t.Errorf("WrapText line %q missing indent", l)
		}
	}
}
