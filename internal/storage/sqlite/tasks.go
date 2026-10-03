package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"task_cli/internal/domain"
	"task_cli/internal/search"

	"github.com/google/uuid"
)

func (d *DB) CreateTask(scope, repoPath, title, description string, status domain.Status, priority domain.Priority, tags []string, author, assignee string, dependsOn ...string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	normalized, err := domain.NormalizeTitle(title)
	if err != nil {
		return nil, err
	}
	title = normalized

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
	defer func() { _ = tx.Rollback() }()

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

	var closedAt, archivedAt *time.Time
	if status == domain.StatusDone || status == domain.StatusClosed {
		closedAt = &now
	}
	if status == domain.StatusArchived {
		archivedAt = &now
	}

	insertSQL := `
	INSERT INTO tasks (
		id, scope, repo_path, seq_num, title, description, status, priority, tags, author, assignee, created_at, updated_at, closed_at, archived_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = tx.Exec(insertSQL,
		taskID, scope, repoPath, nextSeq, title, description, string(status), string(priority), string(tagsJSON), author, assignee, now, now, closedAt, archivedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert task: %w", err)
	}

	for _, depID := range dependsOn {
		depID = strings.TrimSpace(depID)
		if depID == "" {
			continue
		}
		if depID == taskID {
			return nil, fmt.Errorf("task %s cannot depend on itself", taskID)
		}
		var exists int
		if err := tx.QueryRow("SELECT 1 FROM tasks WHERE id = ?", depID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("prerequisite task %s not found", depID)
		}
		_, err = tx.Exec("INSERT OR IGNORE INTO task_dependencies (task_id, depends_on_id, created_at) VALUES (?, ?, ?)", taskID, depID, now)
		if err != nil {
			return nil, fmt.Errorf("failed to link dependency %s -> %s: %w", taskID, depID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit task insert: %w", err)
	}

	return d.getTaskLocked(d.conn, taskID)
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(s rowScanner, task *domain.Task) error {
	var statusStr, priorityStr, tagsJSON string
	var closedAt, archivedAt sql.NullTime

	err := s.Scan(
		&task.ID, &task.Scope, &task.RepoPath, &task.Title, &task.Description,
		&statusStr, &priorityStr, &tagsJSON, &task.Author, &task.Assignee,
		&task.CreatedAt, &task.UpdatedAt, &closedAt, &archivedAt,
		&task.CommitSHA, &task.CommitMsg, &task.DiffStat, &task.DiffContent,
	)
	if err != nil {
		return err
	}

	task.Status = domain.Status(statusStr)
	task.Priority = domain.Priority(priorityStr)
	if closedAt.Valid {
		task.ClosedAt = &closedAt.Time
	}
	if archivedAt.Valid {
		task.ArchivedAt = &archivedAt.Time
	}

	if tagsJSON == "[]" || tagsJSON == "" {
		task.Tags = []string{}
	} else if err := json.Unmarshal([]byte(tagsJSON), &task.Tags); err != nil {
		task.Tags = []string{}
	}
	return nil
}

func (d *DB) getTaskLocked(q queryRower, id string) (*domain.Task, error) {
	query := `
	SELECT id, scope, repo_path, title, description, status, priority, tags, author, assignee,
	       created_at, updated_at, closed_at, archived_at, commit_sha, commit_msg, diff_stat, diff_content
	FROM tasks WHERE id = ?
	`

	row := q.QueryRow(query, id)

	var task domain.Task
	if err := scanTask(row, &task); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("task %q not found", id)
		}
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	// Fetch notes
	notesQuery := `SELECT id, author, content, created_at FROM task_notes WHERE task_id = ? ORDER BY created_at ASC`
	rows, err := q.Query(notesQuery, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query task notes: %w", err)
	}
	defer func() { _ = rows.Close() }()

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

	// Fetch dependencies (prerequisites that must be completed)
	depQuery := `
	SELECT td.depends_on_id, COALESCE(p.status, 'open')
	FROM task_dependencies td
	LEFT JOIN tasks p ON td.depends_on_id = p.id
	WHERE td.task_id = ?
	ORDER BY td.depends_on_id ASC
	`
	depRows, err := q.Query(depQuery, id)
	if err == nil {
		defer func() { _ = depRows.Close() }()
		task.DependsOn = []string{}
		task.BlockedBy = []string{}
		for depRows.Next() {
			var depID, depStatus string
			if err := depRows.Scan(&depID, &depStatus); err == nil {
				task.DependsOn = append(task.DependsOn, depID)
				if domain.Status(depStatus) != domain.StatusDone && domain.Status(depStatus) != domain.StatusClosed {
					task.BlockedBy = append(task.BlockedBy, depID)
					task.IsBlocked = true
				}
			}
		}
	}

	// Fetch downstream (tasks that depend on this task)
	downstreamQuery := `
	SELECT task_id FROM task_dependencies WHERE depends_on_id = ? ORDER BY task_id ASC
	`
	downRows, err := q.Query(downstreamQuery, id)
	if err == nil {
		defer func() { _ = downRows.Close() }()
		task.Blocks = []string{}
		for downRows.Next() {
			var blockedID string
			if err := downRows.Scan(&blockedID); err == nil {
				task.Blocks = append(task.Blocks, blockedID)
			}
		}
	}

	return &task, nil
}

func (d *DB) GetTask(id string) (*domain.Task, error) {
	return d.getTaskLocked(d.conn, id)
}

func (d *DB) UpdateTask(id string, title, description *string, status *domain.Status, priority *domain.Priority, tags *[]string, addTags, removeTags []string, assignee, addNote *string, author string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Verify task exists and load current state in transaction
	var currentTagsJSON, currentTitle string
	err = tx.QueryRow("SELECT tags, title FROM tasks WHERE id = ?", id).Scan(&currentTagsJSON, &currentTitle)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	now := time.Now().UTC()
	updates := []string{"updated_at = ?"}
	args := []any{now}

	// Re-submitting an unchanged title is always allowed so that legacy titles
	// predating the length limit don't block unrelated edits.
	if title != nil && strings.TrimSpace(*title) != strings.TrimSpace(currentTitle) {
		normalized, err := domain.NormalizeTitle(*title)
		if err != nil {
			return nil, err
		}
		updates = append(updates, "title = ?")
		args = append(args, normalized)
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

		if *status == domain.StatusArchived {
			updates = append(updates, "archived_at = ?")
			args = append(args, now)
		} else {
			updates = append(updates, "archived_at = NULL")
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
	updateSQL := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(updates, ", ")) //nolint:gosec // G201: only fixed column/placeholder fragments are interpolated; values are bound via args

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

func (d *DB) ClaimTask(id, assignee string, force bool) (*domain.Task, error) {
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
	defer func() { _ = tx.Rollback() }()

	var currentAssignee, currentStatus, currentTagsJSON string
	query := "SELECT assignee, status, tags FROM tasks WHERE id = ?"
	if err := tx.QueryRow(query, id).Scan(&currentAssignee, &currentStatus, &currentTagsJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	// Conditional atomic claim: competing claim fails unless force is true
	if currentAssignee != "" && currentAssignee != assignee && !force {
		return nil, fmt.Errorf("task %s is already claimed by %q (use --force to overwrite)", id, currentAssignee)
	}

	var tags []string
	if err := json.Unmarshal([]byte(currentTagsJSON), &tags); err != nil {
		tags = []string{}
	}
	var newTags []string
	for _, tag := range tags {
		if tag != "orphaned" {
			newTags = append(newTags, tag)
		}
	}
	newTagsBytes, _ := json.Marshal(newTags)

	now := time.Now().UTC()
	newStatus := currentStatus
	if currentStatus == string(domain.StatusOpen) {
		newStatus = string(domain.StatusInProgress)
	}

	updateSQL := "UPDATE tasks SET assignee = ?, status = ?, tags = ?, updated_at = ? WHERE id = ?"
	if _, err := tx.Exec(updateSQL, assignee, newStatus, string(newTagsBytes), now, id); err != nil {
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

func (d *DB) orphanTaskLocked(id, actor, reason string, now time.Time) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentAssignee, currentStatus, currentTagsJSON string
	query := "SELECT assignee, status, tags FROM tasks WHERE id = ?"
	if err := tx.QueryRow(query, id).Scan(&currentAssignee, &currentStatus, &currentTagsJSON); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("task %s not found", id)
		}
		return fmt.Errorf("failed to query task: %w", err)
	}

	var tags []string
	if err := json.Unmarshal([]byte(currentTagsJSON), &tags); err != nil {
		tags = []string{}
	}

	hasOrphanTag := false
	for _, tag := range tags {
		if strings.EqualFold(tag, "orphaned") {
			hasOrphanTag = true
			break
		}
	}
	if !hasOrphanTag {
		tags = append(tags, "orphaned")
	}
	newTagsBytes, _ := json.Marshal(tags)

	newStatus := currentStatus
	if currentStatus == string(domain.StatusInProgress) {
		newStatus = string(domain.StatusOpen)
	}

	updateSQL := "UPDATE tasks SET assignee = '', status = ?, tags = ?, updated_at = ? WHERE id = ?"
	if _, err := tx.Exec(updateSQL, newStatus, string(newTagsBytes), now, id); err != nil {
		return fmt.Errorf("failed to orphan task: %w", err)
	}

	if actor == "" {
		actor = "system"
	}
	var noteContent string
	if reason != "" {
		if currentAssignee != "" {
			noteContent = fmt.Sprintf("Task orphaned: %s (dropped assignee %q, reset status to %s)", reason, currentAssignee, newStatus)
		} else {
			if strings.HasPrefix(reason, "inactive for ") {
				noteContent = fmt.Sprintf("Task marked orphaned: %s", reason)
			} else {
				noteContent = fmt.Sprintf("Task orphaned: %s (status reset to %s)", reason, newStatus)
			}
		}
	} else {
		if currentAssignee != "" {
			noteContent = fmt.Sprintf("Task orphaned due to inactivity (dropped assignee %q, reset status to %s)", currentAssignee, newStatus)
		} else {
			noteContent = fmt.Sprintf("Task marked as orphaned (status reset to %s)", newStatus)
		}
	}

	noteID := uuid.New().String()
	noteSQL := `INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`
	if _, err := tx.Exec(noteSQL, noteID, id, actor, noteContent, now); err != nil {
		return fmt.Errorf("failed to insert orphan note: %w", err)
	}

	return tx.Commit()
}

func (d *DB) OrphanTask(id, actor, reason string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().UTC()
	if err := d.orphanTaskLocked(id, actor, reason, now); err != nil {
		return nil, err
	}
	return d.getTaskLocked(d.conn, id)
}

func (d *DB) TransferTask(id, fromActor, toAssignee, note string, _ bool) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	toAssignee = strings.TrimSpace(toAssignee)
	if toAssignee == "" {
		return nil, fmt.Errorf("new assignee cannot be empty")
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentAssignee, currentStatus, currentTagsJSON string
	query := "SELECT assignee, status, tags FROM tasks WHERE id = ?"
	if err := tx.QueryRow(query, id).Scan(&currentAssignee, &currentStatus, &currentTagsJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	var tags []string
	if err := json.Unmarshal([]byte(currentTagsJSON), &tags); err != nil {
		tags = []string{}
	}
	var newTags []string
	for _, tag := range tags {
		if tag != "orphaned" {
			newTags = append(newTags, tag)
		}
	}
	newTagsBytes, _ := json.Marshal(newTags)

	now := time.Now().UTC()
	newStatus := currentStatus
	if currentStatus == string(domain.StatusOpen) {
		newStatus = string(domain.StatusInProgress)
	}

	updateSQL := "UPDATE tasks SET assignee = ?, status = ?, tags = ?, updated_at = ? WHERE id = ?"
	if _, err := tx.Exec(updateSQL, toAssignee, newStatus, string(newTagsBytes), now, id); err != nil {
		return nil, fmt.Errorf("failed to transfer task: %w", err)
	}

	noteAuthor := fromActor
	if noteAuthor == "" {
		noteAuthor = toAssignee
	}

	var noteContent string
	if currentAssignee != "" && currentAssignee != toAssignee {
		if note != "" {
			noteContent = fmt.Sprintf("Transferred task from %q to %q: %s", currentAssignee, toAssignee, note)
		} else {
			noteContent = fmt.Sprintf("Transferred task from %q to %q", currentAssignee, toAssignee)
		}
	} else {
		if note != "" {
			noteContent = fmt.Sprintf("Claimed task as %q: %s", toAssignee, note)
		} else {
			noteContent = fmt.Sprintf("Claimed task as %q", toAssignee)
		}
	}

	noteID := uuid.New().String()
	noteSQL := `INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`
	if _, err := tx.Exec(noteSQL, noteID, id, noteAuthor, noteContent, now); err != nil {
		return nil, fmt.Errorf("failed to insert transfer note: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transfer transaction: %w", err)
	}

	return d.getTaskLocked(d.conn, id)
}

func (d *DB) ArchiveTask(id, actor, reason string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentStatus string
	var archivedAt sql.NullTime
	query := "SELECT status, archived_at FROM tasks WHERE id = ?"
	if err := tx.QueryRow(query, id).Scan(&currentStatus, &archivedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	if archivedAt.Valid || currentStatus == string(domain.StatusArchived) {
		return nil, fmt.Errorf("task %s is already archived", id)
	}

	now := time.Now().UTC()
	updateSQL := "UPDATE tasks SET status = ?, archived_at = ?, updated_at = ? WHERE id = ?"
	if _, err := tx.Exec(updateSQL, string(domain.StatusArchived), now, now, id); err != nil {
		return nil, fmt.Errorf("failed to archive task: %w", err)
	}

	noteAuthor := strings.TrimSpace(actor)
	if noteAuthor == "" {
		noteAuthor = "system"
	}

	var noteContent string
	reason = strings.TrimSpace(reason)
	if reason != "" {
		noteContent = fmt.Sprintf("Archived task: %s", reason)
	} else {
		noteContent = "Archived task"
	}

	noteID := uuid.New().String()
	noteSQL := `INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`
	if _, err := tx.Exec(noteSQL, noteID, id, noteAuthor, noteContent, now); err != nil {
		return nil, fmt.Errorf("failed to insert archive note: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit archive transaction: %w", err)
	}

	return d.getTaskLocked(d.conn, id)
}

func (d *DB) UnarchiveTask(id, actor, reason string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentStatus, assignee string
	var archivedAt, closedAt sql.NullTime
	query := "SELECT status, archived_at, closed_at, assignee FROM tasks WHERE id = ?"
	if err := tx.QueryRow(query, id).Scan(&currentStatus, &archivedAt, &closedAt, &assignee); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	if !archivedAt.Valid && currentStatus != string(domain.StatusArchived) {
		return nil, fmt.Errorf("task %s is not archived", id)
	}

	now := time.Now().UTC()
	newStatus := domain.StatusOpen
	if closedAt.Valid {
		newStatus = domain.StatusDone
	} else if strings.TrimSpace(assignee) != "" {
		newStatus = domain.StatusInProgress
	}

	updateSQL := "UPDATE tasks SET status = ?, archived_at = NULL, updated_at = ? WHERE id = ?"
	if _, err := tx.Exec(updateSQL, string(newStatus), now, id); err != nil {
		return nil, fmt.Errorf("failed to unarchive task: %w", err)
	}

	noteAuthor := strings.TrimSpace(actor)
	if noteAuthor == "" {
		noteAuthor = "system"
	}

	var noteContent string
	reason = strings.TrimSpace(reason)
	if reason != "" {
		noteContent = fmt.Sprintf("Unarchived task: %s", reason)
	} else {
		noteContent = "Unarchived task"
	}

	noteID := uuid.New().String()
	noteSQL := `INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`
	if _, err := tx.Exec(noteSQL, noteID, id, noteAuthor, noteContent, now); err != nil {
		return nil, fmt.Errorf("failed to insert unarchive note: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit unarchive transaction: %w", err)
	}

	return d.getTaskLocked(d.conn, id)
}

func (d *DB) FindStaleTasks(scope, repoPath string, threshold time.Duration) ([]domain.StaleTaskCandidate, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.findStaleTasksLocked(scope, repoPath, threshold)
}

func (d *DB) findStaleTasksLocked(scope, repoPath string, threshold time.Duration) ([]domain.StaleTaskCandidate, error) {
	if threshold <= 0 {
		threshold = 1 * time.Hour
	}

	now := time.Now().UTC()
	cutoff := now.Add(-threshold)

	query := `SELECT id FROM tasks WHERE (status = 'in_progress' OR (status = 'open' AND assignee != '')) AND (archived_at IS NULL AND status != 'archived')`
	var args []any
	if scope != "" {
		query += " AND scope = ?"
		args = append(args, scope)
	}
	if scope == "local" && repoPath != "" {
		query += " AND repo_path = ?"
		args = append(args, repoPath)
	}
	query += " ORDER BY seq_num ASC"

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidateIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			candidateIDs = append(candidateIDs, id)
		}
	}
	_ = rows.Close()

	var candidates []domain.StaleTaskCandidate
	for _, id := range candidateIDs {
		t, err := d.getTaskLocked(d.conn, id)
		if err != nil || t == nil {
			continue
		}

		selfActivity := taskActivityTime(t)
		if selfActivity.IsZero() {
			selfActivity = t.CreatedAt
		}
		if !selfActivity.Before(cutoff) {
			continue
		}

		var allDepIDs []string
		allDepIDs = append(allDepIDs, t.DependsOn...)
		allDepIDs = append(allDepIDs, t.Blocks...)

		depActive := false
		for _, depID := range allDepIDs {
			depTask, err := d.getTaskLocked(d.conn, depID)
			if err != nil || depTask == nil {
				continue
			}
			depActivity := taskActivityTime(depTask)
			if !depActivity.Before(cutoff) {
				depActive = true
				break
			}
			if (depTask.Status == domain.StatusDone || depTask.Status == domain.StatusClosed) &&
				depTask.ClosedAt != nil && !depTask.ClosedAt.Before(cutoff) {
				depActive = true
				break
			}
		}

		if depActive {
			continue
		}

		inactiveDur := now.Sub(selfActivity)
		candidates = append(candidates, domain.StaleTaskCandidate{
			TaskID:           t.ID,
			Title:            t.Title,
			Assignee:         t.Assignee,
			Status:           t.Status,
			Priority:         t.Priority,
			InactiveDuration: inactiveDur,
			LastActivity:     selfActivity,
		})
	}

	return candidates, nil
}

func (d *DB) ReapStaleTasks(scope, repoPath string, threshold time.Duration, dryRun bool, actor, reason string) ([]domain.StaleTaskCandidate, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	candidates, err := d.findStaleTasksLocked(scope, repoPath, threshold)
	if err != nil {
		return nil, nil, err
	}

	if dryRun {
		return candidates, nil, nil
	}

	now := time.Now().UTC()
	var orphanedIDs []string
	for _, cand := range candidates {
		orphanReason := reason
		if orphanReason == "" {
			orphanReason = fmt.Sprintf("inactivity for %s (threshold: %s)", formatDurationBrief(cand.InactiveDuration), formatDurationBrief(threshold))
		}
		if err := d.orphanTaskLocked(cand.TaskID, actor, orphanReason, now); err == nil {
			orphanedIDs = append(orphanedIDs, cand.TaskID)
		}
	}

	return candidates, orphanedIDs, nil
}

func taskActivityTime(t *domain.Task) time.Time {
	if t == nil {
		return time.Time{}
	}
	latest := t.UpdatedAt
	if latest.IsZero() {
		latest = t.CreatedAt
	}
	for _, n := range t.Notes {
		if n.CreatedAt.After(latest) {
			latest = n.CreatedAt
		}
	}
	if t.ClosedAt != nil && t.ClosedAt.After(latest) {
		latest = *t.ClosedAt
	}
	return latest
}

func formatDurationBrief(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, mins)
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
	// Status filtering. "orphan" is not a stored status but a computed condition
	// (see Task.IsOrphaned), so it is handled separately from concrete statuses:
	//   - only "orphan"           -> same as OnlyOrphaned
	//   - only concrete statuses  -> SQL status IN (...)
	//   - "orphan" + concrete     -> union: status IN (...) OR task is orphaned
	var concreteStatuses []domain.Status
	wantOrphan := false
	for _, s := range filter.EffectiveStatuses() {
		if s == domain.StatusOrphan {
			wantOrphan = true
		} else {
			concreteStatuses = append(concreteStatuses, s)
		}
	}
	statusUnionOrphan := wantOrphan && len(concreteStatuses) > 0
	statusSet := make(map[domain.Status]bool, len(concreteStatuses))
	if wantOrphan && len(concreteStatuses) == 0 {
		filter.OnlyOrphaned = true
	} else if len(concreteStatuses) > 0 {
		placeholders := make([]string, len(concreteStatuses))
		for i, s := range concreteStatuses {
			placeholders[i] = "?"
			args = append(args, string(s))
			statusSet[s] = true
		}
		inCond := fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ","))
		if statusUnionOrphan {
			// Candidate orphans are pre-selected in SQL; exact orphan check happens in memory below.
			inCond = fmt.Sprintf("(%s OR (tasks.archived_at IS NULL AND tasks.status NOT IN ('done', 'closed', 'archived')))", inCond)
		}
		conditions = append(conditions, inCond)
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
	if filter.OnlyOrphaned {
		// Orphaned tasks cannot be done, closed, or archived
		conditions = append(conditions, "tasks.archived_at IS NULL AND tasks.status NOT IN ('done', 'closed', 'archived')")
	}
	if filter.OnlyArchived {
		conditions = append(conditions, "(tasks.archived_at IS NOT NULL OR tasks.status = 'archived')")
	} else if !filter.IncludeArchived {
		conditions = append(conditions, "tasks.archived_at IS NULL AND tasks.status != 'archived'")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limitClause := ""
	if filter.Limit > 0 && filter.Search == "" && !filter.OnlyReady && !filter.OnlyBlocked && !filter.OnlyOrphaned && !statusUnionOrphan && filter.DependsOn == "" && filter.Blocks == "" {
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
	       created_at, updated_at, closed_at, archived_at, commit_sha, commit_msg, diff_stat, '' as diff_content
	FROM tasks %s
	%s %s
	`, whereClause, orderClause, limitClause)

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*domain.Task
	for rows.Next() {
		var task domain.Task
		if err := scanTask(rows, &task); err != nil {
			return nil, err
		}
		task.Notes = []domain.TaskNote{}
		tasks = append(tasks, &task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tasks: %w", err)
	}
	_ = rows.Close() // Explicitly release cursor back to single connection pool before secondary query

	// Batch load notes for listed tasks (unless explicitly omitted and not required for in-memory search / orphan checks)
	if len(tasks) > 0 && (!filter.OmitNotes || filter.Search != "" || filter.OnlyOrphaned || statusUnionOrphan) {
		taskMap := make(map[string]*domain.Task, len(tasks))
		var taskIDs []any
		var placeholders []string
		for _, t := range tasks {
			taskMap[t.ID] = t
			taskIDs = append(taskIDs, t.ID)
			placeholders = append(placeholders, "?")
		}

		notesQuery := fmt.Sprintf("SELECT id, task_id, author, content, created_at FROM task_notes WHERE task_id IN (%s) ORDER BY created_at ASC", strings.Join(placeholders, ",")) //nolint:gosec // G201: only fixed column/placeholder fragments are interpolated; values are bound via args
		noteRows, err := d.conn.Query(notesQuery, taskIDs...)
		if err == nil {
			defer func() { _ = noteRows.Close() }()
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

	// Batch load dependencies for listed tasks
	if len(tasks) > 0 {
		taskMap := make(map[string]*domain.Task, len(tasks))
		var taskIDs []any
		var placeholders []string
		for _, t := range tasks {
			t.DependsOn = []string{}
			t.BlockedBy = []string{}
			t.Blocks = []string{}
			taskMap[t.ID] = t
			taskIDs = append(taskIDs, t.ID)
			placeholders = append(placeholders, "?")
		}

		inClause := strings.Join(placeholders, ",")
		//nolint:gosec // G201: only "?" placeholders are interpolated; IDs are bound via args
		depsQuery := fmt.Sprintf(`
			SELECT td.task_id, td.depends_on_id, COALESCE(p.status, 'open')
			FROM task_dependencies td
			LEFT JOIN tasks p ON td.depends_on_id = p.id
			WHERE td.task_id IN (%s) OR td.depends_on_id IN (%s)
		`, inClause, inClause)

		args := make([]any, 0, 2*len(taskIDs))
		args = append(args, taskIDs...)
		args = append(args, taskIDs...)
		depRows, err := d.conn.Query(depsQuery, args...)
		if err == nil {
			defer func() { _ = depRows.Close() }()
			for depRows.Next() {
				var tID, depID, depStatus string
				if err := depRows.Scan(&tID, &depID, &depStatus); err == nil {
					if t, ok := taskMap[tID]; ok {
						t.DependsOn = append(t.DependsOn, depID)
						if domain.Status(depStatus) != domain.StatusDone && domain.Status(depStatus) != domain.StatusClosed {
							t.BlockedBy = append(t.BlockedBy, depID)
							t.IsBlocked = true
						}
					}
					if depT, ok := taskMap[depID]; ok {
						depT.Blocks = append(depT.Blocks, tID)
					}
				}
			}
		}
	}

	if filter.OnlyReady || filter.OnlyBlocked || filter.OnlyOrphaned || statusUnionOrphan || filter.DependsOn != "" || filter.Blocks != "" {
		var filtered []*domain.Task
		for _, t := range tasks {
			if filter.OnlyReady && (t.Status != domain.StatusOpen || t.IsBlocked) {
				continue
			}
			if filter.OnlyBlocked && !t.IsBlocked {
				continue
			}
			if filter.OnlyOrphaned && !t.IsOrphaned() {
				continue
			}
			if statusUnionOrphan && !statusSet[t.Status] && !t.IsOrphaned() {
				continue
			}
			if filter.DependsOn != "" {
				hasDep := false
				for _, d := range t.DependsOn {
					if d == filter.DependsOn {
						hasDep = true
						break
					}
				}
				if !hasDep {
					continue
				}
			}
			if filter.Blocks != "" {
				blocksIt := false
				for _, b := range t.Blocks {
					if b == filter.Blocks {
						blocksIt = true
						break
					}
				}
				if !blocksIt {
					continue
				}
			}
			filtered = append(filtered, t)
		}
		tasks = filtered
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
	}

	if filter.Limit > 0 && len(tasks) > filter.Limit {
		tasks = tasks[:filter.Limit]
	}

	return tasks, nil
}

func (d *DB) DeleteTask(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Fetch full task including notes & git diff inside tx
	task, err := d.getTaskLocked(tx, id)
	if err != nil {
		return err
	}

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task for tombstone: %w", err)
	}

	// 2. Insert into task_tombstones
	now := time.Now().UTC()
	_, err = tx.Exec(`
		INSERT INTO task_tombstones (id, scope, repo_path, task_json, deleted_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET task_json = excluded.task_json, deleted_at = excluded.deleted_at
	`, task.ID, task.Scope, task.RepoPath, string(taskJSON), now)
	if err != nil {
		return fmt.Errorf("failed to record tombstone: %w", err)
	}

	// 3. Prune old tombstones if exceeding limit (keep last 50 per scope/repo)
	_, _ = tx.Exec(`
		DELETE FROM task_tombstones
		WHERE id NOT IN (
			SELECT id FROM task_tombstones
			WHERE scope = ? AND repo_path = ?
			ORDER BY deleted_at DESC
			LIMIT 50
		) AND scope = ? AND repo_path = ?
	`, task.Scope, task.RepoPath, task.Scope, task.RepoPath)

	// 4. Delete notes
	if _, err := tx.Exec("DELETE FROM task_notes WHERE task_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete task notes: %w", err)
	}

	// 5. Delete task
	res, err := tx.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("task %q not found", id)
	}

	return tx.Commit()
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
	updateSQL := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(updates, ", ")) //nolint:gosec // G201: only fixed column/placeholder fragments are interpolated; values are bound via args
	if _, err := d.conn.Exec(updateSQL, args...); err != nil {
		return nil, fmt.Errorf("failed to attach git info: %w", err)
	}

	return d.GetTask(id)
}

