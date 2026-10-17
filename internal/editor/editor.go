package editor

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"task_cli/internal/domain"
)

type ParsedNewTask struct {
	Title    string
	Priority domain.Priority
	// Topic is the destination topic path/slug ("" = caller default).
	Topic string
	// Tags is parsed from legacy "# Tags:" headers only; tags are deprecated
	// in favour of topics and new templates no longer emit the header.
	Tags        []string
	Assignee    string
	Reviewer    string
	Description string
}

type ParsedEditTask struct {
	Title    string
	Status   domain.Status
	Priority domain.Priority
	// Topic is the item's topic path after editing ("" or "unfiled" = no topic).
	Topic string
	// Tags mirrors the original tags unless a legacy "# Tags:" header is present.
	Tags          []string
	Assignee      string
	Reviewer      string
	Description   string
	NewNote       string
	NewReviewNote string
}

// EditChanges lists the fields an editor session actually modified relative to
// the snapshot it was opened with. Nil pointers mean "unchanged", so a save
// never overwrites concurrent updates to fields the user did not touch.
type EditChanges struct {
	Title         *string
	Description   *string
	Status        *domain.Status
	Priority      *domain.Priority
	Tags          *[]string
	Assignee      *string
	Reviewer      *string
	AddNote       *string
	AddReviewNote *string
	// Topic is the new topic ("unfiled" = clear), or "" when unchanged.
	Topic string
}

// Empty reports whether the session changed nothing at all.
func (c EditChanges) Empty() bool {
	return c.Title == nil && c.Description == nil && c.Status == nil && c.Priority == nil &&
		c.Tags == nil && c.Assignee == nil && c.Reviewer == nil && c.AddNote == nil && c.AddReviewNote == nil && c.Topic == ""
}

// Changes diffs p against orig, the task the edit template was generated from.
func (p *ParsedEditTask) Changes(orig *domain.Task) EditChanges {
	var c EditChanges
	if p.Title != orig.Title {
		c.Title = &p.Title
	}
	if p.Description != strings.TrimSpace(orig.Description) {
		c.Description = &p.Description
	}
	if p.Status != orig.Status {
		c.Status = &p.Status
	}
	if p.Priority != orig.Priority {
		c.Priority = &p.Priority
	}
	if !slices.Equal(p.Tags, orig.Tags) {
		c.Tags = &p.Tags
	}
	if p.Assignee != orig.Assignee {
		c.Assignee = &p.Assignee
	}
	if p.Reviewer != orig.Reviewer {
		c.Reviewer = &p.Reviewer
	}
	if p.NewNote != "" {
		c.AddNote = &p.NewNote
	}
	if p.NewReviewNote != "" {
		c.AddReviewNote = &p.NewReviewNote
	}
	if p.Topic != "" && p.Topic != editTopicValue(orig) {
		c.Topic = p.Topic
	}
	return c
}

// writeTempFile creates a temp file matching pattern, writes content to it and
// closes it. On any write or close failure the file is removed and the error returned.
func writeTempFile(pattern, content string) (string, error) {
	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}
	return tmpFile.Name(), nil
}

func CreateNewTaskFile(initialTitle string) (string, error) {
	return CreateNewTaskFileInTopic(initialTitle, "")
}

// CreateNewTaskFileInTopic writes a new-task template whose "# Topic:" header
// is pre-filled with topic (empty = leave unfiled / caller default).
func CreateNewTaskFileInTopic(initialTitle, topic string) (string, error) {
	template := fmt.Sprintf(`# Title: %s
# Priority: normal (low | normal | high | urgent)
# Topic: %s
# Assignee: 
# Reviewer: 

Write task description here...
`, initialTitle, topic)

	return writeTempFile("tk-new-*.md", template)
}

