package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"

	"github.com/spf13/cobra"
)

var (
	prDraft bool
	prBase  string
	prHead  string

	ghImportLabel string
	ghImportTopic string
	ghImportLimit int
)

var ghIssueRefRegex = regexp.MustCompile(`(?i)^(?:gh[#-]?)?(\d+)$`)
var ghURLNumberRegex = regexp.MustCompile(`/(?:issues|pull)/(\d+)(?:$|[/?#])`)

type ghIssuePayload struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

type ghPRViewPayload struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"isDraft"`
	Mergeable      string `json:"mergeable"`
	ReviewDecision string `json:"reviewDecision"`
}

var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Create and inspect GitHub Pull Requests linked to tasks (via gh CLI)",
}

var prCreateCmd = &cobra.Command{
	Use:   "create [id]",
	Short: "Create a GitHub Pull Request from a task specification and bind its URL",
	Long: `Synthesizes a GitHub Pull Request title and Markdown body from the task's
specification, dependency chain, commit history, and notes, invokes 'gh pr create',
and records the resulting PR URL on the task.

When [id] is omitted, the active task is inferred from the current git branch.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		id, err := resolveOrInferTaskID(args)
		if err != nil {
			return err
		}
		task, err := taskClient.GetTask(id)
		if err != nil {
			return err
		}

		title := fmt.Sprintf("[%s] %s", task.ID, task.Title)
		body := buildPRBodyForTask(task)

		ghArgs := []string{"pr", "create", "--title", title, "--body", body}
		if prDraft {
			ghArgs = append(ghArgs, "--draft")
		}
		if strings.TrimSpace(prBase) != "" {
			ghArgs = append(ghArgs, "--base", strings.TrimSpace(prBase))
		}
		if strings.TrimSpace(prHead) != "" {
			ghArgs = append(ghArgs, "--head", strings.TrimSpace(prHead))
		} else if strings.TrimSpace(task.Branch) != "" {
			ghArgs = append(ghArgs, "--head", strings.TrimSpace(task.Branch))
		}

		out, err := runGH(ghArgs...)
		if err != nil {
			return err
		}
		prURL := extractLastURL(out)
		if prURL == "" {
			prURL = strings.TrimSpace(out)
		}

		actor := resolveActor("")
		note := fmt.Sprintf("Opened Pull Request: %s", prURL)
		updated, err := taskClient.UpdateTask(protocol.UpdateTaskPayload{
			ID:      task.ID,
			PRURL:   &prURL,
			AddNote: &note,
			Author:  actor,
		})
		if err != nil {
			return err
		}

		if jsonFlag {
			return outputJSON(updated)
		}
		fmt.Printf("✓ Created PR for %s: %s\n", updated.ID, updated.PRURL)
		return nil
	},
}

var prStatusCmd = &cobra.Command{
	Use:   "status [id]",
	Short: "Show GitHub Pull Request status for a task",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		id, err := resolveOrInferTaskID(args)
		if err != nil {
			return err
		}
		task, err := taskClient.GetTask(id)
		if err != nil {
			return err
		}

		target := strings.TrimSpace(task.PRURL)
		if target == "" {
			target = strings.TrimSpace(task.Branch)
		}
		ghArgs := []string{"pr", "view"}
		if target != "" {
			ghArgs = append(ghArgs, target)
		}
		ghArgs = append(ghArgs, "--json", "number,title,url,state,isDraft,mergeable,reviewDecision")

		out, err := runGH(ghArgs...)
		if err != nil {
			return err
		}

		var pr ghPRViewPayload
		if err := json.Unmarshal([]byte(out), &pr); err != nil {
			return fmt.Errorf("failed to parse gh pr view JSON: %w", err)
		}

		if pr.URL != "" && pr.URL != task.PRURL {
			actor := resolveActor("")
			_, _ = taskClient.UpdateTask(protocol.UpdateTaskPayload{
				ID:     task.ID,
				PRURL:  &pr.URL,
				Author: actor,
			})
		}

		if jsonFlag {
			return outputJSON(pr)
		}
		draftStr := ""
		if pr.IsDraft {
			draftStr = " (DRAFT)"
		}
		fmt.Printf("PR #%d: %s [%s%s]\n", pr.Number, pr.Title, pr.State, draftStr)
		fmt.Printf("  URL:             %s\n", pr.URL)
		if pr.ReviewDecision != "" {
			fmt.Printf("  Review Decision: %s\n", pr.ReviewDecision)
		}
		if pr.Mergeable != "" {
			fmt.Printf("  Mergeable:       %s\n", pr.Mergeable)
		}
		return nil
	},
}

var ghCmd = &cobra.Command{
	Use:   "gh",
	Short: "Synchronize tasks with GitHub Issues via the gh CLI",
}

