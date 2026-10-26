package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUHPRequiredCapabilitiesRejectBeforeMaterialization(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	for _, capability := range []string{"max_step", "max_output_tokens", "native_instructions", "input_history", "future_unknown"} {
		body := fmt.Sprintf(`{"input":[{"role":"user","content":[{"type":"input_file","filename":"must-not-exist.txt","file_data":"data:text/plain;base64,aGVsbG8="}]}],"model":"unavailable-model","metadata":{"rundesk_require":[%q]}}`, capability)
		uhpTestError(t, uhpTestCall(h, "POST", "/v1/responses", body, userTestAdmin), 422, "rundesk_capability_unsupported")
	}
	if len(m.Tasks()) != 0 || len(m.Sessions()) != 0 {
		t.Fatal("rejected requirements created execution state")
	}
	for _, workspace := range m.Workspaces() {
		entries, err := os.ReadDir(filepath.Join(workspace.Path, "uploads"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatal("rejected requirements materialized attachments")
		}
	}
	for _, base := range []string{"codex", "kun"} {
		for _, requirement := range []string{"max_step", "native_instructions"} {
			if err := uhpCheckRequiredCapabilities(uhpRequest{RequiredCapabilities: []string{requirement}}, uhpHarness{Base: base}); err == nil {
				t.Fatalf("%s falsely accepted %s", base, requirement)
			}
		}
	}
}

func TestUHPRequiredCapabilitiesValidateMetadata(t *testing.T) {
	for _, value := range []string{`null`, `"max_step"`, `[1]`, `[""]`, `{"max_step":true}`} {
		if _, err := parseUHPRequest(strings.NewReader(`{"input":"x","metadata":{"rundesk_require":` + value + `}}`)); err == nil {
			t.Fatal("accepted malformed required capabilities", value)
		}
	}
	req, err := parseUHPRequest(strings.NewReader(`{"input":"x","metadata":{"rundesk_require":["progress","input_text","progress"]}}`))
	if err != nil || !reflect.DeepEqual(req.RequiredCapabilities, []string{"input_text", "progress"}) {
		t.Fatal(req, err)
	}
}

func TestUHPUnsupportedBudgetsAreExactAndCannotBeSpoofed(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	response := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"show budget disclosure","max_step":1,"max_output_tokens":20,"metadata":{"business_id":"keep-me","rundesk_capabilities":{"max_step":true},"rundesk_execution":{"version":999},"rundesk_instruction_mode":"native","rundesk_progress":{"state":"completed"},"rundesk_require":["input_text","timeout_seconds","progress"]}}`, userTestAdmin))
	if !reflect.DeepEqual(response.Metadata["ignored_fields"], []any{"max_output_tokens", "max_step"}) {
		t.Fatal("request limits were not explicitly disclosed", response.Metadata)
	}
	caps := response.Metadata["rundesk_capabilities"].(map[string]any)
	if caps["max_step"] != false || caps["max_output_tokens"] != false || caps["native_instructions"] != false || caps["timeout_seconds"] != true {
		t.Fatal("forged capability metadata", caps)
	}
	if response.Metadata["business_id"] != "keep-me" || response.Metadata["rundesk_instruction_mode"] != nil {
		t.Fatal("caller metadata crossed server namespace", response.Metadata)
	}
	record := uhpTestRecord(t, m, response.ID)
	task, err := m.Task(record.TaskID)
	if err != nil || record.MaxStep != 0 || task.Deadline != record.Deadline || task.Deadline <= 0 {
		t.Fatal("execution limits were not admitted atomically", record.MaxStep, task, err)
	}
	deadline, err := time.Parse(time.RFC3339Nano, response.Metadata["rundesk_deadline"].(string))
	if err != nil || deadline.UnixNano() != task.Deadline || time.Until(deadline) > time.Hour || time.Until(deadline) < 59*time.Minute {
		t.Fatal("default timeout was not applied", deadline, err)
	}
	for _, event := range record.Projection.Events {
		if event["type"] == "response.created" {
			created := event["response"].(map[string]any)
			meta := created["metadata"].(map[string]any)
			if meta["rundesk_progress"].(map[string]any)["state"] != "queued" || meta["rundesk_deadline"] != response.Metadata["rundesk_deadline"] {
				t.Fatal("initial SSE snapshot differs from accepted metadata", meta)
			}
		}
	}
	terminal := record.Projection.Events[len(record.Projection.Events)-1]
	b, _ := json.Marshal(response)
	var expected map[string]any
	if err := json.Unmarshal(b, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(terminal["response"], expected) {
		t.Fatal("terminal SSE and GET response differ", terminal["response"], expected)
	}
}

func TestUHPInstructionsDiscloseHistoryRetention(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, userTestAdmin, true)
	response := uhpTestResponse(t, uhpTestCall(h, "POST", "/v1/responses", `{"input":"plain task","instructions":"additional guidance"}`, userTestAdmin))
	if response.Metadata["rundesk_instruction_mode"] != "task_text" || response.Metadata["rundesk_instruction_retention"] != "session_history" {
		t.Fatal("native priority or one-turn removal implied", response.Metadata)
	}
}

func TestUHPLegacyStepLimitStopsWithoutClaimingToolRoundEnforcement(t *testing.T) {
	m := testManager(t)
	session, err := m.CreateSession(m.Workspaces()[0].ID, "legacy UHP budget", "demo-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.update(session.ID, func(s *Session) { s.RunID, s.TurnID, s.Status = "legacy-run", "native-turn", "running" }); err != nil {
		t.Fatal(err)
	}
	control := &uhpClosingTaskControl{Manager: m, task: Task{ID: "legacy-task", SessionID: session.ID, RunID: "legacy-run", Status: "running"}}
	u := uhpService{server: &Server{Manager: m, Tasks: control}}
	record := uhpRecord{Owner: "administrator", TaskID: control.task.ID, HostSessionID: session.ID, SessionID: "hsesslegacy", ContainerID: "cntr_legacy", MaxStep: 10, Projection: newUHPProjection(uhpResponse{ID: "resp_legacy", Store: true})}
	m.uhpMu.Lock()
	err = u.refresh(&record)
	m.uhpMu.Unlock()
	if err != nil || record.Projection.Response.Status != "incomplete" || record.Projection.Response.IncompleteDetails["reason"] != "legacy_step_budget_unsupported" {
		t.Fatal("legacy constraint silently discarded or mislabeled", record.Projection.Response, err)
	}
}
