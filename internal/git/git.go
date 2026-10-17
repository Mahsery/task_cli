package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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

var branchTaskIDRegex = regexp.MustCompile(`(?i)(?:^|/)([tg])-?(\d+)(?:$|[-_/])`)

// BranchSlugForTask builds a deterministic branch name for a task (e.g., "tk/t-84-branch-bound-tasks").
func BranchSlugForTask(id, title string) string {
	idSlug := strings.ToLower(strings.TrimSpace(id))
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 40 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "tk/" + idSlug
	}
	return fmt.Sprintf("tk/%s-%s", idSlug, slug)
}

// InferTaskIDFromBranchName extracts a canonical task ID (e.g. "T-84") from a branch name
// such as "tk/t-84-branch-bound-tasks" or "feat/T-12-fix", or returns "" if none is found.
func InferTaskIDFromBranchName(branch string) string {
	m := branchTaskIDRegex.FindStringSubmatch(strings.TrimSpace(branch))
	if len(m) < 3 {
		return ""
	}
	return fmt.Sprintf("%s-%s", strings.ToUpper(m[1]), m[2])
}

// CheckoutOrCreateBranch switches to branchName if it already exists locally, or creates and checks it out.
func CheckoutOrCreateBranch(repoDir, branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	workDir, err := ResolveWorktreeDir(repoDir)
	if err != nil {
		return err
	}
	checkCmd := exec.Command("git", "-C", workDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
	if checkCmd.Run() == nil {
		out, err := exec.Command("git", "-C", workDir, "checkout", branchName).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git checkout %s failed: %s (%w)", branchName, strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	out, err := exec.Command("git", "-C", workDir, "checkout", "-b", branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout -b %s failed: %s (%w)", branchName, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// HooksDir resolves the active git hooks directory for repoDir (respecting core.hooksPath and worktrees).
func HooksDir(repoDir string) (string, error) {
	workDir, err := ResolveWorktreeDir(repoDir)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "-C", workDir, "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve git hooks path: %w", err)
	}
	p := strings.TrimSpace(string(out))
	if !filepath.IsAbs(p) {
		p = filepath.Join(workDir, p)
	}
	return filepath.Clean(p), nil
}

// FindRepoRoot returns the repository root directory. Inside a linked git worktree,
// it resolves through git-common-dir so worktrees share the main .tasks/tasks.db by default,
// unless the worktree has its own .tasks/ directory or TK_WORKTREE_DB is set.
func FindRepoRoot() (string, error) {
	return FindRepoRootInDir("")
}

// FindRepoRootInDir returns the repository root for dir (or CWD when dir is empty).
func FindRepoRootInDir(dir string) (string, error) {
	wtRoot, err := FindWorktreeRootInDir(dir)
	if err != nil {
		return "", err
	}
	if os.Getenv("TK_WORKTREE_DB") != "" {
		return wtRoot, nil
	}
	if info, err := os.Stat(filepath.Join(wtRoot, ".tasks")); err == nil && info.IsDir() {
		return wtRoot, nil
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

func samePath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// ResolveWorktreeDir returns the active worktree directory when the current process is
// inside a linked worktree of repoDir, or repoDir itself otherwise.
func ResolveWorktreeDir(repoDir string) (string, error) {
	if repoDir == "" {
		return FindWorktreeRoot()
	}
	wt, err := FindWorktreeRoot()
	if err == nil && wt != "" {
		if samePath(wt, repoDir) {
			return wt, nil
		}
		if commonRoot, err := FindRepoRootInDir(wt); err == nil && samePath(commonRoot, repoDir) {
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
	if u == "" {
		u = os.Getenv("USERNAME")
	}
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

// GetCommitRange resolves a git revision range (e.g. "main..HEAD" or "HEAD~3..HEAD")
// in chronological order (oldest first).
func GetCommitRange(repoDir, rangeSpec string) ([]*CommitInfo, error) {
	workDir, err := ResolveWorktreeDir(repoDir)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "-C", workDir, "rev-list", "--reverse", rangeSpec)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve commit range %q: %w", rangeSpec, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var commits []*CommitInfo
	for _, line := range lines {
		sha := strings.TrimSpace(line)
		if sha == "" {
			continue
		}
		info, err := GetCommit(workDir, sha)
		if err != nil {
			return nil, err
		}
		commits = append(commits, info)
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("no commits found in range %q", rangeSpec)
	}
	return commits, nil
}

// ScanCommitsForTask searches git log for commits whose subject or body references taskID (e.g. "T-12" or "[T-12]"),
// returning matching commits in chronological order (oldest first).
func ScanCommitsForTask(repoDir, taskID, rangeSpec string) ([]*CommitInfo, error) {
	workDir, err := ResolveWorktreeDir(repoDir)
	if err != nil {
		return nil, err
	}
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	if taskID == "" {
		return nil, fmt.Errorf("task ID cannot be empty")
	}
	args := []string{"-C", workDir, "log", "--reverse", "--regexp-ignore-case", "--extended-regexp", "--grep=(^|[^A-Za-z0-9])" + regexp.QuoteMeta(taskID) + "([^0-9]|$)", "--format=%H"}
	if strings.TrimSpace(rangeSpec) != "" {
		args = append(args, strings.TrimSpace(rangeSpec))
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to scan git log for %s: %w", taskID, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var commits []*CommitInfo
	for _, line := range lines {
		sha := strings.TrimSpace(line)
		if sha == "" {
			continue
		}
		info, err := GetCommit(workDir, sha)
		if err != nil {
			return nil, err
		}
		commits = append(commits, info)
	}
	return commits, nil
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
