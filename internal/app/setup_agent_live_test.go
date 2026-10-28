package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
)

func setupFixtureInstallResult(value any, depth int) map[string]any {
	if depth > 10 {
		return nil
	}
	switch v := value.(type) {
	case map[string]any:
		if _, status := v["status"].(string); status && v["input"] != nil && v["id"] != nil && v["result"] != nil {
			return v
		}
		for _, item := range v {
			if found := setupFixtureInstallResult(item, depth+1); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range v {
			if found := setupFixtureInstallResult(item, depth+1); found != nil {
				return found
			}
		}
	case string:
		var decoded any
		if json.Unmarshal([]byte(v), &decoded) == nil {
			return setupFixtureInstallResult(decoded, depth+1)
		}
	}
	return nil
}

// This exercises a real independently installed Kun worker and the real HTTP
// MCP/managed installer, with a scripted Chat Completions provider and local
// integrity-checked package fixture. It tests orchestration, not LLM judgment or
// public-network installation reliability.
func TestSetupRepairRealKunManagedInstallAndExperience(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native-package fixture executable is a POSIX script")
	}
	worker := buildKunTestBinary(t)
	m := testManager(t)
	m.Demo = false
	m.Kun = worker
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	archive, integrity := codexNativeFixture(t, "1.2.3")
	registry, downloads := codexRegistryFixture(t, archive, integrity, "1.2.3")
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer blocked.Close()
	catalog, err := m.BuildSources()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Groups = append(catalog.Groups, BuildSourceGroup{ID: "fixture", Name: "Local installation fixtures", Sources: []BuildSourceEntry{{ID: "fixture-blocked", Name: "Unavailable fixture", Type: "npm", Values: map[string]string{"registry": blocked.URL}}, {ID: "fixture-working", Name: "Local verified package", Type: "npm", Values: map[string]string{"registry": registry.URL}}}})
	if _, err = m.SaveBuildSources(catalog); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var modelMu sync.Mutex
	stage, installedID := 0, ""
	modelError := ""
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []p.Message `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		message := p.Message{Role: "assistant", Content: "OK"}
		finish := "stop"
		if len(request.Tools) == 1 && request.Tools[0].Function.Name == "rundesk_setup_ping" {
			message.Content = ""
			message.ToolCalls = []p.ToolCall{{ID: "probe", Type: "function", Function: p.Function{Name: "rundesk_setup_ping", Arguments: `{"value":"OK"}`}}}
			finish = "tool_calls"
		} else if len(request.Tools) > 1 {
			modelMu.Lock()
			defer modelMu.Unlock()
			calls.Add(1)
			alias := func(suffix string) string {
				short := suffix
				if len(short) > 20 {
					short = short[:20]
				}
				for _, tool := range request.Tools {
					if strings.Contains(tool.Function.Name, "_"+short+"_") {
						return tool.Function.Name
					}
				}
				modelError = "required setup MCP tool missing: " + suffix
				return suffix
			}
			tool, args := "", map[string]any{}
			switch stage {
			case 0:
				tool = "setup_install_get"
			case 1:
				tool = "setup_experiences_list"
			case 2:
				tool = "setup_sources_list"
			case 3:
				tool = "setup_codex_install"
				args = map[string]any{"method": "native", "sourceGroup": "fixture", "npmSourceId": "fixture-working", "requestId": "0123456789abcdef01234567"}
			case 4:
				for n := len(request.Messages) - 1; n >= 0; n-- {
					if request.Messages[n].Role != "tool" {
						continue
					}
					if result := setupFixtureInstallResult(request.Messages[n].Content, 0); result != nil && result["id"] != nil {
						installedID, _ = result["id"].(string)
						if result["status"] == "succeeded" {
							stage = 5
						} else if result["status"] == "failed" || result["status"] == "interrupted" {
							modelError = fmt.Sprintf("managed retry failed: %v", result["result"])
						}
						break
					}
				}
				if installedID == "" {
					modelError = "could not read installation ID from the actual MCP tool result"
				}
				if stage == 5 {
					tool, args = "setup_codex_verify", map[string]any{"installId": installedID}
				} else {
					tool, args = "setup_install_wait", map[string]any{"installId": installedID, "waitSeconds": 45}
				}
			case 6:
				tool = "setup_experiences_list"
			default:
				message.Content = "The managed Codex installation completed and was independently version-verified. Automatic repair evidence was retained."
			}
			if tool != "" && modelError == "" {
				message.Content = ""
				message.ToolCalls = []p.ToolCall{{ID: fmt.Sprintf("setup-%d", calls.Load()), Type: "function", Function: p.Function{Name: alias(tool), Arguments: string(p.JSON(args))}}}
				finish = "tool_calls"
				if stage != 4 {
					stage++
				}
			} else if modelError != "" {
				message.Content = "Fixture orchestration error: " + modelError
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"total_tokens": 16}})
	}))
	defer model.Close()
	handler := NewHandler(m, "", true)
	server := httptest.NewServer(handler)
	defer server.Close()
	probe := v1Request(handler, "POST", "/setup/model-test", string(p.JSON(SetupModelTestInput{Endpoint: model.URL + "/v1", Model: "setup-fixture"})), store.ID())
	if probe.Code != 200 || !strings.Contains(probe.Body.String(), `"toolsOK":true`) {
		t.Fatal("actual HTTP model probe failed", probe.Code, probe.Body.String())
	}
	configured, err := m.saveSetupKun(t.Context(), SetupKunInput{Endpoint: model.URL + "/v1", Model: "setup-fixture", KunPath: worker})
	if err != nil || !configured.Ready {
		t.Fatal(configured, err)
	}
	failed, err := m.StartCodexInstall(CodexInstallInput{Version: "1.2.3", Confirm: true, Method: "native", SourceGroup: "fixture", NPMSourceID: "fixture-blocked", RequestID: store.ID()})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		failed, err = m.CodexInstall(failed.ID)
		if err != nil || failed.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || failed.Status != "failed" {
		t.Fatal("source download did not fail as arranged", failed, err)
	}
	run, err := m.CreateSetupRepair(SetupRepairInput{Kind: "codex-install", InstallID: failed.ID, RequestID: store.ID()}, server.URL+"/api/setup/agent-mcp")
	if err != nil {
		t.Fatal(err)
	}
	m.queueMu.Lock()
	m.queue.Paused = false
	m.queueMu.Unlock()
	deadline = time.Now().Add(30 * time.Second)
	var session Session
	for time.Now().Before(deadline) {
		session, err = m.Session(run.SessionID)
		if err != nil || (!active(session.Status) && session.Status != "idle") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	modelMu.Lock()
	modelProblem, finalID, finalStage := modelError, installedID, stage
	modelMu.Unlock()
	if err != nil || session.Status != "completed" || modelProblem != "" || finalStage < 7 {
		t.Fatalf("real Kun repair did not complete: session=%#v stage=%d modelError=%s err=%v", session, finalStage, modelProblem, err)
	}
	job, err := m.CodexInstall(finalID)
	if err != nil || job.Status != "succeeded" || !job.Result.OK || job.Input.Version != "1.2.3" || downloads.Load() != 1 {
		t.Fatal("real scoped MCP did not install/verify exact package once", job, err, downloads.Load())
	}
	experiences, err := m.BuildExperiences("codex-install", "1.2.3")
	if err != nil || len(experiences) != 1 || experiences[0].Status != "verified" || experiences[0].Failure.ID != failed.ID || experiences[0].Success.ID != job.ID {
		t.Fatal("automatic repair evidence was not retained", experiences, err)
	}
	if calls.Load() < 7 || calls.Load() > 20 {
		t.Fatal("unexpected model/tool-loop count", calls.Load())
	}
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	next, err := m.CreateSetupRepair(SetupRepairInput{Kind: "codex-install", InstallID: failed.ID, RequestID: store.ID()}, server.URL+"/api/setup/agent-mcp")
	if err != nil {
		t.Fatal(err)
	}
	nextTask, err := m.Task(next.TaskID)
	if err != nil || !strings.Contains(nextTask.Spec.Input.Text, experiences[0].ID) || !strings.Contains(nextTask.Spec.Input.Text, "untrusted evidence") {
		t.Fatal("next repair did not receive scoped verified historical evidence", nextTask, err)
	}
	t.Logf("Real Kun %s: scoped HTTP MCP, actual 403→native SHA512 package install, independent --version, verified automatic experience; fixture model calls=%d", configured.Worker.EngineVersion, calls.Load())
}
