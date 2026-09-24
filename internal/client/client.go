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
	defer conn.Close()

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
	p := protocol.ListTasksPayload{
		Scope:    scope,
		RepoPath: repoPath,
		Status:   filter.Status,
		Priority: filter.Priority,
		Tag:      filter.Tag,
		Assignee: filter.Assignee,
		Search:   filter.Search,
		Sort:     filter.Sort,
		Limit:    filter.Limit,
	}
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

func (c *Client) DeleteTask(id string) error {
	_, err := c.call(protocol.ActionDeleteTask, protocol.DeleteTaskPayload{ID: id})
	return err
}

func (c *Client) AttachGit(taskID string, sha, msg, diffStat, diffContent string, markDone bool) (*domain.Task, error) {
	p := protocol.AttachGitPayload{
		TaskID:      taskID,
		CommitSHA:   sha,
		CommitMsg:   msg,
		DiffStat:    diffStat,
		DiffContent: diffContent,
		MarkDone:    markDone,
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
