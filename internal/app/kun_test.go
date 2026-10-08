package app

import (
	"context"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func buildKunTestBinary(t *testing.T) string {
	t.Helper()
	name := "kun"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-buildvcs=false", "-o", binary, "./cmd/kun")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build Kun: %v\n%s", err, out)
	}
	return binary
}
func TestKunProcessConversationAndDebugHTTP(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("KUN_TEST_API_KEY", "do-not-persist-this-key")
	m, err := New(t.TempDir(), "missing-codex-for-kun-test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Kun = buildKunTestBinary(t)
	w := m.Workspaces()[0]
	old, err := m.CreateSession(w.ID, "old", "")
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	var sid string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer do-not-persist-this-key" {
			t.Error("credential missing")
		}
		w.Header().Set("Content-Type", "application/json")
		if count.Add(1) == 1 {
			call := p.ToolCall{ID: "first-write", Type: "function", Function: p.Function{Name: "write_file", Arguments: string(p.JSON(map[string]string{"path": "outputs/" + sid + "/result.txt", "content": "verified"}))}}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", ToolCalls: []p.ToolCall{call}}, "finish_reason": "tool_calls"}}})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": p.Message{Role: "assistant", Content: "Saved result.txt"}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 24}})
		}
	}))
	defer provider.Close()
	i, _ := m.Instance()
	cfg := p.Config{Kind: "kun", Endpoint: provider.URL + "/v1", Model: "fixture", APIKeyEnv: "KUN_TEST_API_KEY", MaxSteps: 4, TimeoutSeconds: 5, AllowWrite: true, PauseBeforeModel: true}
	if _, err = m.SetAgentRuntime(i.ID, i.Revision, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(old.ID, Input{Text: "wrong backend"}); err == nil {
		t.Fatal("old Codex session silently migrated")
	}
	session, err := m.CreateSession(w.ID, "Kun test", "")
	if err != nil {
		t.Fatal(err)
	}
	sid = session.ID
	if session.RuntimeKind != "kun" || session.Model != "fixture" {
		t.Fatal(session)
	}
	handler := NewHandler(m, "", true)
	if _, err = m.Start(sid, Input{Text: "write the result"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, sid, "waiting")
	client, err := m.kunClient(sid)
	if err != nil {
		t.Fatal(err)
	}
	if client.PID() == os.Getpid() {
		t.Fatal("Kun is not a separate process")
	}
	var state p.State
	get := func() p.State {
		t.Helper()
		r := request(handler, "GET", "/api/sessions/"+sid+"/kun/state", "", "", "")
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		var s p.State
		if e := json.Unmarshal(r.Body.Bytes(), &s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	control := func(s p.State, operation string) {
		t.Helper()
		body := p.JSON(p.Control{RequestID: fmt.Sprintf("test-command-%d-%s", s.Revision, operation), RunID: s.RunID, ExpectedRevision: s.Revision, Operation: operation})
		r := request(handler, "POST", "/api/sessions/"+sid+"/kun/control", string(body), "", "")
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	state = get()
	control(state, "resume")
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		state = get()
		if state.Status == "paused" && state.Step == 1 && state.Phase == "before_model" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state.Status != "paused" || state.Step != 1 {
		t.Fatal(state.Status, state.Phase, state.Error)
	}
	b, err := os.ReadFile(filepath.Join(w.Path, "outputs", sid, "result.txt"))
	if err != nil || string(b) != "verified" {
		t.Fatal(string(b), err)
	}
	control(state, "resume")
	waitState(t, m, sid, "completed")
	exported, err := m.Markdown(sid)
	if err != nil || !strings.Contains(exported, "Saved result.txt") {
		t.Fatal(exported, err)
	}
	rows, err := m.Store.Events(sid, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var modelEvent int64
	var sequence int64
	for _, ev := range rows {
		if strings.Contains(string(ev.Data), "do-not-persist-this-key") {
			t.Fatal("key leaked into journal")
		}
		if ev.Method == "kun/model.completed" {
			modelEvent = ev.ID
		}
		if ev.Method == "kun/model.started" {
			var envelope p.Event
			_ = json.Unmarshal(ev.Data, &envelope)
			sequence = envelope.Sequence
		}
	}
	if modelEvent == 0 || sequence == 0 {
		t.Fatal("missing network records")
	}
	reply, err := m.Reply(sid, modelEvent)
	if err != nil || reply.Text != "Saved result.txt" {
		t.Fatal(reply, err)
	}
	snap := request(handler, "GET", fmt.Sprintf("/api/sessions/%s/kun/snapshots/%d", sid, sequence), "", "", "")
	if snap.Code != 200 || !strings.Contains(snap.Body.String(), "before") && !strings.Contains(snap.Body.String(), `"phase":"model"`) {
		t.Fatal(snap.Code, snap.Body.String())
	}
	if _, err = m.Start(sid, Input{Text: "a second turn"}); err != nil {
		t.Fatal(err)
	}
	second := waitState(t, m, sid, "waiting")
	receipt, err := m.Steer(sid, SteerInput{Input: Input{Text: "additional instruction"}, ExpectedTurnID: second.TurnID, RequestID: "second-turn-steer"})
	if err != nil || receipt.Status != "queued" {
		t.Fatal(receipt, err)
	}
	if err = m.Stop(sid); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, sid, "interrupted")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var current p.State
	if err = client.Call(ctx, "state", nil, &current); err != nil {
		t.Fatal(err)
	}
	if current.RunID != second.RunID {
		t.Fatal("run identity drift")
	}
}
func TestKunRuntimeConfigGuards(t *testing.T) {
	m := testManager(t)
	i, _ := m.Instance()
	if _, err := m.SetAgentRuntime(i.ID, i.Revision, p.Config{Kind: "kun", Endpoint: "http://localhost:123/v1", Model: "fixture"}); err == nil {
		t.Fatal("demo allowed a real model backend")
	}
}
