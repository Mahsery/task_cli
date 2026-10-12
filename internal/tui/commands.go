package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"task_cli/internal/domain"
	"task_cli/internal/git"
	"task_cli/internal/protocol"
)

// reloadErrMsg carries a fresh task list together with an operation error, so the
// list reflects whatever partially succeeded while the failure is still surfaced.
type reloadErrMsg struct {
	tasks []*domain.Task
	err   error
}

// listFilter is the single source of truth for the TUI's current list query.
func (m Model) listFilter() domain.TaskFilter {
	return domain.TaskFilter{
		Scope:           m.scope,
		RepoPath:        m.repoPath,
		Search:          m.filterText,
		IncludeArchived: m.showArchived,
		Topic:           m.topicPath,
	}
}

// reloadAfter re-fetches the task list using the current view filter and reports opErr, if any.
// A failed reload never replaces the visible list with an empty one.
func (m Model) reloadAfter(opErr error) tea.Msg {
	tasks, err := m.client.ListTasks(m.listFilter())
	if err != nil {
		return errMsg(errors.Join(opErr, fmt.Errorf("reload failed: %w", err)))
	}
	if opErr != nil {
		return reloadErrMsg{tasks: tasks, err: opErr}
	}
	return reloadMsg(tasks)
}

// runBatch applies op to every id and aggregates failures into one readable error.
func runBatch(verb string, ids []string, op func(id string) error) error {
	var failed []string
	for _, id := range ids {
		if err := op(id); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", id, err))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("%s failed for %d/%d task(s): %s", verb, len(failed), len(ids), strings.Join(failed, "; "))
}

// latestCommit returns git evidence for the current repository, if any.
func (m Model) latestCommit() (sha, msg, stat, diff string) {
	if m.scope != domain.ScopeLocal || m.repoPath == "" {
		return "", "", "", ""
	}
	commit, err := git.GetLatestCommit(m.repoPath)
	if err != nil {
		return "", "", "", ""
	}
	return commit.ShortSHA, commit.Subject, commit.DiffStat, commit.DiffContent
}

func (m Model) fetchTaskDetailCmd(id string) tea.Cmd {
	return func() tea.Msg {
		task, err := m.client.GetTask(id)
		if err != nil {
			return nil
		}
		return taskDetailMsg(task)
	}
}

func (m Model) fetchTasksCmd() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		tasks, err := m.client.ListTasks(m.listFilter())
		if err != nil {
			return errMsg(err)
		}
		return reloadMsg(tasks)
	}
}

func (m Model) linkTaskCmd(taskID, prereqID string) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.AddDependency(taskID, prereqID); err != nil {
			return errMsg(fmt.Errorf("failed to link %s -> %s: %w", taskID, prereqID, err))
		}
		return m.reloadAfter(nil)
	}
}

func (m Model) deleteTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.DeleteTask(taskID); err != nil {
			return errMsg(err)
		}
		return m.reloadAfter(nil)
	}
}

func (m Model) reopenTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		st := domain.StatusOpen
		note := "Reopened task"
		if _, err := m.client.UpdateTask(protocol.UpdateTaskPayload{ID: taskID, Status: &st, AddNote: &note}); err != nil {
			return errMsg(err)
		}
		return m.reloadAfter(nil)
	}
}

func (m Model) updateStatusCmd(taskID string, newStatus domain.Status) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		if _, err := m.client.UpdateTask(protocol.UpdateTaskPayload{ID: taskID, Status: &newStatus}); err != nil {
			return errMsg(err)
		}
		return m.reloadAfter(nil)
	}
}

func (m Model) markDoneCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		sha, msg, stat, diff := m.latestCommit()
		if _, err := m.client.AttachGit(taskID, sha, msg, stat, diff, true); err != nil {
			return errMsg(err)
		}
		return m.reloadAfter(nil)
	}
}

func (m Model) claimTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		if _, err := m.client.ClaimTask(taskID, m.resolveActor(), false); err != nil {
			return errMsg(err)
		}
		return m.reloadAfter(nil)
	}
}

func (m Model) getSelectedIDs() []string {
	var ids []string
	for _, t := range m.tasks {
		if m.selected[t.ID] {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

func (m Model) batchDoneCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		sha, msg, stat, diff := m.latestCommit()
		err := runBatch("done", ids, func(id string) error {
			if sha != "" {
				_, err := m.client.AttachGit(id, sha, msg, stat, diff, true)
				return err
			}
			st := domain.StatusDone
			note := "Completed task"
			_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{ID: id, Status: &st, AddNote: &note})
			return err
		})
		return m.reloadAfter(err)
	}
}

func (m Model) batchStatusCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		// Toggle: if every selected task is in progress, reopen them; otherwise start them all.
		allInProg := true
		for _, id := range ids {
			for _, t := range m.tasks {
				if t.ID == id && t.Status != domain.StatusInProgress {
					allInProg = false
					break
				}
			}
			if !allInProg {
				break
			}
		}
		targetStatus := domain.StatusInProgress
		if allInProg {
			targetStatus = domain.StatusOpen
		}
		err := runBatch("status change", ids, func(id string) error {
			st := targetStatus
			_, err := m.client.UpdateTask(protocol.UpdateTaskPayload{ID: id, Status: &st})
			return err
		})
		return m.reloadAfter(err)
	}
}

func (m Model) batchClaimCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		actor := m.resolveActor()
		err := runBatch("claim", ids, func(id string) error {
			_, err := m.client.ClaimTask(id, actor, false)
			return err
		})
		return m.reloadAfter(err)
	}
}

func (m Model) batchDeleteCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := runBatch("delete", ids, m.client.DeleteTask)
		return m.reloadAfter(err)
	}
}

func (m Model) archiveTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		if _, err := m.client.ArchiveTask(taskID, m.resolveActor(), "Archived via TUI"); err != nil {
			return errMsg(err)
		}
		return m.reloadAfter(nil)
	}
}

func (m Model) unarchiveTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		if _, err := m.client.UnarchiveTask(taskID, m.resolveActor(), "Unarchived via TUI"); err != nil {
			return errMsg(err)
		}
		return m.reloadAfter(nil)
	}
}

func (m Model) batchArchiveCmd(ids []string, archive bool) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		actor := m.resolveActor()
		verb := "unarchive"
		if archive {
			verb = "archive"
		}
		err := runBatch(verb, ids, func(id string) error {
			var err error
			if archive {
				_, err = m.client.ArchiveTask(id, actor, "Archived via TUI batch")
			} else {
				_, err = m.client.UnarchiveTask(id, actor, "Unarchived via TUI batch")
			}
			return err
		})
		return m.reloadAfter(err)
	}
}

// resolveActor returns the configured actor identity (TK_ACTOR / config), falling back to the git author.
func (m Model) resolveActor() string {
	if m.client != nil {
		if a := m.client.Actor(); a != "" {
			return a
		}
	}
	return git.GetAuthor()
}
