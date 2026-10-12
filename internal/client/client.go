package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"

	"github.com/google/uuid"
)

type Client struct {
	cfg      *config.Config
	conn     net.Conn
	enc      *json.Encoder
	dec      *json.Decoder
	Scope    string
	RepoPath string
}

func New(cfg *config.Config, scope, repoPath string) *Client {
	return &Client{
		cfg:      cfg,
		Scope:    scope,
		RepoPath: repoPath,
	}
}

func (c *Client) Connect() error {
	conn, err := c.dial()
	if err == nil {
		c.conn = conn
		c.enc = json.NewEncoder(conn)
		c.dec = json.NewDecoder(conn)
		return nil
	}

	// Auto-spawn daemon if not running
	if err := c.spawnDaemon(); err != nil {
		return fmt.Errorf("failed to auto-spawn daemon: %w", err)
	}

	// Retry dialing with backoff
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if conn, err := c.dial(); err == nil {
			c.conn = conn
			c.enc = json.NewEncoder(conn)
			c.dec = json.NewDecoder(conn)
			return nil
		}
	}

	return fmt.Errorf("could not connect to daemon after auto-spawn")
}

func (c *Client) Close() error {
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.enc = nil
		c.dec = nil
		return err
	}
	return nil
}

func (c *Client) dial() (net.Conn, error) {
	if c.cfg.TCPAddr != "" {
		return net.DialTimeout("tcp", c.cfg.TCPAddr, 500*time.Millisecond)
	}
	return net.DialTimeout("unix", c.cfg.SocketPath, 500*time.Millisecond)
}

func (c *Client) spawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		exe = "tk"
	}

	cmd := exec.Command(exe, "daemon", "run")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Run in new session so daemon persists
	}
	// Detach stdio
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	return cmd.Start()
}

func (c *Client) call(action protocol.Action, payload any) (*protocol.Response, error) {
	if c.conn == nil {
		if err := c.Connect(); err != nil {
			return nil, err
		}
	}

	var payloadBytes []byte
	var err error
	payload = c.normalizePayload(payload)
	if payload != nil {
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request payload: %w", err)
		}
	}

	reqID := uuid.New().String()
	req := protocol.Request{
		ID:       reqID,
		Action:   action,
		Payload:  payloadBytes,
		Scope:    c.Scope,
		RepoPath: c.RepoPath,
	}

	if err := c.enc.Encode(req); err != nil {
		_ = c.Close()
		c.conn = nil
		c.enc = nil
		c.dec = nil
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp protocol.Response
	if err := c.dec.Decode(&resp); err != nil {
		_ = c.Close()
		c.conn = nil
		c.enc = nil
		c.dec = nil
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}

	return &resp, nil
}

func (c *Client) Ping() error {
	_, err := c.call(protocol.ActionPing, nil)
	return err
}

func (c *Client) PingPassive() error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	req := protocol.Request{
		ID:     uuid.New().String(),
		Action: protocol.ActionPing,
	}
	if err := enc.Encode(req); err != nil {
		return err
	}
	var resp protocol.Response
	if err := dec.Decode(&resp); err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func (c *Client) CreateTask(p protocol.CreateTaskPayload) (*domain.Task, error) {
	resp, err := c.call(protocol.ActionCreateTask, p)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) GetTask(id string) (*domain.Task, error) {
	resp, err := c.call(protocol.ActionGetTask, protocol.GetTaskPayload{ID: id})
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) UpdateTask(p protocol.UpdateTaskPayload) (*domain.Task, error) {
	resp, err := c.call(protocol.ActionUpdateTask, p)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) ClaimTask(id, assignee string, force bool) (*domain.Task, error) {
	p := protocol.ClaimTaskPayload{
		ID:       id,
		Assignee: assignee,
		Force:    force,
	}
	resp, err := c.call(protocol.ActionClaimTask, p)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) Actor() string {
	if c.cfg != nil && c.cfg.Actor != "" {
		return c.cfg.Actor
	}
	return ""
}

