package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"task_cli/internal/domain"

	"github.com/google/uuid"
)

func (d *DB) CreateTask(scope, repoPath, title, description string, status domain.Status, priority domain.Priority, tags []string, author, assignee string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tags: %w", err)
	}

	prefix := "T"
	if scope == "global" {
		prefix = "G"
	}

	// Compute next sequential number for this scope & repo
	var maxSeq sql.NullInt64
	query := "SELECT MAX(seq_num) FROM tasks WHERE scope = ? AND repo_path = ?"
	if err := d.conn.QueryRow(query, scope, repoPath).Scan(&maxSeq); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get max seq_num: %w", err)
	}

	nextSeq := int64(1)
	if maxSeq.Valid {
		nextSeq = maxSeq.Int64 + 1
	}

	taskID := fmt.Sprintf("%s-%d", prefix, nextSeq)
	now := time.Now().UTC()

	var closedAt *time.Time
	if status == domain.StatusDone || status == domain.StatusClosed {
		closedAt = &now
	}

	insertSQL := `
	INSERT INTO tasks (
		id, scope, repo_path, seq_num, title, description, status, priority, tags, author, assignee, created_at, updated_at, closed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = d.conn.Exec(insertSQL,
		taskID, scope, repoPath, nextSeq, title, description, string(status), string(priority), string(tagsJSON), author, assignee, now, now, closedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert task: %w", err)
	}

	return &domain.Task{
		ID:          taskID,
		Scope:       scope,
		RepoPath:    repoPath,
		Title:       title,
		Description: description,
		Status:      status,
		Priority:    priority,
		Tags:        tags,
		Author:      author,
		Assignee:    assignee,
		CreatedAt:   now,
		UpdatedAt:   now,
		ClosedAt:    closedAt,
		Notes:       []domain.TaskNote{},
	}, nil
}

func (d *DB) GetTask(id string) (*domain.Task, error) {
	query := `
	SELECT id, scope, repo_path, title, description, status, priority, tags, author, assignee,
	       created_at, updated_at, closed_at, commit_sha, commit_msg, diff_stat, diff_content
	FROM tasks WHERE id = ?
	`

	row := d.conn.QueryRow(query, id)

	var task domain.Task
	var statusStr, priorityStr, tagsJSON string
	var closedAt sql.NullTime

	err := row.Scan(
		&task.ID, &task.Scope, &task.RepoPath, &task.Title, &task.Description,
		&statusStr, &priorityStr, &tagsJSON, &task.Author, &task.Assignee,
		&task.CreatedAt, &task.UpdatedAt, &closedAt,
		&task.CommitSHA, &task.CommitMsg, &task.DiffStat, &task.DiffContent,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	task.Status = domain.Status(statusStr)
	task.Priority = domain.Priority(priorityStr)
	if closedAt.Valid {
		task.ClosedAt = &closedAt.Time
	}

	if err := json.Unmarshal([]byte(tagsJSON), &task.Tags); err != nil {
		task.Tags = []string{}
	}

	// Fetch notes
	notesQuery := `SELECT id, author, content, created_at FROM task_notes WHERE task_id = ? ORDER BY created_at ASC`
	rows, err := d.conn.Query(notesQuery, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query task notes: %w", err)
	}
	defer rows.Close()

	task.Notes = []domain.TaskNote{}
	for rows.Next() {
		var note domain.TaskNote
		if err := rows.Scan(&note.ID, &note.Author, &note.Content, &note.CreatedAt); err != nil {
			return nil, err
		}
		task.Notes = append(task.Notes, note)
	}

	return &task, nil
}

func (d *DB) UpdateTask(id string, title, description *string, status *domain.Status, priority *domain.Priority, tags *[]string, assignee *string, addNote *string, author string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	task, err := d.GetTask(id)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	updates := []string{"updated_at = ?"}
	args := []any{now}

	if title != nil {
		task.Title = *title
		updates = append(updates, "title = ?")
		args = append(args, *title)
	}
	if description != nil {
		task.Description = *description
		updates = append(updates, "description = ?")
		args = append(args, *description)
	}
	if status != nil {
		task.Status = *status
		updates = append(updates, "status = ?")
		args = append(args, string(*status))

		if *status == domain.StatusDone || *status == domain.StatusClosed {
			task.ClosedAt = &now
			updates = append(updates, "closed_at = ?")
			args = append(args, now)
		} else {
			task.ClosedAt = nil
			updates = append(updates, "closed_at = NULL")
		}
	}
	if priority != nil {
		task.Priority = *priority
		updates = append(updates, "priority = ?")
		args = append(args, string(*priority))
	}
	if tags != nil {
		task.Tags = *tags
		tagsJSON, err := json.Marshal(*tags)
		if err != nil {
			return nil, err
		}
		updates = append(updates, "tags = ?")
		args = append(args, string(tagsJSON))
	}
	if assignee != nil {
		task.Assignee = *assignee
		updates = append(updates, "assignee = ?")
		args = append(args, *assignee)
	}

	args = append(args, id)
	updateSQL := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(updates, ", "))
	if _, err := d.conn.Exec(updateSQL, args...); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	if addNote != nil && strings.TrimSpace(*addNote) != "" {
		noteID := uuid.New().String()
		noteSQL := `INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`
		if _, err := d.conn.Exec(noteSQL, noteID, id, author, strings.TrimSpace(*addNote), now); err != nil {
			return nil, fmt.Errorf("failed to insert note: %w", err)
		}
	}

	return d.GetTask(id)
}

func (d *DB) ListTasks(filter domain.TaskFilter) ([]*domain.Task, error) {
	var conditions []string
	var args []any

	if filter.Scope != "" {
		conditions = append(conditions, "scope = ?")
		args = append(args, filter.Scope)
	}
	if filter.Scope == "local" && filter.RepoPath != "" {
		conditions = append(conditions, "repo_path = ?")
		args = append(args, filter.RepoPath)
	}
	if filter.Status != nil {
		conditions = append(conditions, "status = ?")
		args = append(args, string(*filter.Status))
	}
	if filter.Priority != nil {
		conditions = append(conditions, "priority = ?")
		args = append(args, string(*filter.Priority))
	}
	if filter.Tag != "" {
		conditions = append(conditions, "tags LIKE ?")
		args = append(args, "%\""+filter.Tag+"\"%")
	}
	if filter.Search != "" {
		conditions = append(conditions, "(title LIKE ? OR description LIKE ?)")
		s := "%" + filter.Search + "%"
		args = append(args, s, s)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limitClause := ""
	if filter.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", filter.Limit)
	}

	query := fmt.Sprintf(`
	SELECT id, scope, repo_path, title, description, status, priority, tags, author, assignee,
	       created_at, updated_at, closed_at, commit_sha, commit_msg, diff_stat, diff_content
	FROM tasks %s
	ORDER BY seq_num DESC %s
	`, whereClause, limitClause)

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*domain.Task
	for rows.Next() {
		var task domain.Task
		var statusStr, priorityStr, tagsJSON string
		var closedAt sql.NullTime

		err := rows.Scan(
			&task.ID, &task.Scope, &task.RepoPath, &task.Title, &task.Description,
			&statusStr, &priorityStr, &tagsJSON, &task.Author, &task.Assignee,
			&task.CreatedAt, &task.UpdatedAt, &closedAt,
			&task.CommitSHA, &task.CommitMsg, &task.DiffStat, &task.DiffContent,
		)
		if err != nil {
			return nil, err
		}

		task.Status = domain.Status(statusStr)
		task.Priority = domain.Priority(priorityStr)
		if closedAt.Valid {
			task.ClosedAt = &closedAt.Time
		}
		if err := json.Unmarshal([]byte(tagsJSON), &task.Tags); err != nil {
			task.Tags = []string{}
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

func (d *DB) DeleteTask(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.conn.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

func (d *DB) AttachGit(id, commitSHA, commitMsg, diffStat, diffContent string, markDone bool) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().UTC()
	updates := []string{
		"commit_sha = ?",
		"commit_msg = ?",
		"diff_stat = ?",
		"diff_content = ?",
		"updated_at = ?",
	}
	args := []any{commitSHA, commitMsg, diffStat, diffContent, now}

	if markDone {
		updates = append(updates, "status = ?", "closed_at = ?")
		args = append(args, string(domain.StatusDone), now)
	}

	args = append(args, id)
	updateSQL := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(updates, ", "))
	if _, err := d.conn.Exec(updateSQL, args...); err != nil {
		return nil, fmt.Errorf("failed to attach git info: %w", err)
	}

	return d.GetTask(id)
}
