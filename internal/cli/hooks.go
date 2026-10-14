package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/git"

	"github.com/spf13/cobra"
)

const (
	tkHookBegin = "# --- BEGIN TK HOOK ---"
	tkHookEnd   = "# --- END TK HOOK ---"
)

var commitTaskIDRegex = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])([tg]-\d+)(?:\D|$)`)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage git commit hooks (prepare-commit-msg and post-commit) for automatic task tracking",
	Long: `Install, inspect, or remove non-destructive git hooks that integrate tk with git commit:
  - prepare-commit-msg: Automatically tags commit subjects with [T-XX] when on a task-bound branch.
  - post-commit: Automatically attaches new commits to the active or referenced task's commit history.`,
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install prepare-commit-msg and post-commit hooks in the repository",
	RunE: func(_ *cobra.Command, _ []string) error {
		if repoRoot == "" {
			return fmt.Errorf("not inside a git repository")
		}
		hooksDir, err := git.HooksDir(repoRoot)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(hooksDir, 0o750); err != nil {
			return fmt.Errorf("failed to create hooks directory %s: %w", hooksDir, err)
		}

		prepPath := filepath.Join(hooksDir, "prepare-commit-msg")
		prepBlock := strings.Join([]string{
			tkHookBegin,
			`if command -v tk >/dev/null 2>&1; then`,
			`  tk hooks run prepare-commit-msg "$1" "${2:-}" "${3:-}" || true`,
			`fi`,
			tkHookEnd,
		}, "\n")
		if err := installHookBlock(prepPath, prepBlock); err != nil {
			return err
		}

		postPath := filepath.Join(hooksDir, "post-commit")
		postBlock := strings.Join([]string{
			tkHookBegin,
			`if command -v tk >/dev/null 2>&1; then`,
			`  tk hooks run post-commit || true`,
			`fi`,
			tkHookEnd,
		}, "\n")
		if err := installHookBlock(postPath, postBlock); err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(map[string]any{
				"installed":          true,
				"hooks_dir":          hooksDir,
				"prepare_commit_msg": prepPath,
				"post_commit":        postPath,
			})
		}
		fmt.Printf("✓ Installed tk git hooks in %s\n", hooksDir)
		fmt.Printf("  - %s\n  - %s\n", prepPath, postPath)
		return nil
	},
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove tk hook blocks from prepare-commit-msg and post-commit",
	RunE: func(_ *cobra.Command, _ []string) error {
		if repoRoot == "" {
			return fmt.Errorf("not inside a git repository")
		}
		hooksDir, err := git.HooksDir(repoRoot)
		if err != nil {
			return err
		}
		prepPath := filepath.Join(hooksDir, "prepare-commit-msg")
		postPath := filepath.Join(hooksDir, "post-commit")

		if err := uninstallHookBlock(prepPath); err != nil {
			return err
		}
		if err := uninstallHookBlock(postPath); err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(map[string]any{
				"installed": false,
				"hooks_dir": hooksDir,
			})
		}
		fmt.Printf("✓ Removed tk git hooks from %s\n", hooksDir)
		return nil
	},
}

var hooksStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check whether tk git hooks are installed",
	RunE: func(_ *cobra.Command, _ []string) error {
		if repoRoot == "" {
			return fmt.Errorf("not inside a git repository")
		}
		hooksDir, err := git.HooksDir(repoRoot)
		if err != nil {
			return err
		}
		prepInstalled := hasHookBlock(filepath.Join(hooksDir, "prepare-commit-msg"))
		postInstalled := hasHookBlock(filepath.Join(hooksDir, "post-commit"))

		if jsonFlag {
			return outputJSON(map[string]any{
				"hooks_dir":          hooksDir,
				"prepare_commit_msg": prepInstalled,
				"post_commit":        postInstalled,
				"installed":          prepInstalled && postInstalled,
			})
		}
		fmt.Printf("Hooks directory: %s\n", hooksDir)
		fmt.Printf("  prepare-commit-msg: %s\n", hookStatusLabel(prepInstalled))
		fmt.Printf("  post-commit:        %s\n", hookStatusLabel(postInstalled))
		return nil
	},
}

var hooksRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "Internal hook runner invoked by git hooks",
	Hidden: true,
}

