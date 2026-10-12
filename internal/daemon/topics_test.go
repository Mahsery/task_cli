package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"task_cli/internal/config"
	"task_cli/internal/domain"
	"task_cli/internal/protocol"
)

func newTopicTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{GlobalDir: tmpDir, SocketPath: filepath.Join(tmpDir, "tk.sock")}
	srv := NewServer(cfg)
	t.Cleanup(func() {
		srv.mu.Lock()
		for _, db := range srv.dbs {
			_ = db.Close()
		}
		srv.mu.Unlock()
	})
	return srv, filepath.Join(tmpDir, "repo")
}

func call[T any](t *testing.T, srv *Server, repo string, action protocol.Action, payload any) (T, protocol.Response) {
	t.Helper()
	var out T
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp := srv.dispatch(protocol.Request{ID: "r", Action: action, Payload: raw, Scope: "local", RepoPath: repo})
	if resp.Success && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			t.Fatalf("decode %s: %v", action, err)
		}
	}
	return out, resp
}

func TestDaemonTopicsMovePromoteFlow(t *testing.T) {
	srv, repo := newTopicTestServer(t)

	events := srv.pubsub.Subscribe()
	defer srv.pubsub.Unsubscribe(events)

	if _, r := call[domain.Topic](t, srv, repo, protocol.ActionCreateTopic, protocol.CreateTopicPayload{Title: "Game", Kind: domain.TopicGroup}); !r.Success {
		t.Fatal(r.Error)
	}
	hero, r := call[domain.Topic](t, srv, repo, protocol.ActionCreateTopic, protocol.CreateTopicPayload{Title: "Hero System", Parent: "game"})
	if !r.Success || hero.Path != "game/hero-system" || hero.Kind != domain.TopicTasks {
		t.Fatalf("create hero: %+v %s", hero, r.Error)
	}
	if _, r := call[domain.Topic](t, srv, repo, protocol.ActionCreateTopic, protocol.CreateTopicPayload{Title: "Bug Reports", Kind: domain.TopicBoard}); !r.Success {
		t.Fatal(r.Error)
	}

	select {
	case ev := <-events:
		if ev.Type != protocol.EventTopicsChanged {
			t.Errorf("expected topics_changed event, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("no event published for topic creation")
	}

	topics, r := call[[]*domain.Topic](t, srv, repo, protocol.ActionListTopics, struct{}{})
	if !r.Success || len(topics) != 3 {
		t.Fatalf("list topics: %d %s", len(topics), r.Error)
	}

	post, r := call[domain.Task](t, srv, repo, protocol.ActionCreateTask, protocol.CreateTaskPayload{Title: "Crash", Kind: domain.KindPost, Topic: "bug-reports"})
	if !r.Success || post.ID != "P-1" || post.TopicPath != "bug-reports" {
		t.Fatalf("create post: %+v %s", post, r.Error)
	}
	task, r := call[domain.Task](t, srv, repo, protocol.ActionCreateTask, protocol.CreateTaskPayload{Title: "Stats", Description: "see [[P-1]]"})
	if !r.Success || task.ID != "T-1" {
		t.Fatalf("create task: %+v %s", task, r.Error)
	}

	moved, r := call[[]*domain.Task](t, srv, repo, protocol.ActionMoveItems, protocol.MoveItemsPayload{IDs: []string{"T-1"}, Topic: "hero-system", Actor: "me"})
	if !r.Success || len(moved) != 1 || moved[0].TopicPath != "game/hero-system" {
		t.Fatalf("move: %+v %s", moved, r.Error)
	}
	if _, r := call[[]*domain.Task](t, srv, repo, protocol.ActionMoveItems, protocol.MoveItemsPayload{IDs: []string{"P-1"}, Topic: "hero-system"}); r.Success {
		t.Error("moving a post into a tasks topic must fail over IPC too")
	}

	listed, r := call[[]*domain.Task](t, srv, repo, protocol.ActionListTasks, protocol.ListTasksPayload{Topic: "game", TopicRecursive: true})
	if !r.Success || len(listed) != 1 || listed[0].ID != "T-1" {
		t.Fatalf("recursive topic list: %+v %s", listed, r.Error)
	}

	res, r := call[protocol.PromotePostResult](t, srv, repo, protocol.ActionPromotePost, protocol.PromotePostPayload{ID: "P-1", Topic: "game/hero-system", Author: "me"})
	if !r.Success || res.Task == nil || res.Task.ID != "T-2" || res.Post.Status != domain.StatusDone {
		t.Fatalf("promote: %+v %s", res, r.Error)
	}
	gotPost, _ := call[domain.Task](t, srv, repo, protocol.ActionGetTask, protocol.GetTaskPayload{ID: "P-1"})
	want := map[string]bool{"T-1": true, "T-2": true}
	if len(gotPost.Backlinks) != 2 || !want[gotPost.Backlinks[0]] || !want[gotPost.Backlinks[1]] {
		t.Errorf("post backlinks = %v, want T-1 and T-2", gotPost.Backlinks)
	}

	if _, r := call[json.RawMessage](t, srv, repo, protocol.ActionDeleteTopic, protocol.DeleteTopicPayload{Ref: "game"}); r.Success {
		t.Error("deleting a non-empty group without destination must fail")
	}
	if _, r := call[json.RawMessage](t, srv, repo, protocol.ActionDeleteTopic, protocol.DeleteTopicPayload{Ref: "game", MoveTo: "unfiled"}); !r.Success {
		t.Fatal(r.Error)
	}
	still, _ := call[domain.Topic](t, srv, repo, protocol.ActionGetTopic, protocol.TopicRefPayload{Ref: "hero-system"})
	if still.Path != "hero-system" {
		t.Errorf("sub-topic should move to root when parent deleted to unfiled, got %q", still.Path)
	}
}
