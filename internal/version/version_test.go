package version

import (
	"strings"
	"testing"
)

func TestVersionGet(t *testing.T) {
	info := Get()
	if info.Version == "" {
		t.Errorf("expected non-empty version")
	}
	if info.GoVersion == "" {
		t.Errorf("expected non-empty go version")
	}
	if info.Platform == "" {
		t.Errorf("expected non-empty platform")
	}
}

func TestVersionString(t *testing.T) {
	s := String()
	if !strings.HasPrefix(s, "tk ") {
		t.Errorf("expected version string to start with 'tk ', got: %s", s)
	}
}