var hooksRunPrepareCommitMsgCmd = &cobra.Command{
	Use:    "prepare-commit-msg <file> [source] [sha]",
	Short:  "Annotate commit message with active branch task ID",
	Hidden: true,
	Args:   cobra.RangeArgs(1, 3),
	RunE: func(_ *cobra.Command, args []string) error {
		msgFile := args[0]
		var source string
		if len(args) > 1 {
			source = strings.TrimSpace(args[1])
		}
		if source == "merge" || source == "squash" || source == "commit" {
			return nil
		}

		taskID, err := resolveOrInferTaskID(nil)
		if err != nil || taskID == "" {
			return nil
		}

		data, err := os.ReadFile(msgFile)
		if err != nil {
			return nil
		}
		updated, changed := injectTaskIDIntoCommitMessage(string(data), taskID)
		if !changed {
			return nil
		}
		_ = os.WriteFile(msgFile, []byte(updated), 0o600)
		return nil
	},
}

var hooksRunPostCommitCmd = &cobra.Command{
	Use:    "post-commit",
	Short:  "Attach HEAD commit to the active branch task or referenced task ID",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if repoRoot == "" {
			return nil
		}
		info, err := git.GetCommit(repoRoot, "HEAD")
		if err != nil || info == nil {
			return nil
		}

		taskID, _ := resolveOrInferTaskID(nil)
		if taskID == "" {
			if m := commitTaskIDRegex.FindStringSubmatch(info.Subject); len(m) >= 2 {
				taskID = normID(m[1])
			}
		}
		if taskID == "" {
			return nil
		}

		commits := []domain.TaskCommit{
			{
				SHA:         info.SHA,
				Subject:     info.Subject,
				Author:      info.Author,
				CommittedAt: info.Date,
				DiffStat:    info.DiffStat,
				DiffContent: info.DiffContent,
			},
		}
		_, _ = taskClient.AttachGitCommits(taskID, commits, false, false)
		return nil
	},
}

func installHookBlock(path, block string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	content := string(existing)
	if strings.TrimSpace(content) == "" {
		content = "#!/usr/bin/env sh\n\n" + block + "\n"
	} else if startIdx := strings.Index(content, tkHookBegin); startIdx >= 0 {
		if endIdx := strings.Index(content[startIdx:], tkHookEnd); endIdx >= 0 {
			endPos := startIdx + endIdx + len(tkHookEnd)
			content = content[:startIdx] + block + content[endPos:]
		} else {
			content = strings.TrimRight(content, "\n") + "\n\n" + block + "\n"
		}
	} else {
		content = strings.TrimRight(content, "\n") + "\n\n" + block + "\n"
	}
	// Git hooks must be executable (0o755)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil { //nolint:gosec // G306: git hook scripts must have executable permission
		return fmt.Errorf("failed to write hook %s: %w", path, err)
	}
	return nil
}

func uninstallHookBlock(path string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	content := string(existing)
	startIdx := strings.Index(content, tkHookBegin)
	if startIdx < 0 {
		return nil
	}
	endIdx := strings.Index(content[startIdx:], tkHookEnd)
	if endIdx < 0 {
		return nil
	}
	endPos := startIdx + endIdx + len(tkHookEnd)
	remaining := strings.TrimSpace(content[:startIdx] + content[endPos:])
	if remaining == "" || remaining == "#!/usr/bin/env sh" || remaining == "#!/bin/sh" || remaining == "#!/usr/bin/env bash" || remaining == "#!/bin/bash" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove empty hook %s: %w", path, err)
		}
		return nil
	}
	if err := os.WriteFile(path, []byte(remaining+"\n"), 0o755); err != nil { //nolint:gosec // G306: git hook scripts must keep executable permission
		return fmt.Errorf("failed to update hook %s: %w", path, err)
	}
	return nil
}

func hasHookBlock(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), tkHookBegin) && strings.Contains(string(data), tkHookEnd)
}

func hookStatusLabel(installed bool) string {
	if installed {
		return "installed"
	}
	return "not installed"
}

func injectTaskIDIntoCommitMessage(msg, taskID string) (string, bool) {
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	if taskID == "" {
		return msg, false
	}
	// If the commit message already contains the taskID (case-insensitive), leave it unchanged.
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])` + regexp.QuoteMeta(taskID) + `([^0-9]|$)`)
	lines := strings.Split(msg, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if re.MatchString(trimmed) {
			return msg, false
		}
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "" {
			lines[i] = fmt.Sprintf("[%s] ", taskID)
		} else {
			lines[i] = fmt.Sprintf("%s [%s]", strings.TrimRight(line, " \t\r"), taskID)
		}
		return strings.Join(lines, "\n"), true
	}
	return fmt.Sprintf("[%s]\n%s", taskID, msg), true
}

func init() {
	hooksRunCmd.AddCommand(hooksRunPrepareCommitMsgCmd)
	hooksRunCmd.AddCommand(hooksRunPostCommitCmd)
	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksUninstallCmd)
	hooksCmd.AddCommand(hooksStatusCmd)
	hooksCmd.AddCommand(hooksRunCmd)
	RootCmd.AddCommand(hooksCmd)
}
