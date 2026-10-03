package editor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"task_cli/internal/domain"
)

type ParsedNewTask struct {
	Title       string
	Priority    domain.Priority
	Tags        []string
	Assignee    string
	Description string
}

type ParsedEditTask struct {
	Title       string
	Status      domain.Status
	Priority    domain.Priority
	Tags        []string
	Assignee    string
	Description string
	NewNote     string
}

func CreateNewTaskFile(initialTitle string) (string, error) {
	tmpFile, err := os.CreateTemp("", "tk-new-*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	template := fmt.Sprintf(`# Title: %s
# Priority: normal (low | normal | high | urgent)
# Tags: 
# Assignee: 

Write task description here...
`, initialTitle)

	if _, err := tmpFile.WriteString(template); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

func CreateEditTaskFile(task *domain.Task) (string, error) {
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("tk-edit-%s-*.md", task.ID))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	var sb strings.Builder
	if task.IsOrphaned() {
		sb.WriteString("# ⚠️  WARNING: THIS TASK IS ORPHANED / STALE\n")
		sb.WriteString("# Inactivity has exceeded threshold or assignee abandoned work.\n")
		sb.WriteString("# Run 'tk takeover " + task.ID + "' to claim, or update assignee/status below.\n#\n")
	}
	sb.WriteString(fmt.Sprintf("# Task: %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("# Title: %s\n", task.Title))
	if task.IsOrphaned() {
		sb.WriteString(fmt.Sprintf("# Status: %s [ORPHANED] (open | in_progress | done | closed)\n", task.Status))
	} else {
		sb.WriteString(fmt.Sprintf("# Status: %s (open | in_progress | done | closed)\n", task.Status))
	}
	sb.WriteString(fmt.Sprintf("# Priority: %s (low | normal | high | urgent)\n", task.Priority))
	sb.WriteString(fmt.Sprintf("# Tags: %s\n", strings.Join(task.Tags, ", ")))
	if task.IsOrphaned() && task.Assignee != "" {
		sb.WriteString(fmt.Sprintf("# Assignee: %s [STALE / TIMED OUT]\n\n", task.Assignee))
	} else {
		sb.WriteString(fmt.Sprintf("# Assignee: %s\n\n", task.Assignee))
	}
	sb.WriteString(task.Description)
	sb.WriteString("\n\n# --- Add new notes/comments below (optional) ---\n")

	if _, err := tmpFile.WriteString(sb.String()); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

func BuildEditorCmd(editorCmd, filePath string) *exec.Cmd {
	parts := strings.Fields(editorCmd)
	if len(parts) == 0 {
		parts = []string{"nvim"}
	}
	args := append(parts[1:], filePath)
	return exec.Command(parts[0], args...)
}

func OpenNewTask(editorCmd string, initialTitle string) (*ParsedNewTask, error) {
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