var ghImportCmd = &cobra.Command{
	Use:   "import [issue-number]",
	Short: "Import one or more GitHub Issues into tk tasks idempotently (deduplicated via gh#<number>)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		var issues []ghIssuePayload
		if len(args) == 1 {
			numStr := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(args[0])), "#")
			out, err := runGH("issue", "view", numStr, "--json", "number,title,body,url,state,author")
			if err != nil {
				return err
			}
			var single ghIssuePayload
			if err := json.Unmarshal([]byte(out), &single); err != nil {
				return fmt.Errorf("failed to parse gh issue view JSON: %w", err)
			}
			issues = append(issues, single)
		} else {
			limit := ghImportLimit
			if limit <= 0 {
				limit = 30
			}
			ghArgs := []string{"issue", "list", "--state", "open", "--limit", strconv.Itoa(limit), "--json", "number,title,body,url,state,author"}
			if strings.TrimSpace(ghImportLabel) != "" {
				ghArgs = append(ghArgs, "--label", strings.TrimSpace(ghImportLabel))
			}
			out, err := runGH(ghArgs...)
			if err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(out), &issues); err != nil {
				return fmt.Errorf("failed to parse gh issue list JSON: %w", err)
			}
		}

		actor := resolveActor("")
		var imported []*domain.Task
		for _, iss := range issues {
			if iss.Number <= 0 {
				continue
			}
			extRef := fmt.Sprintf("gh#%d", iss.Number)
			existing, err := taskClient.ListTasks(domain.TaskFilter{
				Scope:       scope,
				RepoPath:    repoRoot,
				ExternalRef: extRef,
			})
			if err != nil {
				return err
			}
			desc := formatImportedIssueDescription(iss)
			title := truncateTitleForImport(iss.Title)

			if len(existing) > 0 {
				t := existing[0]
				updated, err := taskClient.UpdateTask(protocol.UpdateTaskPayload{
					ID:          t.ID,
					Title:       &title,
					Description: &desc,
					ExternalRef: &extRef,
					Author:      actor,
				})
				if err != nil {
					return err
				}
				imported = append(imported, updated)
				continue
			}

			created, err := taskClient.CreateTask(protocol.CreateTaskPayload{
				Title:       title,
				Description: desc,
				Status:      domain.StatusOpen,
				Priority:    domain.PriorityNormal,
				Author:      actor,
				Topic:       strings.TrimSpace(ghImportTopic),
				Kind:        domain.KindTask,
				ExternalRef: extRef,
			})
			if err != nil {
				return err
			}
			imported = append(imported, created)
		}

		if jsonFlag {
			return outputJSON(imported)
		}
		for _, t := range imported {
			fmt.Printf("✓ Imported %s ← %s (%s)\n", t.ID, t.ExternalRef, t.Title)
		}
		if len(imported) == 0 {
			fmt.Println("No GitHub issues found to import.")
		}
		return nil
	},
}

var ghPushCmd = &cobra.Command{
	Use:   "push [id]",
	Short: "Push a task to a GitHub Issue (creates a new issue or comments progress on an existing gh#<N> issue)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		id, err := resolveOrInferTaskID(args)
		if err != nil {
			return err
		}
		task, err := taskClient.GetTask(id)
		if err != nil {
			return err
		}

		actor := resolveActor("")
		if issueNum := parseGHIssueNumber(task.ExternalRef); issueNum != "" {
			commentBody := buildGHIssueSyncComment(task)
			if _, err := runGH("issue", "comment", issueNum, "--body", commentBody); err != nil {
				return err
			}
			if jsonFlag {
				return outputJSON(map[string]any{
					"id":           task.ID,
					"external_ref": task.ExternalRef,
					"action":       "commented",
				})
			}
			fmt.Printf("✓ Synced %s progress to GitHub issue #%s\n", task.ID, issueNum)
			return nil
		}

		title := fmt.Sprintf("[%s] %s", task.ID, task.Title)
		body := buildPRBodyForTask(task)
		out, err := runGH("issue", "create", "--title", title, "--body", body)
		if err != nil {
			return err
		}
		issueURL := extractLastURL(out)
		extRef := issueURL
		if m := ghURLNumberRegex.FindStringSubmatch(issueURL); len(m) >= 2 {
			extRef = "gh#" + m[1]
		}
		note := fmt.Sprintf("Pushed to GitHub Issue %s (%s)", extRef, issueURL)
		updated, err := taskClient.UpdateTask(protocol.UpdateTaskPayload{
			ID:          task.ID,
			ExternalRef: &extRef,
			AddNote:     &note,
			Author:      actor,
		})
		if err != nil {
			return err
		}
		if jsonFlag {
			return outputJSON(updated)
		}
		fmt.Printf("✓ Created GitHub Issue %s for %s (%s)\n", updated.ExternalRef, updated.ID, issueURL)
		return nil
	},
}

