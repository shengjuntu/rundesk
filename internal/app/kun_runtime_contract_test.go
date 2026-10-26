package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	p "github.com/your-org/kun/sdk/protocol"
)

// This fixture implements only the published JSONL boundary. It deliberately
// does not depend on Kun source, its SDK, or any model service.
func buildKunLegacyContractFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "worker.go")
	const program = `package main
import("bufio";"encoding/json";"os";"path/filepath")
func raw(v any) json.RawMessage { b,_:=json.Marshal(v); return b }
func main(){
 data:=""; for n:=1;n+1<len(os.Args);n++ { if os.Args[n]=="--data" {data=os.Args[n+1]} }
 scanner:=bufio.NewScanner(os.Stdin); var session,run string
 for scanner.Scan(){
  var q struct{Version int ` + "`json:\"version\"`" + `; ID string ` + "`json:\"id\"`" + `;Method string ` + "`json:\"method\"`" + `; Params json.RawMessage ` + "`json:\"params\"`" + `}; if json.Unmarshal(scanner.Bytes(),&q)!=nil{return}
  os.MkdirAll(data,0700);os.WriteFile(filepath.Join(data,q.Method+".called"),[]byte("yes"),0600)
  answer:=map[string]any{"version":16,"id":q.ID}
  switch q.Method {
  case "hello": answer["result"]=raw(map[string]any{"engine":"kun","engineVersion":"0.16.0","protocolVersion":16,"pid":os.Getpid(),"capabilities":map[string]bool{}})
  case "start":
   var in struct{SessionID string ` + "`json:\"sessionId\"`" + `;RunID string ` + "`json:\"runId\"`" + `};json.Unmarshal(q.Params,&in);session,run=in.SessionID,in.RunID
   os.MkdirAll(data,0700);os.WriteFile(filepath.Join(data,"start.called"),[]byte("yes"),0600)
   answer["result"]=raw(map[string]any{"sessionId":session,"runId":run,"status":"completed"})
  case "events":answer["result"]=raw([]any{map[string]any{"sequence":1,"sessionId":session,"runId":run,"type":"kun/run.finished","time":"2026-10-18T00:00:00Z","data":map[string]any{"status":"completed"}}})
  default:answer["error"]="fixture does not implement "+q.Method
  }
  if json.NewEncoder(os.Stdout).Encode(answer)!=nil{return}
 }
}`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	name := "legacy-kun-fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-buildvcs=false", "-o", binary, source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build independent JSONL fixture: %v\n%s", err, output)
	}
	return binary
}

