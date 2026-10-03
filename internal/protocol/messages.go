package protocol

import (
	"encoding/json"
	"time"

	"task_cli/internal/domain"
)

type Action string

const (
	ActionPing             Action = "ping"
	ActionCreateTask       Action = "create_task"
	ActionUpdateTask       Action = "update_task"
	ActionClaimTask        Action = "claim_task"
	ActionGetTask          Action = "get_task"
	ActionListTasks        Action = "list_tasks"
	ActionDeleteTask       Action = "delete_task"
	ActionRestoreTask      Action = "restore_task"
	ActionAttachGit        Action = "attach_git"
	ActionStreamEvents     Action = "stream_events"
	ActionShutdown         Action = "shutdown"
	ActionBackupCreate     Action = "backup_create"
	ActionBackupList       Action = "backup_list"
	ActionBackupRestore    Action = "backup_restore"
	ActionBackupVerify     Action = "backup_verify"
	ActionListTombstones   Action = "list_tombstones"
	ActionAddDependency    Action = "add_dependency"
	ActionRemoveDependency Action = "remove_dependency"
	ActionGetDependencies  Action = "get_dependencies"
	ActionOrphanTask       Action = "orphan_task"
	ActionReapStaleTasks   Action = "reap_stale_tasks"
	ActionTransferTask     Action = "transfer_task"
	ActionArchiveTask      Action = "task.archive"
	ActionUnarchiveTask    Action = "task.unarchive"
)

type Request struct {
	ID       string          `json:"id"`
	Action   Action          `json:"action"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Scope    string          `json:"scope,omitempty"`     // "local" or "global"
	RepoPath string          `json:"repo_path,omitempty"` // current repo root
}

type Response struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type CreateTaskPayload struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Status      domain.Status   `json:"status"`
	Priority    domain.Priority `json:"priority"`
	Tags        []string        `json:"tags"`
	Author      string          `json:"author"`
	Assignee    string          `json:"assignee,omitempty"`
	DependsOn   []string        `json:"depends_on,omitempty"`
}

type UpdateTaskPayload struct {
	ID          string           `json:"id"`
	Title       *string          `json:"title,omitempty"`
	Description *string          `json:"description,omitempty"`
	Status      *domain.Status   `json:"status,omitempty"`
	Priority    *domain.Priority `json:"priority,omitempty"`
	Tags        *[]string        `json:"tags,omitempty"`
	AddTags     []string         `json:"add_tags,omitempty"`
	RemoveTags  []string         `json:"remove_tags,omitempty"`
	Assignee    *string          `json:"assignee,omitempty"`
	AddNote     *string          `json:"add_note,omitempty"`
	Author      string           `json:"author,omitempty"`
	Force       bool             `json:"force,omitempty"`
}

type ClaimTaskPayload struct {
	ID       string `json:"id"`
	Assignee string `json:"assignee"`
	Force    bool   `json:"force,omitempty"`
}

type GetTaskPayload struct {
	ID string `json:"id"`
}

type ListTasksPayload struct {
	Scope           string           `json:"scope"`
	RepoPath        string           `json:"repo_path"`
	Status          *domain.Status   `json:"status,omitempty"`
	Statuses        []domain.Status  `json:"statuses,omitempty"`
	Priority        *domain.Priority `json:"priority,omitempty"`
	Tag             string           `json:"tag,omitempty"`
	Assignee        string           `json:"assignee,omitempty"`
	Search          string           `json:"search,omitempty"`
	Sort            string           `json:"sort,omitempty"`
	Limit           int              `json:"limit,omitempty"`
	IncludeNotes    bool             `json:"include_notes,omitempty"`
	OnlyReady       bool             `json:"only_ready,omitempty"`
	OnlyBlocked     bool             `json:"only_blocked,omitempty"`
	OnlyOrphaned    bool             `json:"only_orphaned,omitempty"`
	IncludeArchived bool             `json:"include_archived,omitempty"`
	OnlyArchived    bool             `json:"only_archived,omitempty"`
	DependsOn       string           `json:"depends_on,omitempty"`
	Blocks          string           `json:"blocks,omitempty"`
}

type DeleteTaskPayload struct {
	ID string `json:"id"`
}

type RestoreTaskPayload struct {
	ID   string       `json:"id,omitempty"`
	Task *domain.Task `json:"task,omitempty"`
}

type AttachGitPayload struct {
	TaskID      string `json:"task_id"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	CommitMsg   string `json:"commit_msg,omitempty"`
	DiffStat    string `json:"diff_stat,omitempty"`
	DiffContent string `json:"diff_content,omitempty"`
	MarkDone    bool   `json:"mark_done"`
	Force       bool   `json:"force,omitempty"`
}

type AddDependencyPayload struct {
	TaskID      string `json:"task_id"`
	DependsOnID string `json:"depends_on_id"`
}

type RemoveDependencyPayload struct {
	TaskID      string `json:"task_id"`
	DependsOnID string `json:"depends_on_id"`
}

type GetDependenciesPayload struct {
	TaskID string `json:"task_id"`
}

type DependenciesData struct {
	TaskID    string   `json:"task_id"`
	DependsOn []string `json:"depends_on"`
	BlockedBy []string `json:"blocked_by"`
	Blocks    []string `json:"blocks"`
	IsBlocked bool     `json:"is_blocked"`
}

type EventType string

const (
	EventTaskCreated EventType = "task_created"
	EventTaskUpdated EventType = "task_updated"
	EventTaskDeleted EventType = "task_deleted"
	EventGitAttached EventType = "git_attached"
	EventSnapshot    EventType = "snapshot"
)

type Event struct {
	Type      EventType    `json:"type"`
	Timestamp time.Time    `json:"timestamp"`
	Task      *domain.Task `json:"task,omitempty"`
	Message   string       `json:"message"`
}

type BackupCreatePayload struct {
	Name         string `json:"name,omitempty"`
	MaxSnapshots int    `json:"max_snapshots,omitempty"`
}

type BackupRestorePayload struct {
	BackupPath string `json:"backup_path"`
}

type BackupVerifyPayload struct {
	Target string `json:"target,omitempty"`
}

type BackupInfo struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Location string    `json:"location,omitempty"` // "local" or "central"
}

type OrphanTaskPayload struct {
	ID     string `json:"id"`
	Actor  string `json:"actor,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type ReapStaleTasksPayload struct {
	Scope     string        `json:"scope"`
	RepoPath  string        `json:"repo_path"`
	Threshold time.Duration `json:"threshold"`
	DryRun    bool          `json:"dry_run"`
	Actor     string        `json:"actor,omitempty"`
	Reason    string        `json:"reason,omitempty"`
}

type ReapStaleTasksResult struct {
	Threshold  time.Duration               `json:"threshold"`
	DryRun     bool                        `json:"dry_run"`
	Candidates []domain.StaleTaskCandidate `json:"candidates"`
	Orphaned   []string                    `json:"orphaned"`
}

type TransferTaskPayload struct {
	ID          string `json:"id"`
	NewAssignee string `json:"new_assignee"`
	Note        string `json:"note,omitempty"`
	Actor       string `json:"actor,omitempty"`
	Force       bool   `json:"force,omitempty"`
}

type ArchiveTaskPayload struct {
	ID     string `json:"id"`
	Actor  string `json:"actor,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type UnarchiveTaskPayload struct {
	ID     string `json:"id"`
	Actor  string `json:"actor,omitempty"`
	Reason string `json:"reason,omitempty"`
}
