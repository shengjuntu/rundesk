package app

import (
	"context"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"github.com/shengjuntu/rundesk/internal/tracequery"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestKunDiagnosticSourceRemainsPausedAndFrozen(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	m, err := New(t.TempDir(), "no-codex-needed", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Kun = buildKunTestBinary(t)
	var calls atomic.Int32
	var run string
	var cited int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []p.Message       `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		text := string(p.JSON(req))
		if len(req.Tools) != 7 || strings.Contains(text, "BUSINESS-INSTRUCTIONS") || strings.Contains(text, "FUTURE-SOURCE-RECORD") {
			t.Errorf("diagnostic contamination: %s", text)
		}
		n := calls.Add(1)
		var msg p.Message
		finish := "tool_calls"
		switch n {
		case 1:
			msg = p.Message{Role: "assistant", ToolCalls: []p.ToolCall{{ID: "query", Type: "function", Function: p.Function{Name: "trace_statistics", Arguments: fmt.Sprintf(`{"runId":%q}`, run)}}}}
		case 2:
			msg = p.Message{Role: "assistant", ToolCalls: []p.ToolCall{{ID: "suggest", Type: "function", Function: p.Function{Name: "trace_propose", Arguments: fmt.Sprintf(`{"runId":%q,"proposal":{"kind":"inspect","reason":"运行在模型调用前暂停","text":"核对断点后再决定是否继续","evidenceIds":[%d]}}`, run, cited)}}}}
		default:
			msg = p.Message{Role: "assistant", Content: "已记录事实：模型调用前暂停。建议待审核，没有执行控制。"}
			finish = "stop"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": finish}}, "usage": map[string]int{"total_tokens": 12}})
	}))
	defer provider.Close()
	i, _ := m.Instance()
	cfg := p.Config{Kind: "kun", Endpoint: provider.URL + "/v1", Model: "diagnostic-fixture", AllowWrite: true, PauseBeforeModel: true, SystemPrompt: "BUSINESS-INSTRUCTIONS", MaxSteps: 5}
	if _, err = m.SetAgentRuntime(i.ID, i.Revision, cfg); err != nil {
		t.Fatal(err)
	}
	source, err := m.CreateSession(m.Workspaces()[0].ID, "paused business task", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(source.ID, Input{Text: "source task"}); err != nil {
		t.Fatal(err)
	}
	source = waitState(t, m, source.ID, "waiting")
	run = source.RunID
	client, err := m.kunClient(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	var original p.State
	if err = client.Call(context.Background(), "state", nil, &original); err != nil {
		t.Fatal(err)
	}
	page, _ := m.Store.DebugEvents(source.ID, 0, nil, 200)
	through := page.Through
	cited = page.Events[0].ID
	m.Store.Add(source.ID, "in", "warning", map[string]any{"runId": run, "message": "FUTURE-SOURCE-RECORD"})
	before, _ := m.Store.Events(source.ID, 0, 1000)
	handler := NewHandler(m, userTestAdmin, true)
	body := string(p.JSON(map[string]any{"traceAnalysis": TraceSelection{SessionID: source.ID, RunID: run, Through: &through}}))
	create := func() Session {
		res := appRequest(handler, "POST", "/api/v1/sessions", body, userTestAdmin, "diagnostic-create-fixed-01")
		if res.Code != 200 {
			t.Fatal(res.Code, res.Body.String())
		}
		var v Session
		json.Unmarshal(res.Body.Bytes(), &v)
		return v
	}
	diagnosis := create()
	replay := create()
	if diagnosis.ID != replay.ID || diagnosis.ID == source.ID || diagnosis.RuntimeKind != "kun" || diagnosis.TraceOrigin.Through != through {
		t.Fatal(diagnosis, replay)
	}
	if _, err = m.Start(diagnosis.ID, Input{Text: "analyse", Files: []string{"private.txt"}}); err == nil {
		t.Fatal("diagnostic file input accepted")
	}
	if _, err = m.Start(diagnosis.ID, Input{Text: "解释暂停的原因"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, diagnosis.ID, "completed")
	if calls.Load() != 3 {
		t.Fatal("unexpected model execution", calls.Load())
	}
	var after p.State
	if err = client.Call(context.Background(), "state", nil, &after); err != nil {
		t.Fatal(err)
	}
	afterEvents, _ := m.Store.Events(source.ID, 0, 1000)
	if after.Revision != original.Revision || after.Status != "paused" || len(afterEvents) != len(before) {
		t.Fatal("diagnosis modified original runtime", original, after)
	}
	dclient, err := m.kunClient(diagnosis.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ds p.State
	dclient.Call(context.Background(), "state", nil, &ds)
	if ds.Budget.ReportedTokens != 36 || original.Budget.ReportedTokens != 0 || ds.Diagnostic == nil {
		t.Fatal("diagnosis usage/source missing", ds)
	}
	snapshot := filepath.Join(m.Data, "kun", "sessions", diagnosis.ID, "trace-source.db")
	reader, err := tracequery.Open(snapshot, source.ID, through)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err = reader.Call("trace_read_event", tracequery.Args{EventID: through + 1}); err == nil {
		t.Fatal("later event copied")
	}
	other, _ := m.CreateSession(source.WorkspaceID, "not selected", "")
	if foreign, e := tracequery.Open(snapshot, other.ID, through); e == nil {
		foreign.Close()
		t.Fatal("other session copied")
	}
	if _, err = m.Start(diagnosis.ID, Input{Text: "继续解释，保持相同证据"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, diagnosis.ID, "completed")
	_, _, readKey := setupKey(t, m, "no-analysis-admission", source.WorkspaceID, "read", "run")
	res := appRequest(handler, "POST", "/api/v1/sessions", body, readKey, "diagnostic-denied")
	if res.Code != 403 {
		t.Fatal("application gained diagnostic creation", res.Code)
	}
	zero := int64(0)
	if _, err = m.CreateTraceAnalysis(TraceSelection{SessionID: source.ID, RunID: run, Through: &zero}); err == nil {
		t.Fatal("empty fixed cursor advanced")
	}
	future := through + 999
	if _, err = m.CreateTraceAnalysis(TraceSelection{SessionID: source.ID, RunID: run, Through: &future}); err == nil {
		t.Fatal("future cursor accepted")
	}
	m.Stop(source.ID)
}