func CreateEditTaskFile(task *domain.Task) (string, error) {
	var sb strings.Builder
	if task.IsOrphaned() {
		sb.WriteString("# ⚠️  WARNING: THIS TASK IS ORPHANED / STALE\n")
		sb.WriteString("# Inactivity has exceeded threshold or assignee abandoned work.\n")
		sb.WriteString("# Run 'tk takeover " + task.ID + "' to claim, or update assignee/status below.\n#\n")
	}
	fmt.Fprintf(&sb, "# Task: %s\n", task.ID)
	fmt.Fprintf(&sb, "# Title: %s\n", task.Title)
	if task.IsOrphaned() {
		fmt.Fprintf(&sb, "# Status: %s [ORPHANED] (open | in_progress | done | closed)\n", task.Status)
	} else {
		fmt.Fprintf(&sb, "# Status: %s (open | in_progress | done | closed)\n", task.Status)
	}
	fmt.Fprintf(&sb, "# Priority: %s (low | normal | high | urgent)\n", task.Priority)
	fmt.Fprintf(&sb, "# Topic: %s\n", editTopicValue(task))
	if task.IsOrphaned() && task.Assignee != "" {
		fmt.Fprintf(&sb, "# Assignee: %s [STALE / TIMED OUT]\n", task.Assignee)
	} else {
		fmt.Fprintf(&sb, "# Assignee: %s\n", task.Assignee)
	}
	fmt.Fprintf(&sb, "# Reviewer: %s\n\n", task.Reviewer)
	sb.WriteString(task.Description)
	sb.WriteString("\n\n# --- Add new notes/comments below (optional) ---\n\n")
	sb.WriteString("# --- Add new reviewer notes below (optional) ---\n")

	return writeTempFile(fmt.Sprintf("tk-edit-%s-*.md", task.ID), sb.String())
}

// editTopicValue is the "# Topic:" header value for an existing item.
func editTopicValue(task *domain.Task) string {
	if task.TopicPath == "" {
		return domain.UnfiledTopic
	}
	return task.TopicPath
}

func BuildEditorCmd(editorCmd, filePath string) *exec.Cmd {
	parts := strings.Fields(editorCmd)
	if len(parts) == 0 {
		parts = []string{"nvim"}
	}
	args := make([]string, 0, len(parts))
	args = append(args, parts[1:]...)
	args = append(args, filePath)
	return exec.Command(parts[0], args...)
}