func buildPRBodyForTask(task *domain.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Task `%s`: %s\n\n", task.ID, task.Title)
	if task.TopicPath != "" {
		fmt.Fprintf(&b, "- **Topic**: `@%s`\n", task.TopicPath)
	}
	fmt.Fprintf(&b, "- **Priority**: `%s`\n", task.Priority)
	if task.Branch != "" {
		fmt.Fprintf(&b, "- **Branch**: `%s`\n", task.Branch)
	}
	if issueNum := parseGHIssueNumber(task.ExternalRef); issueNum != "" {
		fmt.Fprintf(&b, "- **External Issue**: Closes #%s\n", issueNum)
	} else if task.ExternalRef != "" {
		fmt.Fprintf(&b, "- **External Ref**: `%s`\n", task.ExternalRef)
	}
	if len(task.DependsOn) > 0 {
		fmt.Fprintf(&b, "- **Depends On**: %s\n", strings.Join(task.DependsOn, ", "))
	}
	if len(task.Blocks) > 0 {
		fmt.Fprintf(&b, "- **Unblocks**: %s\n", strings.Join(task.Blocks, ", "))
	}

	if strings.TrimSpace(task.Description) != "" {
		fmt.Fprintf(&b, "\n### Specification\n\n%s\n", strings.TrimSpace(task.Description))
	}

	if len(task.Commits) > 0 {
		fmt.Fprintf(&b, "\n### Attached Commits (%d)\n\n", len(task.Commits))
		for i := range task.Commits {
			c := &task.Commits[i]
			fmt.Fprintf(&b, "- `%s` %s\n", shortSHA(c.SHA), c.Subject)
		}
	}

	if len(task.Notes) > 0 {
		fmt.Fprintf(&b, "\n### Task Notes\n\n")
		for _, n := range task.Notes {
			fmt.Fprintf(&b, "- **%s**: %s\n", n.Author, strings.TrimSpace(n.Content))
		}
	}
	return b.String()
}

func buildGHIssueSyncComment(task *domain.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### `tk` Task Sync: `%s` (%s)\n\n", task.ID, task.StatusLabel())
	if task.Assignee != "" {
		fmt.Fprintf(&b, "- **Assignee**: `%s`\n", task.Assignee)
	}
	if task.Branch != "" {
		fmt.Fprintf(&b, "- **Branch**: `%s`\n", task.Branch)
	}
	if task.PRURL != "" {
		fmt.Fprintf(&b, "- **PR**: %s\n", task.PRURL)
	}
	if len(task.Commits) > 0 {
		fmt.Fprintf(&b, "\n**Attached Commits (%d)**:\n", len(task.Commits))
		for i := range task.Commits {
			c := &task.Commits[i]
			fmt.Fprintf(&b, "- `%s` %s\n", shortSHA(c.SHA), c.Subject)
		}
	}
	if len(task.Notes) > 0 {
		last := task.Notes[len(task.Notes)-1]
		fmt.Fprintf(&b, "\n**Latest Note (%s)**:\n> %s\n", last.Author, strings.TrimSpace(last.Content))
	}
	return b.String()
}

func parseGHIssueNumber(extRef string) string {
	m := ghIssueRefRegex.FindStringSubmatch(strings.TrimSpace(extRef))
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

func formatImportedIssueDescription(iss ghIssuePayload) string {
	var b strings.Builder
	if iss.URL != "" {
		fmt.Fprintf(&b, "Imported from GitHub Issue #%d (%s)", iss.Number, iss.URL)
		if iss.Author.Login != "" {
			fmt.Fprintf(&b, " by @%s", iss.Author.Login)
		}
		fmt.Fprintln(&b)
	}
	if strings.TrimSpace(iss.Body) != "" {
		if b.Len() > 0 {
			fmt.Fprintln(&b)
		}
		b.WriteString(strings.TrimSpace(iss.Body))
	}
	return b.String()
}

func truncateTitleForImport(title string) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	runes := []rune(clean)
	if len(runes) > domain.MaxTitleLength {
		return string(runes[:domain.MaxTitleLength-1]) + "…"
	}
	return clean
}

func extractLastURL(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
			return line
		}
	}
	return ""
}

func runGH(args ...string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("GitHub CLI ('gh') not found in PATH: install https://cli.github.com/ and run 'gh auth login'")
	}
	cmd := exec.Command("gh", args...)
	if repoRoot != "" {
		cmd.Dir = repoRoot
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh %s failed: %s (%w)", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

func init() {
	prCreateCmd.Flags().BoolVar(&prDraft, "draft", false, "Create the pull request as a draft")
	prCreateCmd.Flags().StringVar(&prBase, "base", "", "Base branch for the pull request")
	prCreateCmd.Flags().StringVar(&prHead, "head", "", "Head branch for the pull request (defaults to task's bound branch)")

	ghImportCmd.Flags().StringVar(&ghImportLabel, "label", "", "Filter GitHub issues by label")
	ghImportCmd.Flags().StringVarP(&ghImportTopic, "topic", "T", "", "Assign imported tasks to a topic")
	ghImportCmd.Flags().IntVar(&ghImportLimit, "limit", 30, "Maximum number of open issues to import")

	prCmd.AddCommand(prCreateCmd)
	prCmd.AddCommand(prStatusCmd)
	ghCmd.AddCommand(ghImportCmd)
	ghCmd.AddCommand(ghPushCmd)

	RootCmd.AddCommand(prCmd)
	RootCmd.AddCommand(ghCmd)
}
