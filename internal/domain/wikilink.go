package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// WikiLink is an Obsidian-style [[...]] reference found in free text.
//
// Supported forms:
//
//	[[T-15]]            item (forgiving: [[t15]], [[T15]])
//	[[T-15#L12]]        line 12 of the item's description (also [[T-15:12]])
//	[[T-15#L12-L20]]    line range (also [[T-15#L12-20]], [[T-15:12-20]])
//	[[T-15#n3]]         note number 3 (1-based)
//	[[T-15|alias]]      custom display text
//	[[@game/hero]]      topic
type WikiLink struct {
	Raw       string `json:"raw"`
	Start     int    `json:"start"` // byte offset of the opening "[[" in the source text
	End       int    `json:"end"`   // byte offset just past the closing "]]"
	Target    string `json:"target"`
	IsTopic   bool   `json:"is_topic,omitempty"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	Note      int    `json:"note,omitempty"`
	Alias     string `json:"alias,omitempty"`
}

// Anchor renders the canonical anchor suffix ("", "#L12", "#L12-L20", "#n3").
func (l WikiLink) Anchor() string {
	switch {
	case l.Note > 0:
		return fmt.Sprintf("#n%d", l.Note)
	case l.LineStart > 0 && l.LineEnd > l.LineStart:
		return fmt.Sprintf("#L%d-L%d", l.LineStart, l.LineEnd)
	case l.LineStart > 0:
		return fmt.Sprintf("#L%d", l.LineStart)
	}
	return ""
}

// Canonical renders the link back in canonical [[...]] syntax.
func (l WikiLink) Canonical() string {
	t := l.Target
	if l.IsTopic {
		t = "@" + t
	}
	s := t + l.Anchor()
	if l.Alias != "" {
		s += "|" + l.Alias
	}
	return "[[" + s + "]]"
}

// Display returns the human label for the link: alias if set, otherwise target+anchor.
func (l WikiLink) Display() string {
	if l.Alias != "" {
		return l.Alias
	}
	if l.IsTopic {
		return "@" + l.Target
	}
	return l.Target + l.Anchor()
}

// ParseWikiLink parses the inner text of a [[...]] link (without brackets).
func ParseWikiLink(inner, scope string) (WikiLink, error) {
	var l WikiLink
	body := inner
	if i := strings.Index(body, "|"); i >= 0 {
		l.Alias = strings.TrimSpace(body[i+1:])
		body = body[:i]
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return l, fmt.Errorf("empty link")
	}

	if strings.HasPrefix(body, "@") {
		ref := NormalizeTopicRef(body)
		if ref == "" {
			return l, fmt.Errorf("empty topic link")
		}
		for _, seg := range strings.Split(ref, "/") {
			if seg == "" || NormalizeSlug(seg) != seg {
				return l, fmt.Errorf("invalid topic link %q", body)
			}
		}
		l.Target = ref
		l.IsTopic = true
		return l, nil
	}

	target, anchor := body, ""
	if i := strings.IndexAny(body, "#:"); i >= 0 {
		target, anchor = strings.TrimSpace(body[:i]), strings.TrimSpace(body[i+1:])
		if body[i] == ':' {
			anchor = "L" + anchor
		}
	}
	// Bare numbers are deliberately not accepted inside links to avoid accidental matches like [[1]].
	if _, _, ok := ParseItemID(target); !ok {
		return l, fmt.Errorf("invalid link target %q", target)
	}
	id, err := NormalizeID(target, scope)
	if err != nil {
		return l, err
	}
	l.Target = id

	if anchor == "" {
		return l, nil
	}
	switch anchor[0] {
	case 'L', 'l':
		parts := strings.SplitN(anchor[1:], "-", 2)
		start, err := strconv.Atoi(parts[0])
		if err != nil || start <= 0 {
			return l, fmt.Errorf("invalid line anchor %q", anchor)
		}
		l.LineStart = start
		if len(parts) == 2 {
			endStr := strings.TrimPrefix(strings.TrimPrefix(parts[1], "L"), "l")
			end, err := strconv.Atoi(endStr)
			if err != nil || end < start {
				return l, fmt.Errorf("invalid line range %q", anchor)
			}
			if end > start {
				l.LineEnd = end
			}
		}
	case 'n', 'N':
		n, err := strconv.Atoi(anchor[1:])
		if err != nil || n <= 0 {
			return l, fmt.Errorf("invalid note anchor %q", anchor)
		}
		l.Note = n
	default:
		return l, fmt.Errorf("invalid anchor %q (use #L12, #L12-L20 or #n3)", anchor)
	}
	return l, nil
}

// ParseWikiLinks extracts all valid [[...]] links from text in order of appearance.
// Links inside fenced code blocks (``` or ~~~) and inline code spans are ignored,
// as are malformed links. Links never span lines.
func ParseWikiLinks(text, scope string) []WikiLink {
	var links []WikiLink
	inFence := false
	fenceMarker := ""
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		lineStart := offset
		offset += len(line)
		trimmed := strings.TrimSpace(line)
		if inFence {
			if strings.HasPrefix(trimmed, fenceMarker) {
				inFence = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = true
			fenceMarker = trimmed[:3]
			continue
		}

		inCode := false
		for i := 0; i < len(line); i++ {
			c := line[i]
			if c == '`' {
				inCode = !inCode
				continue
			}
			if inCode || c != '[' || i+1 >= len(line) || line[i+1] != '[' {
				continue
			}
			closeIdx := strings.Index(line[i+2:], "]]")
			if closeIdx < 0 {
				break
			}
			inner := line[i+2 : i+2+closeIdx]
			end := i + 2 + closeIdx + 2
			if strings.Contains(inner, "[[") {
				// Nested opener: restart scanning from the inner "[[".
				continue
			}
			if l, err := ParseWikiLink(inner, scope); err == nil {
				l.Raw = line[i:end]
				l.Start = lineStart + i
				l.End = lineStart + end
				links = append(links, l)
			}
			i = end - 1
		}
	}
	return links
}

// LinkTargets returns the de-duplicated link targets of the given links in first-seen order.
// Topic targets are prefixed with '@'.
func LinkTargets(links []WikiLink) []string {
	seen := make(map[string]bool, len(links))
	var out []string
	for _, l := range links {
		t := l.Target
		if l.IsTopic {
			t = "@" + t
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// AnchorText returns the text a link's anchor points at inside task t:
// the referenced description line(s) or note. ok is false when the anchor is out
// of range (a "stale" link); links without an anchor return the title.
func (l WikiLink) AnchorText(t *Task) (text string, ok bool) {
	if t == nil {
		return "", false
	}
	switch {
	case l.Note > 0:
		if l.Note > len(t.Notes) {
			return "", false
		}
		return t.Notes[l.Note-1].Content, true
	case l.LineStart > 0:
		lines := strings.Split(t.Description, "\n")
		if l.LineStart > len(lines) {
			return "", false
		}
		end := l.LineStart
		if l.LineEnd > end {
			end = l.LineEnd
		}
		stale := false
		if end > len(lines) {
			end = len(lines)
			stale = true
		}
		return strings.Join(lines[l.LineStart-1:end], "\n"), !stale
	}
	return t.Title, true
}
