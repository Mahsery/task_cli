package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"task_cli/internal/domain"
	"task_cli/internal/search"

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
		repoPath = "" // Normalize repoPath for global scope
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Monotonically allocate sequence number from persistent task_sequences table
	var nextSeq int64
	seqRow := tx.QueryRow(`
		INSERT INTO task_sequences (scope, repo_path, last_seq)
		VALUES (?, ?, 1)
		ON CONFLICT(scope, repo_path) DO UPDATE SET last_seq = task_sequences.last_seq + 1
		RETURNING last_seq;
	`, scope, repoPath)
	if err := seqRow.Scan(&nextSeq); err != nil {
		return nil, fmt.Errorf("failed to allocate task sequence number: %w", err)
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

	_, err = tx.Exec(insertSQL,
		taskID, scope, repoPath, nextSeq, title, description, string(status), string(priority), string(tagsJSON), author, assignee, now, now, closedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert task: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit task insert: %w", err)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate task notes: %w", err)
	}

	return &task, nil
}

func (d *DB) UpdateTask(id string, title, description *string, status *domain.Status, priority *domain.Priority, tags *[]string, addTags []string, removeTags []string, assignee *string, addNote *string, author string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Verify task exists and load current state in transaction
	var currentTagsJSON string
	err = tx.QueryRow("SELECT tags FROM tasks WHERE id = ?", id).Scan(&currentTagsJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	now := time.Now().UTC()
	updates := []string{"updated_at = ?"}
	args := []any{now}

	if title != nil {
		updates = append(updates, "title = ?")
		args = append(args, *title)
	}
	if description != nil {
		updates = append(updates, "description = ?")
		args = append(args, *description)
	}
	if status != nil {
		updates = append(updates, "status = ?")
		args = append(args, string(*status))

		if *status == domain.StatusDone || *status == domain.StatusClosed {
			updates = append(updates, "closed_at = ?")
			args = append(args, now)
		} else {
			updates = append(updates, "closed_at = NULL")
		}
	}
	if priority != nil {
		updates = append(updates, "priority = ?")
		args = append(args, string(*priority))
	}

	// Atomic tag mutations inside the transaction
	if tags != nil || len(addTags) > 0 || len(removeTags) > 0 {
		var currentTags []string
		if currentTagsJSON != "" && currentTagsJSON != "[]" {
			_ = json.Unmarshal([]byte(currentTagsJSON), &currentTags)
		}

		if tags != nil {
			currentTags = *tags
		}
		if len(addTags) > 0 {
			tagSet := make(map[string]bool)
			for _, t := range currentTags {
				tagSet[t] = true
			}
			for _, t := range addTags {
				t = strings.TrimSpace(t)
				if t != "" && !tagSet[t] {
					tagSet[t] = true
					currentTags = append(currentTags, t)
				}
			}
		}
		if len(removeTags) > 0 {
			removeSet := make(map[string]bool)
			for _, t := range removeTags {
				removeSet[strings.TrimSpace(t)] = true
			}
			var filtered []string
			for _, t := range currentTags {
				if !removeSet[t] {
					filtered = append(filtered, t)
				}
			}
			currentTags = filtered
		}
		if currentTags == nil {
			currentTags = []string{}
		}
		newTagsBytes, err := json.Marshal(currentTags)
		if err != nil {
			return nil, err
		}
		updates = append(updates, "tags = ?")
		args = append(args, string(newTagsBytes))
	}

	if assignee != nil {
		updates = append(updates, "assignee = ?")
		args = append(args, *assignee)
	}

	args = append(args, id)
	updateSQL := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(updates, ", "))

	if _, err := tx.Exec(updateSQL, args...); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	if addNote != nil && strings.TrimSpace(*addNote) != "" {
		noteID := uuid.New().String()
		noteSQL := `INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`
		if _, err := tx.Exec(noteSQL, noteID, id, author, strings.TrimSpace(*addNote), now); err != nil {
			return nil, fmt.Errorf("failed to insert note: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit update: %w", err)
	}

	return d.GetTask(id)
}

func (d *DB) ClaimTask(id string, assignee string, force bool) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	assignee = strings.TrimSpace(assignee)
	if assignee == "" {
		return nil, fmt.Errorf("claim assignee cannot be empty")
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var currentAssignee, currentStatus string
	query := "SELECT assignee, status FROM tasks WHERE id = ?"
	if err := tx.QueryRow(query, id).Scan(&currentAssignee, &currentStatus); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	// Conditional atomic claim: competing claim fails unless force is true
	if currentAssignee != "" && currentAssignee != assignee && !force {
		return nil, fmt.Errorf("task %s is already claimed by %q (use --force to overwrite)", id, currentAssignee)
	}

	now := time.Now().UTC()
	newStatus := currentStatus
	if currentStatus == string(domain.StatusOpen) {
		newStatus = string(domain.StatusInProgress)
	}

	updateSQL := "UPDATE tasks SET assignee = ?, status = ?, updated_at = ? WHERE id = ?"
	if _, err := tx.Exec(updateSQL, assignee, newStatus, now, id); err != nil {
		return nil, fmt.Errorf("failed to claim task: %w", err)
	}

	if currentAssignee != assignee {
		noteID := uuid.New().String()
		noteContent := fmt.Sprintf("Claimed task as %q", assignee)
		noteSQL := `INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`
		if _, err := tx.Exec(noteSQL, noteID, id, assignee, noteContent, now); err != nil {
			return nil, fmt.Errorf("failed to insert claim note: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit claim: %w", err)
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
		conditions = append(conditions, "EXISTS (SELECT 1 FROM json_each(tasks.tags) WHERE value = ?)")
		args = append(args, filter.Tag)
	}
	if filter.Assignee != "" {
		conditions = append(conditions, "assignee = ?")
		args = append(args, filter.Assignee)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limitClause := ""
	if filter.Limit > 0 && filter.Search == "" {
		limitClause = fmt.Sprintf("LIMIT %d", filter.Limit)
	}

	orderClause := `ORDER BY 
		CASE status
			WHEN 'in_progress' THEN 1
			WHEN 'open' THEN 2
			WHEN 'done' THEN 3
			WHEN 'closed' THEN 4
			ELSE 5
		END ASC,
		CASE priority
			WHEN 'urgent' THEN 1
			WHEN 'high' THEN 2
			WHEN 'normal' THEN 3
			WHEN 'low' THEN 4
			ELSE 5
		END ASC,
		seq_num ASC`

	switch strings.ToLower(strings.TrimSpace(filter.Sort)) {
	case "id", "id_asc":
		orderClause = "ORDER BY seq_num ASC"
	case "id_desc", "recent", "seq_num":
		orderClause = "ORDER BY seq_num DESC"
	case "priority":
		orderClause = `ORDER BY 
			CASE priority
				WHEN 'urgent' THEN 1
				WHEN 'high' THEN 2
				WHEN 'normal' THEN 3
				WHEN 'low' THEN 4
				ELSE 5
			END ASC, seq_num ASC`
	case "created", "created_at":
		orderClause = "ORDER BY created_at DESC"
	case "updated", "updated_at":
		orderClause = "ORDER BY updated_at DESC"
	}

	query := fmt.Sprintf(`
	SELECT id, scope, repo_path, title, description, status, priority, tags, author, assignee,
	       created_at, updated_at, closed_at, commit_sha, commit_msg, diff_stat, diff_content
	FROM tasks %s
	%s %s
	`, whereClause, orderClause, limitClause)

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
		if tagsJSON == "[]" || tagsJSON == "" {
			task.Tags = []string{}
		} else if err := json.Unmarshal([]byte(tagsJSON), &task.Tags); err != nil {
			task.Tags = []string{}
		}
		task.Notes = []domain.TaskNote{}
		tasks = append(tasks, &task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tasks: %w", err)
	}

	// Batch load notes for listed tasks
	if len(tasks) > 0 {
		taskMap := make(map[string]*domain.Task, len(tasks))
		var taskIDs []any
		var placeholders []string
		for _, t := range tasks {
			taskMap[t.ID] = t
			taskIDs = append(taskIDs, t.ID)
			placeholders = append(placeholders, "?")
		}

		notesQuery := fmt.Sprintf("SELECT id, task_id, author, content, created_at FROM task_notes WHERE task_id IN (%s) ORDER BY created_at ASC", strings.Join(placeholders, ","))
		noteRows, err := d.conn.Query(notesQuery, taskIDs...)
		if err == nil {
			defer noteRows.Close()
			for noteRows.Next() {
				var note domain.TaskNote
				var tID string
				if err := noteRows.Scan(&note.ID, &tID, &note.Author, &note.Content, &note.CreatedAt); err == nil {
					if t, ok := taskMap[tID]; ok {
						t.Notes = append(t.Notes, note)
					}
				}
			}
		}
	}

	// Evaluate search DSL filter across all fields including tags and notes
	if filter.Search != "" {
		compiled := search.Compile(filter.Search)
		var matched []*domain.Task
		for _, task := range tasks {
			if compiled.Match(task) {
				matched = append(matched, task)
			}
		}
		tasks = matched
		if filter.Limit > 0 && len(tasks) > filter.Limit {
			tasks = tasks[:filter.Limit]
		}
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
