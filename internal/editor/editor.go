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
	sb.WriteString(fmt.Sprintf("# Task: %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("# Title: %s\n", task.Title))
	sb.WriteString(fmt.Sprintf("# Status: %s (open | in_progress | done | closed)\n", task.Status))
	sb.WriteString(fmt.Sprintf("# Priority: %s (low | normal | high | urgent)\n", task.Priority))
	sb.WriteString(fmt.Sprintf("# Tags: %s\n", strings.Join(task.Tags, ", ")))
	sb.WriteString(fmt.Sprintf("# Assignee: %s\n\n", task.Assignee))
	sb.WriteString(task.Description)
	sb.WriteString("\n\n# --- Add new notes/comments below (optional) ---\n")

	if _, err := tmpFile.WriteString(sb.String()); err != nil {
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
	defer os.Remove(tmpPath)

	cmd := BuildEditorCmd(editorCmd, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	contentBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, err
	}

	return ParseNewTaskContent(string(contentBytes))
}

func OpenEditTask(editorCmd string, task *domain.Task) (*ParsedEditTask, error) {
	tmpPath, err := CreateEditTaskFile(task)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpPath)

	cmd := BuildEditorCmd(editorCmd, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	contentBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, err
	}

	return ParseEditTaskContent(string(contentBytes), task)
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
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "# title:") {
				res.Title = strings.TrimSpace(line[len("# title:"):])
			} else if strings.HasPrefix(lower, "# priority:") {
				val := strings.TrimSpace(line[len("# priority:"):])
				if idx := strings.Index(val, "("); idx != -1 {
					val = strings.TrimSpace(val[:idx])
				}
				if p, err := domain.ParsePriority(val); err == nil {
					res.Priority = p
				}
			} else if strings.HasPrefix(lower, "# tags:") {
				tagStr := strings.TrimSpace(line[len("# tags:"):])
				if tagStr != "" {
					parts := strings.Split(tagStr, ",")
					for _, p := range parts {
						p = strings.TrimSpace(p)
						if p != "" {
							res.Tags = append(res.Tags, p)
						}
					}
				}
			} else if strings.HasPrefix(lower, "# assignee:") {
				res.Assignee = strings.TrimSpace(line[len("# assignee:"):])
			}
			continue
		}

		inHeader = false
		descLines = append(descLines, line)
	}

	res.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
	if res.Title == "" {
		return nil, fmt.Errorf("task creation aborted: title cannot be empty")
	}

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
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "# title:") {
				res.Title = strings.TrimSpace(line[len("# title:"):])
			} else if strings.HasPrefix(lower, "# status:") {
				val := strings.TrimSpace(line[len("# status:"):])
				if idx := strings.Index(val, "("); idx != -1 {
					val = strings.TrimSpace(val[:idx])
				}
				if s, err := domain.ParseStatus(val); err == nil {
					res.Status = s
				}
			} else if strings.HasPrefix(lower, "# priority:") {
				val := strings.TrimSpace(line[len("# priority:"):])
				if idx := strings.Index(val, "("); idx != -1 {
					val = strings.TrimSpace(val[:idx])
				}
				if p, err := domain.ParsePriority(val); err == nil {
					res.Priority = p
				}
			} else if strings.HasPrefix(lower, "# tags:") {
				tagStr := strings.TrimSpace(line[len("# tags:"):])
				var tags []string
				if tagStr != "" {
					parts := strings.Split(tagStr, ",")
					for _, p := range parts {
						p = strings.TrimSpace(p)
						if p != "" {
							tags = append(tags, p)
						}
					}
				}
				res.Tags = tags
			} else if strings.HasPrefix(lower, "# assignee:") {
				res.Assignee = strings.TrimSpace(line[len("# assignee:"):])
			}
			continue
		}

		inHeader = false
		bodyLines = append(bodyLines, line)
	}

	res.Description = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	res.NewNote = strings.TrimSpace(strings.Join(noteLines, "\n"))

	return res, nil
}
