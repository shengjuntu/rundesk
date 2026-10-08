package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func collabConfig(t *testing.T, m *Manager, agents ...CollaborationAgent) {
	t.Helper()
	_, e := m.saveCollaborationConfig(CollaborationConfig{Agents: agents})
	if e != nil {
		t.Fatal(e)
	}
}
func localCollabAgent(m *Manager, id string) CollaborationAgent {
	return CollaborationAgent{ID: id, Name: id, Description: "research", InstanceID: DefaultInstance, WorkspaceID: m.Workspaces()[0].ID}
}
func TestCollaborationAutomaticDelegationAndRecovery(t *testing.T) {
	m := testManager(t)
	var mu sync.Mutex
	calls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				Message a2aMessage `json:"message"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		text := `{"action":"delegate","summary":"核验","tasks":[{"agent":"worker","text":"hello"}]}`
		if n > 1 {
			text = `{"action":"finish","summary":"核验完成","tasks":[]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": a2aMessage{Kind: "message", Role: "agent", MessageID: fmt.Sprint(n), Parts: []a2aPart{{Kind: "text", Text: text}}}})
	}))
	defer remote.Close()
	collabConfig(t, m, CollaborationAgent{ID: "lead", Name: "lead", Description: "coordinate", Endpoint: remote.URL}, localCollabAgent(m, "worker"))
	c, e := m.createCollaboration(CollaborationInput{Goal: "核查", Leader: "lead", Agents: []string{"worker"}, Mode: "p2p"})
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(22 * time.Second)
	for time.Now().Before(deadline) {
		m.collabMu.Lock()
		c, e = m.collaboration(c.ID)
		m.collabMu.Unlock()
		if e != nil {
			t.Fatal(e)
		}
		if c.Status == "completed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if c.Status != "completed" || len(c.Work) != 3 || c.Rounds != 2 {
		t.Fatalf("unexpected workflow %+v", c)
	}
	if c.Work[1].TaskID == "" || c.Work[1].Result == "" {
		t.Fatal("no actual queued execution/result")
	}
	// Replaying the durable handoff must not create another execution.
	a, _ := c.agent("worker")
	before := len(m.Tasks())
	if _, e = m.enqueueCollaboration(c, c.Work[1], a); e != nil {
		t.Fatal(e)
	}
	if len(m.Tasks()) != before {
		t.Fatal("duplicate task on replay")
	}
}
func TestCollaborationA2AIdempotencyAndIsolation(t *testing.T) {
	m := testManager(t)
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	wid := m.Workspaces()[0].ID
	a, credential, key := setupKey(t, m, "a2a-test", wid, "read", "run")
	_, otherKey, e := m.CreateApplicationKey(a.AppID, KeyInput{Name: "other", WorkspaceIDs: []string{wid}, Scopes: []string{"read", "run"}})
	if e != nil {
		t.Fatal(e)
	}
	collabConfig(t, m, CollaborationAgent{ID: "worker", Name: "worker", Description: "test", InstanceID: a.InstanceID, WorkspaceID: wid})
	h := NewHandler(m, "admin-test", true)
	data := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"kind":"message","role":"user","messageId":"stable-id","parts":[{"kind":"text","text":"hello"}]},"configuration":{"blocking":false}}}`
	call := func(body, token string) map[string]any {
		r := appRequest(h, "POST", "/api/v1/a2a/worker", body, token, "")
		if r.Code != 200 {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
		return object(t, r)
	}
	first := call(data, key)
	if first["error"] != nil {
		t.Fatal(first)
	}
	if len(m.Tasks()) != 1 || m.Tasks()[0].SubmittingKeyID != credential.ID {
		t.Fatal("A2A lost submitting credential")
	}
	second := call(data, key)
	id := first["result"].(map[string]any)["id"].(string)
	if second["result"].(map[string]any)["id"] != id || len(m.Tasks()) != 1 {
		t.Fatal("duplicate")
	}
	if call(strings.Replace(data, "hello", "changed", 1), key)["error"] == nil {
		t.Fatal("conflicting reuse allowed")
	}
	get := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":%q}}`, id)
	if call(get, otherKey)["error"].(map[string]any)["code"] != float64(-32001) {
		t.Fatal("cross-key read")
	}
	if r := appRequest(h, "GET", "/api/v1/collaborations", "", key, ""); r.Code != 403 {
		t.Fatal("app accessed administrative collaboration")
	}
	cancel := strings.Replace(get, "tasks/get", "tasks/cancel", 1)
	if call(cancel, key)["result"].(map[string]any)["status"].(map[string]any)["state"] != "canceled" {
		t.Fatal("cancel")
	}
	card := appRequest(h, "GET", "/api/v1/a2a/worker/agent-card.json", "", key, "")
	if card.Code != 200 || object(t, card)["protocolVersion"] != "0.3.0" {
		t.Fatal(card.Body.String())
	}
}
func TestCollaborationGiteaMirrorAndInboundDedup(t *testing.T) {
	m := testManager(t)
	m.collabMu.Lock()
	defer m.collabMu.Unlock()
	t.Setenv("TEST_GITEA_TOKEN", "secret")
	comments := []boardComment{}
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing credential")
		}
		if strings.HasSuffix(r.URL.Path, "/comments") {
			if r.Method == "POST" {
				var x struct{ Body string }
				json.NewDecoder(r.Body).Decode(&x)
				comments = append(comments, boardComment{ID: int64(len(comments) + 1), Body: x.Body})
				json.NewEncoder(w).Encode(comments[len(comments)-1])
			} else {
				json.NewEncoder(w).Encode(comments)
			}
			return
		}
		if r.Method == "POST" {
			creates++
			json.NewEncoder(w).Encode(map[string]any{"number": 1, "html_url": "http://example.test/o/r/issues/1"})
			return
		}
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()
	c := Collaboration{ID: "board-test", Goal: "research", Status: "running", Board: BlackboardConfig{URL: server.URL, Owner: "o", Repo: "r", TokenEnv: "TEST_GITEA_TOKEN"}}
	c.entry("goal", "user", "goal")
	if e := m.syncBlackboard(&c); e != nil {
		t.Fatal(e)
	}
	if creates != 1 || len(comments) != 1 {
		t.Fatal("missing issue/comment")
	}
	// Lost local acknowledgement: find the marker instead of duplicating the comment.
	c.Entries[0].Mirrored = false
	c.NextSync = ""
	if e := m.syncBlackboard(&c); e != nil {
		t.Fatal(e)
	}
	if len(comments) != 1 {
		t.Fatal("duplicate mirrored event")
	}
	comments = append(comments, boardComment{ID: 2, Body: "new evidence"})
	c.NextSync = ""
	if e := m.syncBlackboard(&c); e != nil {
		t.Fatal(e)
	}
	if len(c.Entries) != 2 || !c.Wake {
		t.Fatal("no incoming wake")
	}
	c.NextSync = ""
	m.syncBlackboard(&c)
	if len(c.Entries) != 2 {
		t.Fatal("duplicate incoming comment")
	}
	uncertain := Collaboration{ID: "unknown", Board: c.Board, BoardAttempted: true}
	if e := m.syncBlackboard(&uncertain); e == nil {
		t.Fatal("ambiguous create retried")
	}
	if creates != 1 {
		t.Fatal("recreated issue")
	}
}
func TestCollaborationDecisionBoundsPauseAndAmbiguousSend(t *testing.T) {
	m := testManager(t)
	m.collabMu.Lock()
	defer m.collabMu.Unlock()
	c := Collaboration{Leader: "lead", Status: "running", Mode: "p2p", MaxRounds: 1, Rounds: 1, Wake: true, Agents: []CollaborationAgent{{ID: "lead"}, {ID: "worker"}}}
	if e := m.applyCollaborationDecision(&c, CollaborationWork{Agent: "lead", Result: `{"action":"delegate","tasks":[{"agent":"stranger","text":"do it"}]}`}, true); e == nil {
		t.Fatal("unknown agent allowed")
	}
	if len(c.Work) != 0 {
		t.Fatal("partially committed decision")
	}
	if e := m.advanceCollaboration(&c); e != nil {
		t.Fatal(e)
	}
	if c.Status != "waiting" {
		t.Fatal("unbounded supervisor")
	}
	c.Status = "paused"
	c.Work = []CollaborationWork{{ID: "interrupted", Agent: "worker", Status: "sending"}}
	c.Agents[1].Endpoint = "http://127.0.0.1:1"
	if e := m.advanceCollaboration(&c); e != nil {
		t.Fatal(e)
	}
	if c.Work[0].Status != "unconfirmed" {
		t.Fatal("restart replayed send")
	}
}

