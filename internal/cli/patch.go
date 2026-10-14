package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"task_cli/internal/domain"
	"task_cli/internal/git"

	"github.com/spf13/cobra"
)

var patchOutputDir string

type patchEntry struct {
	TaskID      string `json:"task_id"`
	TaskTitle   string `json:"task_title"`
	SHA         string `json:"sha"`
	Subject     string `json:"subject"`
	Author      string `json:"author"`
	CommittedAt string `json:"committed_at"`
	DiffStat    string `json:"diff_stat,omitempty"`
	DiffContent string `json:"diff_content"`
}

var patchCmd = &cobra.Command{
	Use:   "patch [id|@topic]",
	Short: "Export task or topic commits as a numbered [PATCH M/N] series with cover letter",
	Long: `Export attached git commits and diffs for a task (or an entire @topic in dependency order)
as a standard git format-patch / kernel-style patch series, including a 0000-cover-letter.patch
containing the task specification, discussion notes, and dependency metadata.

When [id|@topic] is omitted, the task is inferred from the active git branch.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		var arg string
		if len(args) > 0 {
			arg = strings.TrimSpace(args[0])
		}

		var tasks []*domain.Task
		var seriesTitle string
		if strings.HasPrefix(arg, "@") {
			topicRef := domain.NormalizeTopicRef(arg)
			seriesTitle = fmt.Sprintf("@%s patch series", topicRef)
			listed, err := taskClient.ListTasks(domain.TaskFilter{
				Scope:          scope,
				RepoPath:       repoRoot,
				Topic:          topicRef,
				TopicRecursive: true,
				Kind:           domain.KindTask,
			})
			if err != nil {
				return err
			}
			sorted := topoSortTasksForPatch(listed)
			for _, summary := range sorted {
				full, err := taskClient.GetTask(summary.ID)
				if err != nil {
					continue
				}
				tasks = append(tasks, full)
			}
		} else {
			var rawArgs []string
			if arg != "" {
				rawArgs = []string{arg}
			}
			id, err := resolveOrInferTaskID(rawArgs)
			if err != nil {
				return err
			}
			task, err := taskClient.GetTask(id)
			if err != nil {
				return err
			}
			tasks = []*domain.Task{task}
			seriesTitle = fmt.Sprintf("[%s] %s", task.ID, task.Title)
		}

		entries := collectPatchEntries(tasks)
		if len(entries) == 0 {
			return fmt.Errorf("no attached commits or diffs found to export")
		}

		coverLetter := formatCoverLetter(seriesTitle, tasks, entries)
		renderedPatches := make([]string, len(entries))
		filenames := make([]string, len(entries)+1)
		filenames[0] = "0000-cover-letter.patch"

		for i := range entries {
			renderedPatches[i] = formatPatchEntry(&entries[i], i+1, len(entries))
			slug := patchSlug(entries[i].Subject)
			if slug == "" {
				slug = strings.ToLower(entries[i].TaskID)
			}
			filenames[i+1] = fmt.Sprintf("%04d-%s.patch", i+1, slug)
		}

		if strings.TrimSpace(patchOutputDir) != "" {
			if err := os.MkdirAll(patchOutputDir, 0o750); err != nil {
				return fmt.Errorf("failed to create output directory %s: %w", patchOutputDir, err)
			}
			var writtenPaths []string
			coverPath := filepath.Join(patchOutputDir, filenames[0])
			if err := os.WriteFile(coverPath, []byte(coverLetter), 0o600); err != nil {
				return fmt.Errorf("failed to write %s: %w", coverPath, err)
			}
			writtenPaths = append(writtenPaths, coverPath)

			for i, content := range renderedPatches {
				p := filepath.Join(patchOutputDir, filenames[i+1])
				if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
					return fmt.Errorf("failed to write %s: %w", p, err)
				}
				writtenPaths = append(writtenPaths, p)
			}

			if jsonFlag {
				return outputJSON(map[string]any{
					"series_title": seriesTitle,
					"patch_count":  len(entries),
					"files":        writtenPaths,
				})
			}
			for _, p := range writtenPaths {
				fmt.Println(p)
			}
			return nil
		}

		if jsonFlag {
			return outputJSON(map[string]any{
				"series_title": seriesTitle,
				"cover_letter": coverLetter,
				"patches":      entries,
			})
		}

		fmt.Print(coverLetter)
		for _, p := range renderedPatches {
			fmt.Println()
			fmt.Print(p)
		}
		return nil
	},
}

func collectPatchEntries(tasks []*domain.Task) []patchEntry {
	var entries []patchEntry
	for _, t := range tasks {
		if len(t.Commits) > 0 {
			for i := range t.Commits {
				c := &t.Commits[i]
				diffContent := c.DiffContent
				diffStat := c.DiffStat
				author := c.Author
				committedAt := c.CommittedAt
				if diffContent == "" && repoRoot != "" && c.SHA != "" && c.SHA != "working-tree" {
					if info, err := git.GetCommit(repoRoot, c.SHA); err == nil {
						diffContent = info.DiffContent
						if diffStat == "" {
							diffStat = info.DiffStat
						}
						if author == "" {
							author = info.Author
						}
						if committedAt == "" {
							committedAt = info.Date
						}
					}
				}
				if diffContent == "" && i == len(t.Commits)-1 && t.DiffContent != "" {
					diffContent = t.DiffContent
				}
				if author == "" {
					author = t.Assignee
					if author == "" {
						author = t.Author
					}
				}
				if committedAt == "" {
					committedAt = t.UpdatedAt.UTC().Format(time.RFC1123Z)
				}
				subj := strings.TrimSpace(c.Subject)
				if subj == "" {
					subj = fmt.Sprintf("[%s] %s", t.ID, t.Title)
				}
				entries = append(entries, patchEntry{
					TaskID:      t.ID,
					TaskTitle:   t.Title,
					SHA:         c.SHA,
					Subject:     subj,
					Author:      author,
					CommittedAt: committedAt,
					DiffStat:    diffStat,
					DiffContent: diffContent,
				})
			}
			continue
		}
		if strings.TrimSpace(t.DiffContent) != "" || strings.TrimSpace(t.CommitSHA) != "" {
			sha := t.CommitSHA
			if sha == "" {
				sha = "0000000000000000000000000000000000000000"
			}
			subj := strings.TrimSpace(t.CommitMsg)
			if subj == "" {
				subj = fmt.Sprintf("[%s] %s", t.ID, t.Title)
			}
			author := t.Assignee
			if author == "" {
				author = t.Author
			}
			entries = append(entries, patchEntry{
				TaskID:      t.ID,
				TaskTitle:   t.Title,
				SHA:         sha,
				Subject:     subj,
				Author:      author,
				CommittedAt: t.UpdatedAt.UTC().Format(time.RFC1123Z),
				DiffStat:    t.DiffStat,
				DiffContent: t.DiffContent,
			})
		}
	}
	return entries
}

func formatCoverLetter(seriesTitle string, tasks []*domain.Task, entries []patchEntry) string {
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC1123Z)
	author := "tk"
	if len(tasks) > 0 {
		if tasks[0].Assignee != "" {
			author = tasks[0].Assignee
		} else if tasks[0].Author != "" {
			author = tasks[0].Author
		}
	}
	fmt.Fprintf(&b, "From 0000000000000000000000000000000000000000 Mon Sep 17 00:00:00 2001\n")
	fmt.Fprintf(&b, "From: %s\n", author)
	fmt.Fprintf(&b, "Date: %s\n", now)
	fmt.Fprintf(&b, "Subject: [PATCH 0/%d] %s\n\n", len(entries), seriesTitle)

	for _, t := range tasks {
		fmt.Fprintf(&b, "=== %s: %s (status: %s, priority: %s) ===\n", t.ID, t.Title, t.Status, t.Priority)
		if t.Branch != "" {
			fmt.Fprintf(&b, "Branch: %s\n", t.Branch)
		}
		if len(t.DependsOn) > 0 {
			fmt.Fprintf(&b, "Depends-On: %s\n", strings.Join(t.DependsOn, ", "))
		}
		if len(t.Blocks) > 0 {
			fmt.Fprintf(&b, "Blocks: %s\n", strings.Join(t.Blocks, ", "))
		}
		if strings.TrimSpace(t.Description) != "" {
			fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(t.Description))
		}
		if len(t.Notes) > 0 {
			fmt.Fprintf(&b, "\nNotes:\n")
			for _, n := range t.Notes {
				fmt.Fprintf(&b, "  - [%s] %s\n", n.Author, strings.TrimSpace(n.Content))
			}
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintf(&b, "Commits (%d):\n", len(entries))
	for i := range entries {
		e := &entries[i]
		fmt.Fprintf(&b, "  [%d/%d] %s %s (%s)\n", i+1, len(entries), shortSHA(e.SHA), e.Subject, e.TaskID)
	}
	fmt.Fprintf(&b, "\n-- \ntk patch\n")
	return b.String()
}

func formatPatchEntry(e *patchEntry, idx, total int) string {
	var b strings.Builder
	sha := e.SHA
	if sha == "" {
		sha = "0000000000000000000000000000000000000000"
	}
	author := e.Author
	if author == "" {
		author = "tk"
	}
	date := e.CommittedAt
	if date == "" {
		date = time.Now().UTC().Format(time.RFC1123Z)
	}
	prefix := "[PATCH]"
	if total > 1 {
		prefix = fmt.Sprintf("[PATCH %d/%d]", idx, total)
	}
	fmt.Fprintf(&b, "From %s Mon Sep 17 00:00:00 2001\n", sha)
	fmt.Fprintf(&b, "From: %s\n", author)
	fmt.Fprintf(&b, "Date: %s\n", date)
	fmt.Fprintf(&b, "Subject: %s %s\n", prefix, e.Subject)
	fmt.Fprintf(&b, "Task-ID: %s\n\n", e.TaskID)
	fmt.Fprintf(&b, "---\n")
	if strings.TrimSpace(e.DiffStat) != "" {
		fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(e.DiffStat))
	}
	if strings.TrimSpace(e.DiffContent) != "" {
		fmt.Fprintf(&b, "%s\n", strings.TrimRight(e.DiffContent, "\n"))
	}
	fmt.Fprintf(&b, "-- \ntk patch\n")
	return b.String()
}

func patchSlug(subject string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(subject) {
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
		if b.Len() >= 48 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func topoSortTasksForPatch(tasks []*domain.Task) []*domain.Task {
	byID := make(map[string]*domain.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	visited := make(map[string]bool, len(tasks))
	var out []*domain.Task

	sorted := make([]*domain.Task, len(tasks))
	copy(sorted, tasks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt) ||
			(sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) && sorted[i].ID < sorted[j].ID)
	})

	var visit func(t *domain.Task)
	visit = func(t *domain.Task) {
		if t == nil || visited[t.ID] {
			return
		}
		visited[t.ID] = true
		for _, depID := range t.DependsOn {
			if prereq, ok := byID[depID]; ok {
				visit(prereq)
			}
		}
		out = append(out, t)
	}
	for _, t := range sorted {
		visit(t)
	}
	return out
}

func init() {
	patchCmd.Flags().StringVarP(&patchOutputDir, "output-dir", "o", "", "Write 0000-cover-letter.patch and 000N-<slug>.patch files to directory")
	RootCmd.AddCommand(patchCmd)
}