func (c *Client) ListTasks(filter domain.TaskFilter) ([]*domain.Task, error) {
	scope := filter.Scope
	if scope == "" {
		scope = c.Scope
	}
	repoPath := filter.RepoPath
	if repoPath == "" {
		repoPath = c.RepoPath
	}
	f := filter
	f.Scope = scope
	f.RepoPath = repoPath
	p := protocol.FilterToListTasksPayload(f)
	resp, err := c.call(protocol.ActionListTasks, p)
	if err != nil {
		return nil, err
	}
	var tasks []*domain.Task
	if err := json.Unmarshal(resp.Data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (c *Client) ArchiveTask(id, actor, reason string) (*domain.Task, error) {
	p := protocol.ArchiveTaskPayload{
		ID:     id,
		Actor:  actor,
		Reason: reason,
	}
	resp, err := c.call(protocol.ActionArchiveTask, p)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) UnarchiveTask(id, actor, reason string) (*domain.Task, error) {
	p := protocol.UnarchiveTaskPayload{
		ID:     id,
		Actor:  actor,
		Reason: reason,
	}
	resp, err := c.call(protocol.ActionUnarchiveTask, p)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) OrphanTask(id, actor, reason string) (*domain.Task, error) {
	p := protocol.OrphanTaskPayload{
		ID:     id,
		Actor:  actor,
		Reason: reason,
	}
	resp, err := c.call(protocol.ActionOrphanTask, p)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) TransferTask(id, actor, newAssignee, note string, force bool) (*domain.Task, error) {
	p := protocol.TransferTaskPayload{
		ID:          id,
		NewAssignee: newAssignee,
		Note:        note,
		Actor:       actor,
		Force:       force,
	}
	resp, err := c.call(protocol.ActionTransferTask, p)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) ReapStaleTasks(scope, repoPath string, threshold time.Duration, dryRun bool, actor, reason string) (*protocol.ReapStaleTasksResult, error) {
	if scope == "" {
		scope = c.Scope
	}
	if repoPath == "" {
		repoPath = c.RepoPath
	}
	p := protocol.ReapStaleTasksPayload{
		Scope:     scope,
		RepoPath:  repoPath,
		Threshold: threshold,
		DryRun:    dryRun,
		Actor:     actor,
		Reason:    reason,
	}
	resp, err := c.call(protocol.ActionReapStaleTasks, p)
	if err != nil {
		return nil, err
	}
	var res protocol.ReapStaleTasksResult
	if err := json.Unmarshal(resp.Data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) FindStaleTasks(scope, repoPath string, threshold time.Duration) ([]domain.StaleTaskCandidate, error) {
	res, err := c.ReapStaleTasks(scope, repoPath, threshold, true, "", "")
	if err != nil {
		return nil, err
	}
	return res.Candidates, nil
}

func (c *Client) DeleteTask(id string) error {
	_, err := c.call(protocol.ActionDeleteTask, protocol.DeleteTaskPayload{ID: id})
	return err
}

func (c *Client) RestoreTask(task *domain.Task) (*domain.Task, error) {
	resp, err := c.call(protocol.ActionRestoreTask, protocol.RestoreTaskPayload{Task: task})
	if err != nil {
		return nil, err
	}
	var res domain.Task
	if err := json.Unmarshal(resp.Data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) RestoreTaskByID(id string) (*domain.Task, error) {
	resp, err := c.call(protocol.ActionRestoreTask, protocol.RestoreTaskPayload{ID: id})
	if err != nil {
		return nil, err
	}
	var res domain.Task
	if err := json.Unmarshal(resp.Data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) ListTombstones() ([]*domain.Task, error) {
	resp, err := c.call(protocol.ActionListTombstones, nil)
	if err != nil {
		return nil, err
	}
	var tasks []*domain.Task
	if err := json.Unmarshal(resp.Data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (c *Client) AddDependency(taskID, dependsOnID string) error {
	p := protocol.AddDependencyPayload{
		TaskID:      taskID,
		DependsOnID: dependsOnID,
	}
	_, err := c.call(protocol.ActionAddDependency, p)
	return err
}

func (c *Client) RemoveDependency(taskID, dependsOnID string) error {
	p := protocol.RemoveDependencyPayload{
		TaskID:      taskID,
		DependsOnID: dependsOnID,
	}
	_, err := c.call(protocol.ActionRemoveDependency, p)
	return err
}

func (c *Client) GetDependencies(taskID string) (*protocol.DependenciesData, error) {
	p := protocol.GetDependenciesPayload{
		TaskID: taskID,
	}
	resp, err := c.call(protocol.ActionGetDependencies, p)
	if err != nil {
		return nil, err
	}
	var data protocol.DependenciesData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *Client) AttachGit(taskID, sha, msg, diffStat, diffContent string, markDone bool) (*domain.Task, error) {
	return c.AttachGitWithForce(taskID, sha, msg, diffStat, diffContent, markDone, false)
}

func (c *Client) AttachGitWithForce(taskID, sha, msg, diffStat, diffContent string, markDone, force bool) (*domain.Task, error) {
	p := protocol.AttachGitPayload{
		TaskID:      taskID,
		CommitSHA:   sha,
		CommitMsg:   msg,
		DiffStat:    diffStat,
		DiffContent: diffContent,
		MarkDone:    markDone,
		Force:       force,
	}
	resp, err := c.call(protocol.ActionAttachGit, p)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) StreamEvents(handler func(protocol.Event)) error {
	if c.conn == nil {
		if err := c.Connect(); err != nil {
			return err
		}
	}

	req := protocol.Request{
		ID:     uuid.New().String(),
		Action: protocol.ActionStreamEvents,
	}
	if err := c.enc.Encode(req); err != nil {
		return err
	}

	// Read initial ack
	var ack protocol.Response
	if err := c.dec.Decode(&ack); err != nil {
		return err
	}

	for {
		var resp protocol.Response
		if err := c.dec.Decode(&resp); err != nil {
			if err == io.EOF || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") {
				return nil
			}
			return err
		}

		if resp.Success && len(resp.Data) > 0 {
			var ev protocol.Event
			if err := json.Unmarshal(resp.Data, &ev); err == nil {
				handler(ev)
			}
		}
	}
}

func (c *Client) ShutdownDaemon() error {
	if c.conn == nil {
		conn, err := c.dial()
		if err != nil {
			return err
		}
		c.conn = conn
		c.enc = json.NewEncoder(conn)
		c.dec = json.NewDecoder(conn)
	}
	_, err := c.call(protocol.ActionShutdown, nil)
	return err
}

func (c *Client) CreateBackup(name string, maxSnapshots int) (string, error) {
	resp, err := c.call(protocol.ActionBackupCreate, protocol.BackupCreatePayload{
		Name:         name,
		MaxSnapshots: maxSnapshots,
	})
	if err != nil {
		return "", err
	}
	var res map[string]string
	if err := json.Unmarshal(resp.Data, &res); err != nil {
		return "", err
	}
	return res["path"], nil
}

func (c *Client) ListBackups() ([]protocol.BackupInfo, error) {
	resp, err := c.call(protocol.ActionBackupList, nil)
	if err != nil {
		return nil, err
	}
	var backups []protocol.BackupInfo
	if err := json.Unmarshal(resp.Data, &backups); err != nil {
		return nil, err
	}
	return backups, nil
}

func (c *Client) VerifyBackup(target string) (string, error) {
	resp, err := c.call(protocol.ActionBackupVerify, protocol.BackupVerifyPayload{
		Target: target,
	})
	if err != nil {
		return "", err
	}
	var res map[string]string
	if err := json.Unmarshal(resp.Data, &res); err != nil {
		return "", err
	}
	return res["message"], nil
}

func (c *Client) RestoreBackup(backupPath string) (string, error) {
	resp, err := c.call(protocol.ActionBackupRestore, protocol.BackupRestorePayload{
		BackupPath: backupPath,
	})
	if err != nil {
		return "", err
	}
	var res map[string]string
	if err := json.Unmarshal(resp.Data, &res); err != nil {
		return "", err
	}
	return res["message"], nil
}

// callInto performs a request and decodes the response data into out (if non-nil).
func (c *Client) callInto(action protocol.Action, payload, out any) error {
	resp, err := c.call(action, payload)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Data, out)
}

// ListTopics returns all topics of the client's scope in tree order.
func (c *Client) ListTopics() ([]*domain.Topic, error) {
	var topics []*domain.Topic
	if err := c.callInto(protocol.ActionListTopics, struct{}{}, &topics); err != nil {
		return nil, err
	}
	return topics, nil
}

// GetTopic resolves a topic by path, unique slug or numeric ID.
func (c *Client) GetTopic(ref string) (*domain.Topic, error) {
	var tp domain.Topic
	if err := c.callInto(protocol.ActionGetTopic, protocol.TopicRefPayload{Ref: ref}, &tp); err != nil {
		return nil, err
	}
	return &tp, nil
}

func (c *Client) CreateTopic(p protocol.CreateTopicPayload) (*domain.Topic, error) {
	var tp domain.Topic
	if err := c.callInto(protocol.ActionCreateTopic, p, &tp); err != nil {
		return nil, err
	}
	return &tp, nil
}

func (c *Client) UpdateTopic(p protocol.UpdateTopicPayload) (*domain.Topic, error) {
	var tp domain.Topic
	if err := c.callInto(protocol.ActionUpdateTopic, p, &tp); err != nil {
		return nil, err
	}
	return &tp, nil
}

// MoveTopic re-parents a topic; parent "" moves it to the root.
func (c *Client) MoveTopic(ref, parent string) (*domain.Topic, error) {
	var tp domain.Topic
	if err := c.callInto(protocol.ActionMoveTopic, protocol.MoveTopicPayload{Ref: ref, Parent: parent}, &tp); err != nil {
		return nil, err
	}
	return &tp, nil
}

// DeleteTopic deletes a topic, moving its contents to moveTo when it is not empty.
func (c *Client) DeleteTopic(ref, moveTo string) error {
	return c.callInto(protocol.ActionDeleteTopic, protocol.DeleteTopicPayload{Ref: ref, MoveTo: moveTo}, nil)
}

// MoveItems moves tasks/posts into a topic ("unfiled" clears it).
func (c *Client) MoveItems(ids []string, topic, actor string) ([]*domain.Task, error) {
	var moved []*domain.Task
	if err := c.callInto(protocol.ActionMoveItems, protocol.MoveItemsPayload{IDs: ids, Topic: topic, Actor: actor}, &moved); err != nil {
		return nil, err
	}
	return moved, nil
}

// PromotePost converts a post into a task in topic, returning the new task and the resolved post.
func (c *Client) PromotePost(id, topic, author string) (*protocol.PromotePostResult, error) {
	var res protocol.PromotePostResult
	if err := c.callInto(protocol.ActionPromotePost, protocol.PromotePostPayload{ID: id, Topic: topic, Author: author}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// nid canonicalises a forgiving item reference ("t15", "15") for the client's scope.
// Unparseable input is passed through unchanged so the daemon reports "not found".
func (c *Client) nid(id string) string {
	if n, err := domain.NormalizeID(id, c.Scope); err == nil {
		return n
	}
	return id
}

func (c *Client) nids(ids []string) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = c.nid(id)
	}
	return out
}

// normalizePayload canonicalises every item ID carried by a request payload so all
// callers (CLI, TUI, scripts) accept t15 / T15 / t-15 / 15 interchangeably.
func (c *Client) normalizePayload(payload any) any {
	switch p := payload.(type) {
	case protocol.GetTaskPayload:
		p.ID = c.nid(p.ID)
		return p
	case protocol.UpdateTaskPayload:
		p.ID = c.nid(p.ID)
		return p
	case protocol.ClaimTaskPayload:
		p.ID = c.nid(p.ID)
		return p
	case protocol.DeleteTaskPayload:
		p.ID = c.nid(p.ID)
		return p
	case protocol.RestoreTaskPayload:
		if p.ID != "" && p.ID != "latest" {
			p.ID = c.nid(p.ID)
		}
		return p
	case protocol.ArchiveTaskPayload:
		p.ID = c.nid(p.ID)
		return p
	case protocol.UnarchiveTaskPayload:
		p.ID = c.nid(p.ID)
		return p
	case protocol.OrphanTaskPayload:
		p.ID = c.nid(p.ID)
		return p
	case protocol.TransferTaskPayload:
		p.ID = c.nid(p.ID)
		return p
	case protocol.AddDependencyPayload:
		p.TaskID, p.DependsOnID = c.nid(p.TaskID), c.nid(p.DependsOnID)
		return p
	case protocol.RemoveDependencyPayload:
		p.TaskID, p.DependsOnID = c.nid(p.TaskID), c.nid(p.DependsOnID)
		return p
	case protocol.GetDependenciesPayload:
		p.TaskID = c.nid(p.TaskID)
		return p
	case protocol.AttachGitPayload:
		p.TaskID = c.nid(p.TaskID)
		return p
	case protocol.CreateTaskPayload:
		p.DependsOn = c.nids(p.DependsOn)
		return p
	case protocol.ListTasksPayload:
		if p.DependsOn != "" {
			p.DependsOn = c.nid(p.DependsOn)
		}
		if p.Blocks != "" {
			p.Blocks = c.nid(p.Blocks)
		}
		if p.LinksTo != "" && !strings.HasPrefix(p.LinksTo, "@") {
			p.LinksTo = c.nid(p.LinksTo)
		}
		return p
	case protocol.MoveItemsPayload:
		p.IDs = c.nids(p.IDs)
		return p
	case protocol.PromotePostPayload:
		p.ID = c.nid(p.ID)
		return p
	}
	return payload
}