func TestCollaborationRestartBetweenEnqueueAndLinkThenCancel(t *testing.T) {
	dir := t.TempDir()
	m, e := New(dir, "", true)
	if e != nil {
		t.Fatal(e)
	}
	q := m.Queue()
	q.Paused = true
	m.SaveQueue(q)
	a := localCollabAgent(m, "worker")
	collabConfig(t, m, a)
	c, e := m.createCollaboration(CollaborationInput{Goal: "restart", Leader: "worker", Mode: "p2p"})
	if e != nil {
		m.Close()
		t.Fatal(e)
	}
	m.collabMu.Lock()
	c.Wake = false
	c.Work = []CollaborationWork{{ID: "before-link", Agent: a.ID, Prompt: "hello", Status: "queued"}}
	m.saveCollaboration(&c)
	task, e := m.enqueueCollaboration(c, c.Work[0], a)
	m.collabMu.Unlock()
	if e != nil {
		m.Close()
		t.Fatal(e)
	}
	m.Close()
	m, e = New(dir, "", true)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	m.collabMu.Lock()
	defer m.collabMu.Unlock()
	c, e = m.collaboration(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	c.Status = "canceling"
	if e = m.advanceCollaboration(&c); e != nil {
		t.Fatal(e)
	}
	if c.Status != "canceled" {
		t.Fatal(c.Status)
	}
	task, e = m.Task(task.ID)
	if e != nil || task.Status != "canceled" {
		t.Fatal("orphan queue task escaped cancellation", task, e)
	}
}

func TestCollaborationP2PReturnsToRequester(t *testing.T) {
	m := testManager(t)
	m.collabMu.Lock()
	defer m.collabMu.Unlock()
	c := Collaboration{Leader: "leader", Status: "running", Mode: "p2p", MaxRounds: 8, Agents: []CollaborationAgent{{ID: "leader"}, {ID: "a"}, {ID: "b"}}, Work: []CollaborationWork{{ID: "parent", Agent: "a", Status: "completed", Handled: true, Prompt: "original"}, {ID: "child", Parent: "parent", Agent: "b", Status: "completed", Result: "evidence"}}}
	if e := m.advanceCollaboration(&c); e != nil {
		t.Fatal(e)
	}
	if len(c.Work) != 3 || c.Work[2].Agent != "a" || !c.Work[0].Returned || !strings.Contains(c.Work[2].Prompt, "evidence") {
		t.Fatalf("missing P2P reply %+v", c.Work)
	}
}
