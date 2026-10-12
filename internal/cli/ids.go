package cli

import "task_cli/internal/domain"

// normID canonicalises a forgiving item reference for the current scope
// ("t15", "T15", "t-15" and "15" all become "T-15"). Invalid input is returned
// trimmed but otherwise unchanged so the daemon can report a precise error.
func normID(s string) string {
	if n, err := domain.NormalizeID(s, scope); err == nil {
		return n
	}
	return s
}

func normIDs(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = normID(id)
	}
	return out
}

// normIDIfSet is normID for optional flags: empty stays empty.
func normIDIfSet(s string) string {
	if s == "" {
		return ""
	}
	return normID(s)
}
