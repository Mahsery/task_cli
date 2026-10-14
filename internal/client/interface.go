package client

import (
	"time"

	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

type TaskClient interface {
	Connect() error
	Close() error
	Ping() error
	PingPassive() error
	CreateTask(p protocol.CreateTaskPayload) (*domain.Task, error)
	GetTask(id string) (*domain.Task, error)
	UpdateTask(p protocol.UpdateTaskPayload) (*domain.Task, error)
	ClaimTask(id, assignee string, force bool) (*domain.Task, error)
	ClaimTaskWithBranch(id, assignee, branch string, force bool) (*domain.Task, error)
	Actor() string
	ListTasks(filter domain.TaskFilter) ([]*domain.Task, error)
	ArchiveTask(id, actor, reason string) (*domain.Task, error)
	UnarchiveTask(id, actor, reason string) (*domain.Task, error)
	OrphanTask(id, actor, reason string) (*domain.Task, error)
	TransferTask(id, actor, newAssignee, note string, force bool) (*domain.Task, error)
	ReapStaleTasks(scope, repoPath string, threshold time.Duration, dryRun bool, actor, reason string) (*protocol.ReapStaleTasksResult, error)
	DeleteTask(id string) error
	RestoreTask(task *domain.Task) (*domain.Task, error)
	RestoreTaskByID(id string) (*domain.Task, error)
	ListTombstones() ([]*domain.Task, error)
	AddDependency(taskID, dependsOnID string) error
	RemoveDependency(taskID, dependsOnID string) error
	GetDependencies(taskID string) (*protocol.DependenciesData, error)
	AttachGit(taskID string, sha, msg, diffStat, diffContent string, markDone bool) (*domain.Task, error)
	AttachGitWithForce(taskID string, sha, msg, diffStat, diffContent string, markDone, force bool) (*domain.Task, error)
	AttachGitCommits(taskID string, commits []domain.TaskCommit, markDone, force bool) (*domain.Task, error)
	StreamEvents(handler func(protocol.Event)) error
	ShutdownDaemon() error
	CreateBackup(name string, maxSnapshots int) (string, error)
	ListBackups() ([]protocol.BackupInfo, error)
	VerifyBackup(target string) (string, error)
	RestoreBackup(backupPath string) (string, error)
	FindStaleTasks(scope, repoPath string, threshold time.Duration) ([]domain.StaleTaskCandidate, error)
	ListTopics() ([]*domain.Topic, error)
	GetTopic(ref string) (*domain.Topic, error)
	CreateTopic(p protocol.CreateTopicPayload) (*domain.Topic, error)
	UpdateTopic(p protocol.UpdateTopicPayload) (*domain.Topic, error)
	MoveTopic(ref, parent string) (*domain.Topic, error)
	DeleteTopic(ref, moveTo string) error
	MoveItems(ids []string, topic, actor string) ([]*domain.Task, error)
	PromotePost(id, topic, author string) (*protocol.PromotePostResult, error)
}

var _ TaskClient = (*Client)(nil)
