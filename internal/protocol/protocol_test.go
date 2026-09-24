package protocol

import (
	"encoding/json"
	"testing"

	"task_cli/internal/domain"
)

func TestRequestResponseJSON(t *testing.T) {
	createPayload := CreateTaskPayload{
		Title:       "Test Protocol",
		Description: "Protocol testing",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityHigh,
		Tags:        []string{"net", "ipc"},
		Author:      "Tester",
		Assignee:    "Developer",
	}

	rawPayload, err := json.Marshal(createPayload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	req := Request{
		ID:       "req-1",
		Action:   ActionCreateTask,
		Payload:  rawPayload,
		Scope:    "local",
		RepoPath: "/test/repo",
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request failed: %v", err)
	}

	var decodedReq Request
	if err := json.Unmarshal(reqData, &decodedReq); err != nil {
		t.Fatalf("unmarshal request failed: %v", err)
	}

	if decodedReq.Action != ActionCreateTask {
		t.Errorf("expected action create_task, got %s", decodedReq.Action)
	}
	if decodedReq.Scope != "local" {
		t.Errorf("expected scope local, got %s", decodedReq.Scope)
	}

	var decodedPayload CreateTaskPayload
	if err := json.Unmarshal(decodedReq.Payload, &decodedPayload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	if decodedPayload.Assignee != "Developer" {
		t.Errorf("expected assignee Developer, got %s", decodedPayload.Assignee)
	}

	// Test Response
	resp := Response{
		ID:      "req-1",
		Success: true,
		Data:    reqData,
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response failed: %v", err)
	}

	var decodedResp Response
	if err := json.Unmarshal(respBytes, &decodedResp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}

	if !decodedResp.Success {
		t.Errorf("expected success true, got false")
	}
}

func TestListTasksPayloadAssignee(t *testing.T) {
	payload := ListTasksPayload{
		Scope:    "local",
		RepoPath: "/test/repo",
		Assignee: "john",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ListTasksPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Assignee != "john" {
		t.Errorf("expected assignee john, got %s", decoded.Assignee)
	}
}

func BenchmarkProtocol_RequestEncodeDecode(b *testing.B) {
	createPayload := CreateTaskPayload{
		Title:       "Test Protocol",
		Description: "Protocol testing",
		Status:      domain.StatusOpen,
		Priority:    domain.PriorityHigh,
		Tags:        []string{"net", "ipc"},
		Author:      "Tester",
		Assignee:    "Developer",
	}
	rawPayload, _ := json.Marshal(createPayload)
	req := Request{
		ID:       "req-1",
		Action:   ActionCreateTask,
		Payload:  rawPayload,
		Scope:    "local",
		RepoPath: "/test/repo",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(req)
		var decoded Request
		_ = json.Unmarshal(data, &decoded)
		var p CreateTaskPayload
		_ = json.Unmarshal(decoded.Payload, &p)
	}
}

func BenchmarkProtocol_RoundTrip500Tasks(b *testing.B) {
	tasks := make([]*domain.Task, 500)
	for i := 0; i < 500; i++ {
		tasks[i] = &domain.Task{
			ID:          "T-1",
			Scope:       "local",
			RepoPath:    "/repo/test",
			Title:       "Task Title",
			Description: "Task Description for benchmarking JSON",
			Status:      domain.StatusOpen,
			Priority:    domain.PriorityNormal,
			Tags:        []string{"bench", "json"},
			Author:      "Dev",
			Assignee:    "Worker",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Server side: Marshal tasks -> wrap in Response -> Marshal Response
		taskData, _ := json.Marshal(tasks)
		resp := Response{
			ID:      "req-1",
			Success: true,
			Data:    taskData,
		}
		respBytes, _ := json.Marshal(resp)

		// Client side: Unmarshal Response -> Unmarshal tasks
		var clientResp Response
		_ = json.Unmarshal(respBytes, &clientResp)
		var clientTasks []*domain.Task
		_ = json.Unmarshal(clientResp.Data, &clientTasks)
	}
}
