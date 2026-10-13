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

// FindRepoRoot returns the shared repository root directory (resolving through
// git-common-dir when inside a linked git worktree so all worktrees share .tasks/tasks.db).
func FindRepoRoot() (string, error) {
	return FindRepoRootInDir("")
}

// FindRepoRootInDir returns the shared repository root for dir (or CWD when dir is empty).
func FindRepoRootInDir(dir string) (string, error) {
	wtRoot, err := FindWorktreeRootInDir(dir)
	if err != nil {
		return "", err
	}
	var cmd *exec.Cmd
	if dir != "" {
		cmd = exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	} else {
		cmd = exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	}
	out, err := cmd.Output()
	if err == nil {
		commonDir := filepath.Clean(strings.TrimSpace(string(out)))
		if !filepath.IsAbs(commonDir) && dir != "" {
			commonDir = filepath.Clean(filepath.Join(dir, commonDir))
		}
		if filepath.Base(commonDir) == ".git" {
			return filepath.Dir(commonDir), nil
		}
	}
	return wtRoot, nil
}

// FindWorktreeRoot returns the top-level working tree directory of the current checkout.
func FindWorktreeRoot() (string, error) {
	return FindWorktreeRootInDir("")
}

// FindWorktreeRootInDir returns the top-level working tree directory for dir (or CWD when dir is empty).
func FindWorktreeRootInDir(dir string) (string, error) {
	var cmd *exec.Cmd
	if dir != "" {
		cmd = exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	} else {
		cmd = exec.Command("git", "rev-parse", "--show-toplevel")
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	return filepath.Clean(root), nil
}

// ResolveWorktreeDir returns the active worktree directory when the current process is
// inside a linked worktree of repoDir, or repoDir itself otherwise.
func ResolveWorktreeDir(repoDir string) (string, error) {
	if repoDir == "" {
		return FindWorktreeRoot()
	}
	wt, err := FindWorktreeRoot()
	if err == nil && wt != "" {
		if wt == repoDir {
			return wt, nil
		}
		if commonRoot, err := FindRepoRootInDir(wt); err == nil && commonRoot == filepath.Clean(repoDir) {
			return wt, nil
		}
	}
	return repoDir, nil
}

// CurrentBranch returns the checked-out branch name for the worktree at repoDir (or "" if detached).
func CurrentBranch(repoDir string) (string, error) {
	workDir, err := ResolveWorktreeDir(repoDir)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "-C", workDir, "symbolic-ref", "--short", "-q", "HEAD")
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out)), nil
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

// MaxDiffContentBytes caps stored inline diffs at 256KB to prevent database row bloat
// and excessive disk I/O when commits touch large generated assets or lockfiles.
const MaxDiffContentBytes = 256 * 1024

func sanitizeDiffContent(diff []byte) string {
	if len(diff) > MaxDiffContentBytes {
		cut := MaxDiffContentBytes
		// Rewind to the nearest newline in the last 2KB if possible
		lastNL := bytes.LastIndexByte(diff[:cut], '\n')
		if lastNL > cut-2048 {
			cut = lastNL
		} else {
			// Guarantee we do not slice in the middle of a multi-byte UTF-8 rune
			for cut > 0 && (diff[cut]&0xC0) == 0x80 {
				cut--
			}
		}
		return strings.TrimSpace(string(diff[:cut])) + "\n\n... [diff truncated by tk at 256KB to preserve storage efficiency; full patch in git]"
	}
	return strings.TrimSpace(string(diff))
}

func GetLatestCommit(repoDir string) (*CommitInfo, error) {
	return GetCommit(repoDir, "HEAD")
}

func GetCommit(repoDir, ref string) (*CommitInfo, error) {
	var err error
	repoDir, err = ResolveWorktreeDir(repoDir)
	if err != nil {
		return nil, err
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
		info.DiffContent = sanitizeDiffContent(diffOut)
	}

	return info, nil
}

func GetWorkingDiff(repoDir string) (*CommitInfo, error) {
	var err error
	repoDir, err = ResolveWorktreeDir(repoDir)
	if err != nil {
		return nil, err
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

	diffStr := sanitizeDiffContent(diffOut)
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
	fmt.Fprintf(&sb, "commit %s\n", info.SHA)
	fmt.Fprintf(&sb, "Author: %s\n", info.Author)
	fmt.Fprintf(&sb, "Date:   %s\n\n", info.Date)
	fmt.Fprintf(&sb, "    %s\n", info.Subject)
	if info.Body != "" {
		sb.WriteString("\n")
		lines := strings.Split(info.Body, "\n")
		for _, l := range lines {
			fmt.Fprintf(&sb, "    %s\n", l)
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