func (d *DB) restoreTaskTx(tx *sql.Tx, task *domain.Task) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}

	tagsJSON, err := json.Marshal(task.Tags)
	if err != nil {
		tagsJSON = []byte("[]")
	}

	var closedAt, archivedAt any
	if task.ClosedAt != nil {
		closedAt = *task.ClosedAt
	}
	if task.ArchivedAt != nil {
		archivedAt = *task.ArchivedAt
	}

	var seqNum int
	if parts := strings.Split(task.ID, "-"); len(parts) == 2 {
		seqNum, _ = strconv.Atoi(parts[1])
	}

	// Clean up any stale notes before restoring to prevent PRIMARY KEY collisions
	if _, err := tx.Exec("DELETE FROM task_notes WHERE task_id = ?", task.ID); err != nil {
		return fmt.Errorf("failed to clean existing notes: %w", err)
	}

	insertSQL := `
	INSERT INTO tasks (
		id, scope, repo_path, seq_num, title, description, status, priority, tags, author, assignee, created_at, updated_at, closed_at, archived_at,
		commit_sha, commit_msg, diff_stat, diff_content
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		scope = excluded.scope,
		repo_path = excluded.repo_path,
		seq_num = excluded.seq_num,
		title = excluded.title,
		description = excluded.description,
		status = excluded.status,
		priority = excluded.priority,
		tags = excluded.tags,
		author = excluded.author,
		assignee = excluded.assignee,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at,
		closed_at = excluded.closed_at,
		archived_at = excluded.archived_at,
		commit_sha = excluded.commit_sha,
		commit_msg = excluded.commit_msg,
		diff_stat = excluded.diff_stat,
		diff_content = excluded.diff_content
	`
	_, err = tx.Exec(insertSQL,
		task.ID, task.Scope, task.RepoPath, seqNum, task.Title, task.Description,
		string(task.Status), string(task.Priority), string(tagsJSON), task.Author, task.Assignee,
		task.CreatedAt, task.UpdatedAt, closedAt, archivedAt,
		task.CommitSHA, task.CommitMsg, task.DiffStat, task.DiffContent,
	)
	if err != nil {
		return fmt.Errorf("failed to insert restored task: %w", err)
	}

	for _, n := range task.Notes {
		noteID := n.ID
		if noteID == "" {
			noteID = uuid.New().String()
		}
		_, err = tx.Exec(`INSERT INTO task_notes (id, task_id, author, content, created_at) VALUES (?, ?, ?, ?, ?)`,
			noteID, task.ID, n.Author, n.Content, n.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to restore note: %w", err)
		}
	}

	seqRepoPath := task.RepoPath
	if task.Scope == string(domain.ScopeGlobal) {
		seqRepoPath = ""
	}

	_, err = tx.Exec(`
	INSERT INTO task_sequences (scope, repo_path, last_seq) VALUES (?, ?, ?)
	ON CONFLICT(scope, repo_path) DO UPDATE SET last_seq = MAX(last_seq, excluded.last_seq)
	`, task.Scope, seqRepoPath, seqNum)
	if err != nil {
		return fmt.Errorf("failed to update sequence: %w", err)
	}

	// Restore dependencies if present in task.DependsOn
	now := time.Now().UTC()
	for _, depID := range task.DependsOn {
		var exists int
		if err := tx.QueryRow("SELECT 1 FROM tasks WHERE id = ?", depID).Scan(&exists); err == nil {
			_, _ = tx.Exec("INSERT OR IGNORE INTO task_dependencies (task_id, depends_on_id, created_at) VALUES (?, ?, ?)", task.ID, depID, now)
		}
	}

	_, _ = tx.Exec("DELETE FROM task_tombstones WHERE id = ?", task.ID)
	return nil
}

