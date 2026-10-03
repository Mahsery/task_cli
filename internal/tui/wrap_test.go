package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestWrapTextNeverExceedsWidth(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		width  int
		indent string
	}{
		{"long unbroken word", strings.Repeat("A", 130), 30, "  "},
		{"mixed words", "short " + strings.Repeat("B", 50) + " tail words here", 20, "    "},
		{"wide runes", strings.Repeat("界", 40), 21, ""},
		{"emoji title", "Deploy 🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀 now", 12, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := wrapText(c.text, c.width, c.indent)
			if len(lines) == 0 {
				t.Fatal("expected output lines")
			}
			var rebuilt strings.Builder
			for _, l := range lines {
				if w := runewidth.StringWidth(l); w > c.width {
					t.Errorf("line %q has width %d > %d", l, w, c.width)
				}
				if !strings.HasPrefix(l, c.indent) {
					t.Errorf("line %q missing indent %q", l, c.indent)
				}
				rebuilt.WriteString(strings.ReplaceAll(strings.TrimPrefix(l, c.indent), " ", ""))
			}
			// No content may be lost by wrapping.
			if want := strings.ReplaceAll(c.text, " ", ""); rebuilt.String() != want {
				t.Errorf("content changed:\n got %q\nwant %q", rebuilt.String(), want)
			}
		})
	}
}

func TestConfirmModalCapsLongDetailLists(t *testing.T) {
	m := Model{width: 100, height: 30, theme: NewKanagawaTheme()}
	var details []string
	for i := 0; i < 40; i++ {
		details = append(details, fmt.Sprintf("T-%d: %s", i, strings.Repeat("long title words ", 7)))
	}
	out := m.renderConfirmationModal(&ConfirmDialog{Title: "CONFIRM", Prompt: "Do it?", Details: details}, 100, 30)
	lines := strings.Split(out, "\n")
	if len(lines) > 30 {
		t.Fatalf("modal height %d exceeds terminal height 30", len(lines))
	}
	if !strings.Contains(out, "more") {
		t.Errorf("expected overflow summary line in modal")
	}
}
