package version

import (
	"fmt"
	"runtime"
)

var (
	// Version is overridden at build time via -ldflags.
	Version = "0.2.0-dev"
	// Commit is overridden at build time via -ldflags.
	Commit = "none"
	// BuildDate is overridden at build time via -ldflags.
	BuildDate = "unknown"
)

// Info holds structured version metadata.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Compiler  string `json:"compiler"`
	Platform  string `json:"platform"`
}

// Get returns the full Info struct.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String returns a single line human-readable version string.
func String() string {
	if Commit != "none" && Commit != "unknown" {
		return fmt.Sprintf("tk %s (%s, %s)", Version, Commit, BuildDate)
	}
	return fmt.Sprintf("tk %s", Version)
}
