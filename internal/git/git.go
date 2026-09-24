package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CommitInfo struct {
	SHA         string
	ShortSHA    string
	Author      string
	Date        string
	Subject     string
	Body        string
	DiffStat    string
	DiffContent string
}

func FindRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	return filepath.Clean(root), nil
}

func GetAuthor() string {
	cmdName := exec.Command("git", "config", "user.name")
	nameOut, err := cmdName.Output()
	name := strings.TrimSpace(string(nameOut))

	cmdEmail := exec.Command("git", "config", "user.email")
	emailOut, err2 := cmdEmail.Output()
	email := strings.TrimSpace(string(emailOut))

	if err == nil && err2 == nil && name != "" && email != "" {
		return fmt.Sprintf("%s <%s>", name, email)
	}
	if name != "" {
		return name
	}
	u := os.Getenv("USER")
	if u != "" {
		return u
	}
	return "anonymous"
}

func GetLatestCommit(repoDir string) (*CommitInfo, error) {
	return GetCommit(repoDir, "HEAD")
}

func GetCommit(repoDir, ref string) (*CommitInfo, error) {
	if repoDir == "" {
		var err error
		repoDir, err = FindRepoRoot()
		if err != nil {
			return nil, err
		}
	}

	// 1. Get commit metadata (SHA, short SHA, author, date, subject, body)
	// %H = commit hash, %h = short hash, %an <%ae> = author, %ad = date, %s = subject, %b = body
	format := "%H%x00%h%x00%an <%ae>%x00%ad%x00%s%x00%b"
	cmd := exec.Command("git", "-C", repoDir, "log", "-1", fmt.Sprintf("--format=%s", format), ref)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit %q: %w", ref, err)
	}

	parts := bytes.Split(out, []byte{0})
	if len(parts) < 6 {
		return nil, fmt.Errorf("unexpected git log output for %q", ref)
	}

	info := &CommitInfo{
		SHA:      string(parts[0]),
		ShortSHA: string(parts[1]),
		Author:   string(parts[2]),
		Date:     string(parts[3]),
		Subject:  string(parts[4]),
		Body:     strings.TrimSpace(string(parts[5])),
	}

	// 2. Get diffstat
	cmdStat := exec.Command("git", "-C", repoDir, "show", "--stat", "--format=", ref)
	statOut, err := cmdStat.Output()
	if err == nil {
		info.DiffStat = strings.TrimSpace(string(statOut))
	}

	// 3. Get diff content
	cmdDiff := exec.Command("git", "-C", repoDir, "show", "--patch", "--format=", ref)
	diffOut, err := cmdDiff.Output()
	if err == nil {
		info.DiffContent = strings.TrimSpace(string(diffOut))
	}

	return info, nil
}

func GetWorkingDiff(repoDir string) (*CommitInfo, error) {
	if repoDir == "" {
		var err error
		repoDir, err = FindRepoRoot()
		if err != nil {
			return nil, err
		}
	}

	// Diffstat of unstaged + staged
	cmdStat := exec.Command("git", "-C", repoDir, "diff", "HEAD", "--stat")
	statOut, _ := cmdStat.Output()

	// Patch of unstaged + staged
	cmdDiff := exec.Command("git", "-C", repoDir, "diff", "HEAD", "--patch")
	diffOut, err := cmdDiff.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get working diff: %w", err)
	}

	diffStr := strings.TrimSpace(string(diffOut))
	if diffStr == "" {
		return nil, fmt.Errorf("no uncommitted changes found in working tree")
	}

	return &CommitInfo{
		SHA:         "working-tree",
		ShortSHA:    "working",
		Author:      GetAuthor(),
		Date:        "uncommitted changes",
		Subject:     "Working tree changes",
		DiffStat:    strings.TrimSpace(string(statOut)),
		DiffContent: diffStr,
	}, nil
}

func FormatKernelPatch(info *CommitInfo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("commit %s\n", info.SHA))
	sb.WriteString(fmt.Sprintf("Author: %s\n", info.Author))
	sb.WriteString(fmt.Sprintf("Date:   %s\n\n", info.Date))
	sb.WriteString(fmt.Sprintf("    %s\n", info.Subject))
	if info.Body != "" {
		sb.WriteString("\n")
		lines := strings.Split(info.Body, "\n")
		for _, l := range lines {
			sb.WriteString(fmt.Sprintf("    %s\n", l))
		}
	}
	sb.WriteString("\n")
	if info.DiffStat != "" {
		sb.WriteString(info.DiffStat)
		sb.WriteString("\n\n")
	}
	if info.DiffContent != "" {
		sb.WriteString(info.DiffContent)
		sb.WriteString("\n")
	}
	return sb.String()
}