func TestKunExternalVersionAndDiagnosticCapabilityBoundary(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	m, err := New(t.TempDir(), "missing-codex-must-not-fallback", false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Kun = buildKunLegacyContractFixture(t)
	i, _ := m.Instance()
	cfg := p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:1/v1", Model: "unused-fixture"}
	if _, err = m.SetAgentRuntime(i.ID, i.Revision, cfg); err != nil {
		t.Fatal(err)
	}
	source, err := m.CreateSession(m.Workspaces()[0].ID, "legacy core", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(source.ID, Input{Text: "core task"}); err != nil {
		t.Fatal(err)
	}
	source = waitState(t, m, source.ID, "completed")
	status, err := m.Runtime(source.ID)
	if err != nil || status.UserAgent != "Kun/0.16.0" || status.Effective["engineVersion"] != "0.16.0" || status.Effective["protocolVersion"] != float64(16) {
		t.Fatalf("runtime did not retain actual handshake: %+v (%v)", status, err)
	}
	if _, ok := status.Effective["capabilities"].(map[string]any); !ok {
		t.Fatal("missing observed capabilities", status.Effective)
	}
	handler := NewHandler(m, "", true)
	for _, operation := range []string{"pause", "resume", "step", "steer", "set_breakpoints", "set_harness"} {
		t.Run("control_"+operation, func(t *testing.T) {
			body := string(p.JSON(p.Control{RequestID: "unsupported-" + operation, RunID: source.RunID, ExpectedRevision: 1, Operation: operation}))
			response := request(handler, "POST", "/api/v1/sessions/"+source.ID+"/kun/control", body, "", "")
			if response.Code != 409 || !strings.Contains(response.Body.String(), "kun_capability_unsupported") {
				t.Fatal("control did not reject missing capability", response.Code, response.Body.String())
			}
		})
	}
	snapshot := request(handler, "GET", "/api/v1/sessions/"+source.ID+"/kun/snapshots/1", "", "", "")
	if snapshot.Code != 409 || !strings.Contains(snapshot.Body.String(), "kun_capability_unsupported") {
		t.Fatal("snapshot did not reject missing capability", snapshot.Code, snapshot.Body.String())
	}
	assertUnsupported := func(err error) {
		t.Helper()
		if e, ok := err.(*apiError); !ok || e.Code != "kun_capability_unsupported" || e.Status != 409 {
			t.Fatal("expected explicit capability error", err)
		}
	}
	_, err = m.KunCheckpoint(source.ID)
	assertUnsupported(err)
	_, err = m.KunForkPoints(source.ID, 0, 10)
	assertUnsupported(err)
	steerSource := source
	steerSource.Status = "running"
	_, err = m.steerKun(steerSource, SteerInput{Input: Input{Text: "new direction"}, RequestID: "unsupported-steer", ExpectedTurnID: source.TurnID})
	assertUnsupported(err)
	for _, method := range []string{"control", "snapshot", "checkpoint", "fork.points"} {
		if _, err := os.Stat(filepath.Join(m.Data, "kun", "sessions", source.ID, method+".called")); !os.IsNotExist(err) {
			t.Fatalf("unsupported %s was dispatched (%v)", method, err)
		}
	}
	if kunControlCapability("cancel") != "" || kunControlCapability("approve") != "" || kunControlCapability("reject") != "" {
		t.Fatal("stable core controls gained an extension requirement")
	}
	page, err := m.Store.DebugEvents(source.ID, 0, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	diagnosis, err := m.CreateTraceAnalysis(TraceSelection{SessionID: source.ID, RunID: source.RunID, Through: &page.Through})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := m.Workspace(diagnosis.WorkspaceID)
	start, err := m.kunDiagnosticRequest(diagnosis, w, Input{Text: "why"}, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	d := start.Diagnostic
	if d == nil || d.Provider == nil || d.SnapshotPath != "" || d.Provider.Name != "rundesk_trace" || !d.Provider.Allowlist || len(d.Provider.EnabledTools) != 9 {
		t.Fatalf("diagnostic provider boundary: %+v", d)
	}
	if err = d.Provider.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.Provider.Args) != 4 || d.Provider.Args[0] != "__trace_mcp" || d.Provider.Args[2] != source.ID {
		t.Fatal("diagnostic helper scope", d.Provider.Args)
	}
	if _, err = m.Start(diagnosis.ID, Input{Text: "why"}); err != nil {
		t.Fatal(err)
	}
	failed := waitState(t, m, diagnosis.ID, "failed")
	if !strings.Contains(failed.Error, "diagnosticProvider") || failed.RuntimeKind != "kun" {
		t.Fatal("missing extension did not fail explicitly", failed)
	}
	if _, err = os.Stat(filepath.Join(m.Data, "kun", "sessions", diagnosis.ID, "start.called")); !os.IsNotExist(err) {
		t.Fatal("diagnostic start was dispatched to an unsupported engine", err)
	}
	status, err = m.Runtime(diagnosis.ID)
	if err != nil || status.UserAgent != "Kun/0.16.0" {
		t.Fatal(status, err)
	}
	encoded, err := json.Marshal(start.Diagnostic)
	if err != nil || strings.Contains(string(encoded), "snapshotPath") {
		t.Fatalf("private database leaked into engine contract: %s (%v)", encoded, err)
	}
	sourceClient, err := m.kunClient(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	sourceClient.Close()
	_, err = m.kunClient(source.ID)
	if e, ok := err.(*apiError); !ok || e.Code != "kun_offline" || e.Status != 409 {
		t.Fatal("exited worker was not reported offline", err)
	}
	stateResponse := request(handler, "GET", "/api/v1/sessions/"+source.ID+"/kun/state", "", "", "")
	if stateResponse.Code != 409 || !strings.Contains(stateResponse.Body.String(), "kun_offline") {
		t.Fatal("state query reopened or used an exited worker", stateResponse.Code, stateResponse.Body.String())
	}
}

func TestKunSnapshotCapabilityLegacyAlias(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		caps          map[string]bool
		want          bool
	}{
		{"old_inspect", "0.16.0", map[string]bool{"inspect": true}, true},
		{"new_snapshot", "0.17.0", map[string]bool{"snapshot": true}, true},
		{"new_inspect_is_not_snapshot", "0.17.0", map[string]bool{"inspect": true}, false},
		{"unknown_inspect", "0.99.0", map[string]bool{"inspect": true}, false},
		{"unknown_explicit_snapshot", "0.99.0", map[string]bool{"snapshot": true}, true},
		{"old_disabled_inspect", "0.16.0", map[string]bool{"inspect": false}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := kunHasCapability(p.Hello{EngineVersion: tc.version, Capabilities: tc.caps}, "snapshot"); got != tc.want {
				t.Fatalf("snapshot capability = %v, want %v", got, tc.want)
			}
		})
	}
}