func (d *DB) RestoreTask(task *domain.Task) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.restoreTaskTx(tx, task); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) RestoreTaskByID(id string) (*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var taskJSON string
	err := d.conn.QueryRow("SELECT task_json FROM task_tombstones WHERE id = ?", id).Scan(&taskJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("tombstone for task %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query tombstone: %w", err)
	}

	var task domain.Task
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tombstone task: %w", err)
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.restoreTaskTx(tx, &task); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit restore: %w", err)
	}

	return &task, nil
}

func (d *DB) ListTombstones(scope, repoPath string) ([]*domain.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var query string
	var args []any

	if scope == string(domain.ScopeGlobal) {
		query = "SELECT task_json FROM task_tombstones WHERE scope = ? ORDER BY deleted_at DESC LIMIT 50"
		args = []any{scope}
	} else if scope != "" && repoPath != "" {
		query = "SELECT task_json FROM task_tombstones WHERE scope = ? AND repo_path = ? ORDER BY deleted_at DESC LIMIT 50"
		args = []any{scope, repoPath}
	} else {
		query = "SELECT task_json FROM task_tombstones ORDER BY deleted_at DESC LIMIT 50"
	}

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tombstones: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*domain.Task
	for rows.Next() {
		var taskJSON string
		if err := rows.Scan(&taskJSON); err != nil {
			return nil, err
		}
		var t domain.Task
		if err := json.Unmarshal([]byte(taskJSON), &t); err != nil {
			continue
		}
		tasks = append(tasks, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tombstones: %w", err)
	}
	return tasks, nil
}

func (d *DB) AddDependency(taskID, dependsOnID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	taskID = strings.TrimSpace(taskID)
	dependsOnID = strings.TrimSpace(dependsOnID)
	if taskID == "" || dependsOnID == "" {
		return fmt.Errorf("task ID and dependency ID cannot be empty")
	}
	if taskID == dependsOnID {
		return fmt.Errorf("task %s cannot depend on itself", taskID)
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Verify both tasks exist
	var exists int
	if err := tx.QueryRow("SELECT 1 FROM tasks WHERE id = ?", taskID).Scan(&exists); err != nil {
		return fmt.Errorf("task %s not found", taskID)
	}
	if err := tx.QueryRow("SELECT 1 FROM tasks WHERE id = ?", dependsOnID).Scan(&exists); err != nil {
		return fmt.Errorf("prerequisite task %s not found", dependsOnID)
	}

	// Cycle detection: Does dependsOnID already depend on taskID (directly or transitively)?
	cycleQuery := `
	WITH RECURSIVE chain(id) AS (
		SELECT depends_on_id FROM task_dependencies WHERE task_id = ?
		UNION
		SELECT d.depends_on_id FROM task_dependencies d
		JOIN chain c ON d.task_id = c.id
	)
	SELECT 1 FROM chain WHERE id = ? LIMIT 1;
	`
	var cycleExists int
	if err := tx.QueryRow(cycleQuery, dependsOnID, taskID).Scan(&cycleExists); err == nil {
		return fmt.Errorf("cannot link %s -> %s: dependency cycle detected", taskID, dependsOnID)
	}

	now := time.Now().UTC()
	_, err = tx.Exec(`
		INSERT OR IGNORE INTO task_dependencies (task_id, depends_on_id, created_at)
		VALUES (?, ?, ?)
	`, taskID, dependsOnID, now)
	if err != nil {
		return fmt.Errorf("failed to insert dependency: %w", err)
	}

	return tx.Commit()
}

func (d *DB) RemoveDependency(taskID, dependsOnID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	taskID = strings.TrimSpace(taskID)
	dependsOnID = strings.TrimSpace(dependsOnID)

	res, err := d.conn.Exec("DELETE FROM task_dependencies WHERE task_id = ? AND depends_on_id = ?", taskID, dependsOnID)
	if err != nil {
		return fmt.Errorf("failed to delete dependency: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("dependency %s -> %s not found", taskID, dependsOnID)
	}
	return nil
}

func (d *DB) GetDependencies(taskID string) (dependsOn, blocks []string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	depRows, err := d.conn.Query("SELECT depends_on_id FROM task_dependencies WHERE task_id = ? ORDER BY depends_on_id ASC", taskID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = depRows.Close() }()

	dependsOn = []string{}
	for depRows.Next() {
		var id string
		if err := depRows.Scan(&id); err == nil {
			dependsOn = append(dependsOn, id)
		}
	}

	blockRows, err := d.conn.Query("SELECT task_id FROM task_dependencies WHERE depends_on_id = ? ORDER BY task_id ASC", taskID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = blockRows.Close() }()

	blocks = []string{}
	for blockRows.Next() {
		var id string
		if err := blockRows.Scan(&id); err == nil {
			blocks = append(blocks, id)
		}
	}

	return dependsOn, blocks, nil
}

func (d *DB) CheckBlocked(taskID string) (blocked bool, unfinishedPrereqs []*domain.Task, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
	SELECT p.id, p.scope, p.repo_path, p.title, p.description, p.status, p.priority, p.tags, p.author, p.assignee,
	       p.created_at, p.updated_at, p.closed_at, p.commit_sha, p.commit_msg, p.diff_stat, p.diff_content
	FROM task_dependencies td
	JOIN tasks p ON td.depends_on_id = p.id
	WHERE td.task_id = ? AND p.status NOT IN ('done', 'closed')
	ORDER BY p.id ASC;
	`
	rows, err := d.conn.Query(query, taskID)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = rows.Close() }()

	var prereqs []*domain.Task
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
			return false, nil, err
		}
		task.Status = domain.Status(statusStr)
		task.Priority = domain.Priority(priorityStr)
		prereqs = append(prereqs, &task)
	}

	return len(prereqs) > 0, prereqs, nil
}