func OpenNewTask(editorCmd, initialTitle string) (*ParsedNewTask, error) {
	tmpPath, err := CreateNewTaskFile(initialTitle)
	if err != nil {
		return nil, err
	}

	cmd := BuildEditorCmd(editorCmd, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	contentBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	parsed, err := ParseNewTaskContent(string(contentBytes))
	if err != nil {
		return nil, fmt.Errorf("%w (draft preserved at: %s)", err, tmpPath)
	}

	_ = os.Remove(tmpPath)
	return parsed, nil
}

func OpenEditTask(editorCmd string, task *domain.Task) (*ParsedEditTask, error) {
	tmpPath, err := CreateEditTaskFile(task)
	if err != nil {
		return nil, err
	}

	cmd := BuildEditorCmd(editorCmd, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	contentBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	parsed, err := ParseEditTaskContent(string(contentBytes), task)
	if err != nil {
		return nil, fmt.Errorf("%w (draft preserved at: %s)", err, tmpPath)
	}

	_ = os.Remove(tmpPath)
	return parsed, nil
}

func parseHeaderKV(trimmed string) (key, val string, ok bool) {
	if !strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	colonIdx := strings.Index(trimmed, ":")
	if colonIdx == -1 {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(trimmed[:colonIdx]))
	val = strings.TrimSpace(trimmed[colonIdx+1:])
	return key, val, true
}

func ParseNewTaskContent(content string) (*ParsedNewTask, error) {
	lines := strings.Split(content, "\n")
	res := &ParsedNewTask{
		Priority: domain.PriorityNormal,
	}

	var descLines []string
	inHeader := true

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inHeader && strings.HasPrefix(trimmed, "#") {
			if key, val, ok := parseHeaderKV(trimmed); ok {
				switch key {
				case "# title":
					res.Title = val
				case "# priority":
					if idx := strings.Index(val, "("); idx != -1 {
						val = strings.TrimSpace(val[:idx])
					}
					if val != "" {
						p, err := domain.ParsePriority(val)
						if err != nil {
							return nil, err
						}
						res.Priority = p
					}
				case "# topic":
					res.Topic = val
				case "# tags":
					if val != "" {
						for _, p := range strings.Split(val, ",") {
							p = strings.TrimSpace(p)
							if p != "" {
								res.Tags = append(res.Tags, p)
							}
						}
					}
				case "# assignee":
					res.Assignee = val
				case "# reviewer":
					res.Reviewer = val
				}
				continue
			}
			// Keyless comment lines (banners, bare "#") belong to the header template.
			continue
		}

		inHeader = false
		descLines = append(descLines, line)
	}

	res.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
	if res.Description == "Write task description here..." {
		res.Description = ""
	}

	if res.Title == "" {
		return nil, fmt.Errorf("task creation aborted: title cannot be empty")
	}
	title, err := domain.NormalizeTitle(res.Title)
	if err != nil {
		return nil, err
	}
	res.Title = title

	return res, nil
}

func ParseEditTaskContent(content string, orig *domain.Task) (*ParsedEditTask, error) {
	lines := strings.Split(content, "\n")
	res := &ParsedEditTask{
		Title:    orig.Title,
		Status:   orig.Status,
		Priority: orig.Priority,
		Topic:    editTopicValue(orig),
		Tags:     orig.Tags,
		Assignee: orig.Assignee,
		Reviewer: orig.Reviewer,
	}

	var bodyLines []string
	var noteLines []string
	var reviewNoteLines []string
	inHeader := true
	inNotes := false
	inReviewNotes := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "# --- Add new reviewer notes below") {
			inReviewNotes = true
			inNotes = false
			continue
		}
		if strings.HasPrefix(trimmed, "# --- Add new notes/comments below") {
			inNotes = true
			inReviewNotes = false
			continue
		}

		if inReviewNotes {
			reviewNoteLines = append(reviewNoteLines, line)
			continue
		}
		if inNotes {
			noteLines = append(noteLines, line)
			continue
		}

		if inHeader && strings.HasPrefix(trimmed, "#") {
			if key, val, ok := parseHeaderKV(trimmed); ok {
				switch key {
				case "# title":
					res.Title = val
				case "# status":
					if idx := strings.Index(val, "("); idx != -1 {
						val = strings.TrimSpace(val[:idx])
					}
					if idx := strings.Index(val, "["); idx != -1 {
						val = strings.TrimSpace(val[:idx])
					}
					if val != "" {
						s, err := domain.ParseStatus(val)
						if err != nil {
							return nil, err
						}
						res.Status = s
					}
				case "# priority":
					if idx := strings.Index(val, "("); idx != -1 {
						val = strings.TrimSpace(val[:idx])
					}
					if idx := strings.Index(val, "["); idx != -1 {
						val = strings.TrimSpace(val[:idx])
					}
					if val != "" {
						p, err := domain.ParsePriority(val)
						if err != nil {
							return nil, err
						}
						res.Priority = p
					}
				case "# topic":
					if val == "" {
						val = domain.UnfiledTopic
					}
					res.Topic = val
				case "# tags":
					var tags []string
					if val != "" {
						for _, p := range strings.Split(val, ",") {
							p = strings.TrimSpace(p)
							if p != "" {
								tags = append(tags, p)
							}
						}
					}
					res.Tags = tags
				case "# assignee":
					if idx := strings.Index(val, "["); idx != -1 {
						val = strings.TrimSpace(val[:idx])
					}
					res.Assignee = val
				case "# reviewer":
					if idx := strings.Index(val, "["); idx != -1 {
						val = strings.TrimSpace(val[:idx])
					}
					res.Reviewer = val
				}
				continue
			}
			// Keyless comment lines (banners, bare "#") belong to the header template.
			continue
		}

		inHeader = false
		bodyLines = append(bodyLines, line)
	}

	res.Description = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	res.NewNote = strings.TrimSpace(strings.Join(noteLines, "\n"))
	res.NewReviewNote = strings.TrimSpace(strings.Join(reviewNoteLines, "\n"))

	if res.Title != orig.Title {
		title, err := domain.NormalizeTitle(res.Title)
		if err != nil {
			return nil, err
		}
		res.Title = title
	}

	return res, nil
}
