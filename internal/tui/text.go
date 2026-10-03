package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// wrapText word-wraps text to the given visual width (terminal cells), prefixing each
// line with indent. Words wider than the available space are hard-broken so no line overflows.
func wrapText(text string, width int, indent string) []string {
	var result []string
	indentW := runewidth.StringWidth(indent)
	avail := width - indentW
	if avail < 1 {
		avail = 1
	}
	paragraphs := strings.Split(text, "\n")
	for _, p := range paragraphs {
		p = strings.TrimRight(p, " \t\r")
		if p == "" {
			result = append(result, "")
			continue
		}
		words := strings.Fields(p)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}
		var curLine strings.Builder
		curLine.WriteString(indent)
		curLen := 0 // visual width excluding indent

		flush := func() {
			result = append(result, curLine.String())
			curLine.Reset()
			curLine.WriteString(indent)
			curLen = 0
		}

		for _, w := range words {
			ww := runewidth.StringWidth(w)
			if curLen > 0 && curLen+1+ww > avail {
				flush()
			}
			if curLen > 0 {
				curLine.WriteString(" ")
				curLen++
			}
			if ww <= avail-curLen {
				curLine.WriteString(w)
				curLen += ww
				continue
			}
			// Hard-break a word that cannot fit on a line by itself.
			for _, r := range w {
				rw := runewidth.RuneWidth(r)
				if curLen > 0 && curLen+rw > avail {
					flush()
				}
				curLine.WriteRune(r)
				curLen += rw
			}
		}
		if curLen > 0 {
			result = append(result, curLine.String())
		}
	}
	return result
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func isASCIIOnly() bool {
	if strings.ToLower(os.Getenv("TERM")) == "dumb" {
		return true
	}
	lc := os.Getenv("LC_ALL")
	if lc == "" {
		lc = os.Getenv("LANG")
	}
	return lc == "C" || lc == "POSIX"
}

// truncateToWidth truncates s so its terminal visual display width does not exceed maxWidth.
// If truncated, it appends ".." within the maxWidth budget. It never slices multi-byte UTF-8 runes.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	w := runewidth.StringWidth(s)
	if w <= maxWidth {
		return s
	}
	if maxWidth <= 2 {
		return strings.Repeat(".", maxWidth)
	}

	target := maxWidth - 2
	var sb strings.Builder
	curW := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if curW+rw > target {
			break
		}
		sb.WriteRune(r)
		curW += rw
	}
	sb.WriteString("..")
	return sb.String()
}

// padRight pads s with spaces so its visual terminal display width equals targetWidth.
func padRight(s string, targetWidth int) string {
	w := runewidth.StringWidth(s)
	if w >= targetWidth {
		return s
	}
	return s + strings.Repeat(" ", targetWidth-w)
}

// compactTreeBranch compacts deep tree branch indentation so it does not exceed maxW visual cells,
// ensuring the connector and task title remain clearly visible without getting squeezed out.
func compactTreeBranch(branch string, maxW int) string {
	w := runewidth.StringWidth(branch)
	if w <= maxW || maxW <= 0 {
		return branch
	}

	ascii := isASCIIOnly()
	connectorW := 4
	if maxW < connectorW+2 {
		return truncateToWidth(branch, maxW)
	}

	var connector string
	switch {
	case strings.HasSuffix(branch, "├── "):
		connector = "├── "
	case strings.HasSuffix(branch, "└── "):
		connector = "└── "
	case strings.HasSuffix(branch, "|-- "):
		connector = "|-- "
	case strings.HasSuffix(branch, "\\-- "):
		connector = "\\-- "
	default:
		runes := []rune(branch)
		if len(runes) < 4 {
			return truncateToWidth(branch, maxW)
		}
		connector = string(runes[len(runes)-4:])
	}

	ellipsis := "..│ "
	if ascii {
		ellipsis = "..| "
	}
	if maxW < 8 {
		ellipsis = ".."
	}
	elW := runewidth.StringWidth(ellipsis)
	avail := maxW - connectorW - elW
	if avail <= 0 {
		return ellipsis + connector
	}

	prefixWithoutConnector := branch[:len(branch)-len(connector)]
	runes := []rune(prefixWithoutConnector)
	var middle string
	for len(runes) >= 4 {
		lastChunk := string(runes[len(runes)-4:])
		cw := runewidth.StringWidth(lastChunk)
		if avail-cw < 0 {
			break
		}
		middle = lastChunk + middle
		avail -= cw
		runes = runes[:len(runes)-4]
	}

	return ellipsis + middle + connector
}
