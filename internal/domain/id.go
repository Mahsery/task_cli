package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Item ID prefixes. Local repositories use T (tasks) and P (posts); the global
// database uses G (tasks) and GP (posts).
const (
	PrefixTask       = "T"
	PrefixGlobalTask = "G"
	PrefixPost       = "P"
	PrefixGlobalPost = "GP"
)

var knownPrefixes = map[string]bool{
	PrefixTask:       true,
	PrefixGlobalTask: true,
	PrefixPost:       true,
	PrefixGlobalPost: true,
}

// ParseItemID splits a forgiving item reference ("t15", "T15", "t-15", "GP-3")
// into its upper-cased prefix and sequence number. ok is false for anything
// that is not <known prefix><optional dash><digits>.
func ParseItemID(s string) (prefix string, num int, ok bool) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z')) {
		i++
	}
	if i == 0 || i == len(s) {
		return "", 0, false
	}
	prefix = strings.ToUpper(s[:i])
	if !knownPrefixes[prefix] {
		return "", 0, false
	}
	rest := s[i:]
	if rest[0] == '-' {
		rest = rest[1:]
	}
	if rest == "" {
		return "", 0, false
	}
	for j := 0; j < len(rest); j++ {
		if rest[j] < '0' || rest[j] > '9' {
			return "", 0, false
		}
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return prefix, n, true
}

// NormalizeID converts a user-supplied item reference into its canonical form
// (e.g. "t15" -> "T-15", "p-3" -> "P-3"). A bare positive number is interpreted
// as a task in the given scope ("15" -> "T-15" locally, "G-15" globally).
func NormalizeID(s, scope string) (string, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return "", fmt.Errorf("item ID cannot be empty")
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n <= 0 {
			return "", fmt.Errorf("invalid item ID %q", s)
		}
		prefix := PrefixTask
		if scope == ScopeGlobal {
			prefix = PrefixGlobalTask
		}
		return fmt.Sprintf("%s-%d", prefix, n), nil
	}
	prefix, n, ok := ParseItemID(raw)
	if !ok {
		return "", fmt.Errorf("invalid item ID %q (expected e.g. T-15, t15, P-3 or 15)", s)
	}
	return fmt.Sprintf("%s-%d", prefix, n), nil
}

// NormalizeIDs applies NormalizeID to every element, failing on the first invalid one.
func NormalizeIDs(ids []string, scope string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		n, err := NormalizeID(id, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// IsPostID reports whether a canonical ID refers to a post.
func IsPostID(id string) bool {
	prefix, _, ok := ParseItemID(id)
	return ok && (prefix == PrefixPost || prefix == PrefixGlobalPost)
}
