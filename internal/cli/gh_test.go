package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"task_cli/internal/domain"
)

func TestGitHubHelpersAndMockGHExecution(t *testing.T) {
	task := &domain.Task{
		ID:          "T-87",
		Title:       "Zero-Config GitHub Integration",
		Description: "Delegate auth to gh CLI.",
		Priority:    domain.PriorityHigh,
		Status:      domain.StatusInProgress,
		Assignee:    "alice",
		TopicPath:   "github-integration",
		Branch:      "tk/t-87-zero-config-github",
		ExternalRef: "gh#42",
		PRURL:       "https://github.com/org/repo/pull/101",
		DependsOn:   []string{"T-85"},
		Blocks:      []string{"T-90"},
		Commits: []domain.TaskCommit{
			{SHA: "abcdef1234567", Subject: "feat(gh): add tk pr and tk gh [T-87]"},
		},
		Notes: []domain.TaskNote{
			{Author: "alice", Content: "Ready for review"},
		},
	}

	if got := parseGHIssueNumber("gh#42"); got != "42" {
		t.Errorf("parseGHIssueNumber(gh#42) = %q, want 42", got)
	}
	if got := parseGHIssueNumber("GH-99"); got != "99" {
		t.Errorf("parseGHIssueNumber(GH-99) = %q, want 99", got)
	}
	if got := parseGHIssueNumber("jira-12"); got != "" {
		t.Errorf("parseGHIssueNumber(jira-12) = %q, want empty", got)
	}

	prBody := buildPRBodyForTask(task)
	for _, want := range []string{
		"## Task `T-87`: Zero-Config GitHub Integration",
		"Closes #42",
		"`tk/t-87-zero-config-github`",
		"`abcdef1` feat(gh): add tk pr and tk gh [T-87]",
		"**alice**: Ready for review",
	} {
		if !strings.Contains(prBody, want) {
			t.Errorf("buildPRBodyForTask missing %q in:\n%s", want, prBody)
		}
	}

	syncComment := buildGHIssueSyncComment(task)
	if !strings.Contains(syncComment, "https://github.com/org/repo/pull/101") || !strings.Contains(syncComment, "Ready for review") {
		t.Errorf("unexpected sync comment:\n%s", syncComment)
	}

	if got := extractLastURL("Creating pull request...\nhttps://github.com/org/repo/pull/101\n"); got != "https://github.com/org/repo/pull/101" {
		t.Errorf("extractLastURL = %q", got)
	}

	// Verify mock gh execution in PATH
	binDir := t.TempDir()
	mockGH := filepath.Join(binDir, "gh")
	script := "#!/usr/bin/env sh\necho 'https://github.com/org/repo/pull/55'\n"
	if err := os.WriteFile(mockGH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := runGH("pr", "create", "--title", "test")
	if err != nil {
		t.Fatalf("runGH with mock gh failed: %v", err)
	}
	if extractLastURL(out) != "https://github.com/org/repo/pull/55" {
		t.Fatalf("unexpected runGH output: %q", out)
	}

	// Verify command registration
	for _, path := range [][]string{
		{"pr", "create"},
		{"pr", "status"},
		{"gh", "import"},
		{"gh", "push"},
	} {
		cmd, _, err := RootCmd.Find(path)
		if err != nil || cmd == nil {
			t.Errorf("command %v not registered: %v", path, err)
		}
	}
}
