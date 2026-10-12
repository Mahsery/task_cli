package editor

import (
	"fmt"
	"os"
	"os/exec"
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
	Description string
}

type ParsedEditTask struct {
	Title    string
	Status   domain.Status
	Priority domain.Priority
	// Topic is the item's topic path after editing ("" or "unfiled" = no topic).
	Topic string
	// Tags mirrors the original tags unless a legacy "# Tags:" header is present.
	Tags        []string
	Assignee    string
	Description string
	NewNote     string
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
		fmt.Fprintf(&sb, "# Assignee: %s [STALE / TIMED OUT]\n\n", task.Assignee)
	} else {
		fmt.Fprintf(&sb, "# Assignee: %s\n\n", task.Assignee)
	}
	sb.WriteString(task.Description)
	sb.WriteString("\n\n# --- Add new notes/comments below (optional) ---\n")

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
				}
				continue
			}
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
	}

	var bodyLines []string
	var noteLines []string
	inHeader := true
	inNotes := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "# --- Add new notes/comments below") {
			inNotes = true
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
				}
				continue
			}
		}

		inHeader = false
		bodyLines = append(bodyLines, line)
	}

	res.Description = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	res.NewNote = strings.TrimSpace(strings.Join(noteLines, "\n"))

	if res.Title != orig.Title {
		title, err := domain.NormalizeTitle(res.Title)
		if err != nil {
			return nil, err
		}
		res.Title = title
	}

	return res, nil
}
