package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeTitle(t *testing.T) {
	atLimit := strings.Repeat("a", MaxTitleLength)
	overLimit := strings.Repeat("a", MaxTitleLength+1)
	// Multi-byte runes: the limit is in characters, not bytes.
	unicodeAtLimit := strings.Repeat("é", MaxTitleLength)

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{"trims whitespace", "  Fix bug  ", "Fix bug", ""},
		{"empty", "", "", "cannot be empty"},
		{"whitespace only", "   \t ", "", "cannot be empty"},
		{"exactly at limit", atLimit, atLimit, ""},
		{"one over limit", overLimit, "", "too long"},
		{"unicode counted as runes", unicodeAtLimit, unicodeAtLimit, ""},
		{"newline rejected", "line one\nline two", "", "single line"},
		{"carriage return rejected", "line one\rline two", "", "single line"},
		{"control char rejected", "bell\a", "", "control character"},
		{"inner tab becomes space", "a\tb", "a b", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeTitle(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NormalizeTitle(%q) err = %v, want containing %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeTitle(%q) unexpected err: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	if n := utf8.RuneCountInString(unicodeAtLimit); len(unicodeAtLimit) <= MaxTitleLength || n != MaxTitleLength {
		t.Fatalf("test precondition: unicode title must exceed limit in bytes but not runes")
	}
}

func TestNormalizeTitleErrorReportsLength(t *testing.T) {
	_, err := NormalizeTitle(strings.Repeat("x", 150))
	if err == nil || !strings.Contains(err.Error(), "150") || !strings.Contains(err.Error(), "120") {
		t.Fatalf("expected error to mention actual (150) and max (120) length, got %v", err)
	}
}
