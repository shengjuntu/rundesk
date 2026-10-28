package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
)

// Decode the actual result as it travels through MCP text blocks and the
// worker's tool message. The scripted provider never fabricates IDs/results.
func kunBuildFixtureValue(value any, predicate func(map[string]any) bool, depth int) map[string]any {
	if depth > 20 {
		return nil
	}
	switch v := value.(type) {
	case map[string]any:
		if predicate(v) {
			return v
		}
		for _, item := range v {
			if found := kunBuildFixtureValue(item, predicate, depth+1); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range v {
			if found := kunBuildFixtureValue(item, predicate, depth+1); found != nil {
				return found
			}
		}
	case string:
		var decoded any
		if json.Unmarshal([]byte(v), &decoded) == nil {
			return kunBuildFixtureValue(decoded, predicate, depth+1)
		}
	}
	return nil
}

// This is a real external Kun process and real scoped HTTP MCP integration.
// Docker build/run/inspect are process-backed fixtures, and the model provider
// is scripted. It verifies orchestration and evidence, not Docker correctness
// or a real model's ability to diagnose a dependency defect.
func TestBuildAssistantRealKunRepairPackImportBuildAndEvidence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Docker admission/process fixtures require Linux")
	}
	worker := buildKunTestBinary(t)
	m := testManager(t)
	m.Demo = false
	m.Kun = worker
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	driver := &recipeQueueDocker{fakeDocker: newFakeDocker()}
	m.docker = driver
	original := buildZip(t, map[string]string{"Dockerfile": "FROM scratch\nLABEL fixture.broken=true\n", "tools/smoke.sh": "#!/bin/sh\nprintf 'fixture artifact\\n' > /tmp/result.txt\n"})
	source := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: "recipe", Status: "failed", Name: "Failed fixture image", Dockerfile: "Dockerfile", ContextSHA256: fmt.Sprintf("%x", sha256.Sum256(original)), PlatformOS: runtime.GOOS, PlatformArch: runtime.GOARCH, RecipeSmoke: &RecipeSmokeSpec{Command: []string{"sh", "tools/smoke.sh"}, TimeoutSeconds: 30, ExpectedFiles: []string{"/tmp/result.txt"}}, Error: "fixture dependency failed", Created: store.Now(), Updated: store.Now()}
	if err := os.MkdirAll(m.buildDir(source.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.buildDir(source.ID), "context.zip"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.buildDir(source.ID), "build.log"), []byte("fixture dependency failed while packaging\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Store.Put("image-build", source.ID, source); err != nil {
		t.Fatal(err)
	}
	var modelMu sync.Mutex
	var calls atomic.Int32
	stage, problem := 0, ""
	recipeID, buildID, offlineID, smokeID := "", "", "", ""
	recipeRevision := 0
	observedTools := map[string]bool{}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []p.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		modelMu.Lock()
		defer modelMu.Unlock()
		number := calls.Add(1)
		alias := func(name string) string {
			if name == "write_file" {
				for _, tool := range request.Tools {
					if tool.Function.Name == name {
						observedTools[name] = true
						return name
					}
				}
			}
			short := name
			if len(short) > 20 {
				short = short[:20]
			}
			for _, tool := range request.Tools {
				if strings.Contains(tool.Function.Name, "_"+short+"_") {
					observedTools[name] = true
					return tool.Function.Name
				}
			}
			problem = "missing actual worker tool: " + name
			return name
		}
		var latest any
		for n := len(request.Messages) - 1; n >= 0; n-- {
			if request.Messages[n].Role == "tool" {
				latest = request.Messages[n].Content
				break
			}
		}
		if failure := kunBuildFixtureValue(latest, func(v map[string]any) bool { return v["isError"] == true }, 0); failure != nil {
			problem = fmt.Sprintf("tool reported error at stage %d: %v", stage, failure)
		}
		find := func(keys ...string) map[string]any {
			return kunBuildFixtureValue(latest, func(v map[string]any) bool {
				for _, key := range keys {
					if _, ok := v[key]; !ok {
						return false
					}
				}
				return true
			}, 0)
		}
		tool := ""
		advanceStage := true
		args := map[string]any{}
		switch stage {
		case 0:
			tool = "build_get"
			args["buildId"] = source.ID
		case 1:
			result := find("id", "status", "scope")
			if result == nil || result["id"] != source.ID || result["status"] != "failed" {
				problem = fmt.Sprintf("source result missing: %v", latest)
			}
			tool = "build_log"
			args["buildId"] = source.ID
		case 2:
			result := find("text", "hasMore")
			if result == nil || !strings.Contains(fmt.Sprint(result["text"]), "fixture dependency failed") {
				problem = "source log was not delivered by scoped MCP"
			}
			tool = "build_experiences_list"
		case 3:
			tool = "build_sources_list"
		case 4:
			tool = "write_file"
			args = map[string]any{"path": "artifacts/context/Dockerfile", "content": "FROM scratch\nLABEL fixture.repaired=true\n"}
		case 5:
			tool = "build_context_pack"
			args = map[string]any{"contextDirectory": "artifacts/context", "contextPath": "artifacts/context.zip"}
		case 6:
			tool = "build_recipe_import"
			args = map[string]any{"manifestPath": "artifacts/recipe.json", "contextPath": "artifacts/context.zip", "requestId": "111111111111111111111111"}
		case 7:
			result := find("id", "revision", "kind")
			if result == nil {
				problem = fmt.Sprintf("import result missing: %v", latest)
			} else {
				recipeID, _ = result["id"].(string)
				revision, _ := result["revision"].(float64)
				recipeRevision = int(revision)
			}
			tool = "build_start"
			args = map[string]any{"recipeId": recipeID, "recipeRevision": recipeRevision, "requestId": "222222222222222222222222"}
		case 8:
			result := find("id", "scope", "status")
			if result == nil {
				problem = fmt.Sprintf("queued build result missing: %v", latest)
			} else {
				buildID, _ = result["id"].(string)
			}
			tool = "build_wait"
			args = map[string]any{"buildId": buildID, "waitSeconds": 5}
		case 9:
			result := find("id", "scope", "status")
			if result != nil && buildActive(fmt.Sprint(result["status"])) {
				tool = "build_wait"
				args = map[string]any{"buildId": buildID, "waitSeconds": 5}
				advanceStage = false
				break
			}
			if result == nil || result["status"] != "succeeded" {
				problem = fmt.Sprintf("fixture build did not succeed: %v", latest)
			}
			tool = "build_image_check"
			args["buildId"] = buildID
		case 10:
			tool = "build_image_test"
			args = map[string]any{"buildId": buildID, "kind": "offline", "requestId": "333333333333333333333333"}
		case 11:
			result := find("id", "scope", "testKind")
			if result == nil {
				problem = fmt.Sprintf("offline test result missing: %v", latest)
			} else {
				offlineID, _ = result["id"].(string)
			}
			tool = "build_wait"
			args = map[string]any{"buildId": offlineID, "waitSeconds": 5}
		case 12:
			result := find("id", "scope", "status")
			if result != nil && buildActive(fmt.Sprint(result["status"])) {
				tool = "build_wait"
				args = map[string]any{"buildId": offlineID, "waitSeconds": 5}
				advanceStage = false
				break
			}
			if result == nil || result["status"] != "succeeded" {
				problem = fmt.Sprintf("offline fixture did not succeed: %v", latest)
			}
			tool = "build_image_test"
			args = map[string]any{"buildId": buildID, "kind": "smoke", "requestId": "444444444444444444444444"}
		case 13:
			result := find("id", "scope", "testKind")
			if result == nil {
				problem = fmt.Sprintf("smoke test result missing: %v", latest)
			} else {
				smokeID, _ = result["id"].(string)
			}
			tool = "build_wait"
			args = map[string]any{"buildId": smokeID, "waitSeconds": 5}
		case 14:
			result := find("id", "scope", "status")
			if result != nil && buildActive(fmt.Sprint(result["status"])) {
				tool = "build_wait"
				args = map[string]any{"buildId": smokeID, "waitSeconds": 5}
				advanceStage = false
				break
			}
			if result == nil || result["status"] != "succeeded" {
				problem = fmt.Sprintf("smoke fixture did not succeed: %v", latest)
			}
			tool = "build_experiences_list"
		default:
			verified := kunBuildFixtureValue(latest, func(v map[string]any) bool { return v["kind"] == "image-build" && v["status"] == "verified" }, 0)
			if verified == nil {
				// Completion persistence precedes the post-completion evidence
				// callback. Poll the real tool rather than guessing that the
				// callback completed in the same scheduling slice.
				if number < 30 {
					time.Sleep(20 * time.Millisecond)
					tool = "build_experiences_list"
					advanceStage = false
				} else {
					problem = fmt.Sprintf("automatic verified evidence not returned by actual MCP: %v", latest)
				}
			}
		}
		message := p.Message{Role: "assistant", Content: "Fixture repair complete; pack/import/build/offline/smoke evidence retained. Docker and model were fixtures."}
		finish := "stop"
		if problem != "" {
			message.Content = problem
		} else if tool != "" {
			message.Content = ""
			message.ToolCalls = []p.ToolCall{{ID: fmt.Sprintf("build-fixture-%d", number), Type: "function", Function: p.Function{Name: alias(tool), Arguments: string(p.JSON(args))}}}
			finish = "tool_calls"
			if advanceStage {
				stage++
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"total_tokens": 16}})
	}))
	defer model.Close()
	instance, err := m.CreateInstance(InstancePatch{Name: "Kun real worker build fixture"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err = m.SetAgentRuntime(instance.ID, instance.Revision, p.Config{Kind: "kun", Endpoint: model.URL + "/v1", Model: "scripted-build-fixture", AllowWrite: true, MaxSteps: 40, TimeoutSeconds: 10, Budget: p.BudgetLimits{MaxToolCalls: 80, MaxActiveSeconds: 120, MaxConsecutiveFailures: 2}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(m, "", true))
	defer server.Close()
	run, err := m.CreateBuildAssistantRun(BuildAssistantInput{Prompt: "Repair the copied frozen Dockerfile and preserve the original smoke contract.", InstanceID: instance.ID, SourceBuildID: source.ID, RequestID: store.ID()}, server.URL+"/api/build-center/mcp")
	if err != nil {
		t.Fatal(err)
	}
	m.queueMu.Lock()
	m.queue.Paused = false
	m.queueMu.Unlock()
	deadline := time.Now().Add(45 * time.Second)
	var session Session
	for time.Now().Before(deadline) {
		session, err = m.Session(run.SessionID)
		if err != nil || (!active(session.Status) && session.Status != "idle") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	modelMu.Lock()
	finalStage, modelProblem := stage, problem
	finalRecipe, finalBuild, finalOffline, finalSmoke := recipeID, buildID, offlineID, smokeID
	toolSet := make(map[string]bool, len(observedTools))
	for name, present := range observedTools {
		toolSet[name] = present
	}
	modelMu.Unlock()
	if err != nil || session.Status != "completed" || modelProblem != "" || finalStage != 15 {
		events, _ := m.Store.Events(run.SessionID, 0, 1000)
		t.Fatalf("real worker integration failed: session=%+v stage=%d problem=%s err=%v events=%s", session, finalStage, modelProblem, err, p.JSON(events))
	}
	build, err := m.BuildCenterBuild(finalBuild)
	if err != nil || build.Status != "succeeded" || build.RebuildOf != source.ID || build.OwnerRunID != run.ID {
		t.Fatal(build, err)
	}
	for _, id := range []string{finalOffline, finalSmoke} {
		test, err := m.BuildCenterBuild(id)
		if err != nil || test.Status != "succeeded" || test.SourceBuildID != build.ID {
			t.Fatal(test, err)
		}
	}
	experiences, err := m.BuildExperiences("image-build", "")
	if err != nil {
		t.Fatal(err)
	}
	verified := false
	for _, experience := range experiences {
		if experience.Status == "verified" && experience.Success.ID == build.ID && experience.Failure.ID == source.ID && experience.Validation.Offline && experience.Validation.Smoke && experience.Patch != nil && strings.Contains(experience.Patch.DockerfileDiff, "fixture.repaired=true") {
			verified = true
		}
	}
	if !verified {
		t.Fatal("automatic versioned repair evidence absent", experiences)
	}
	frozen, err := os.ReadFile(filepath.Join(m.buildDir(source.ID), "context.zip"))
	if err != nil || !bytes.Equal(original, frozen) {
		t.Fatal("source frozen context changed", err)
	}
	workspace, err := m.Workspace(run.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	patched, err := os.ReadFile(filepath.Join(workspace.Path, "artifacts", "context", "Dockerfile"))
	if err != nil || !strings.Contains(string(patched), "fixture.repaired=true") {
		t.Fatal("actual worker write_file did not patch the copy", string(patched), err)
	}
	if finalRecipe == "" || calls.Load() < 16 || calls.Load() >= 30 {
		t.Fatal("unexpected recipe/model count", finalRecipe, calls.Load())
	}
	for _, name := range []string{"build_get", "build_log", "build_experiences_list", "build_sources_list", "write_file", "build_context_pack", "build_recipe_import", "build_start", "build_wait", "build_image_check", "build_image_test"} {
		if !toolSet[name] {
			t.Fatal("expected real tool invocation absent", name)
		}
	}
	t.Logf("real external Kun + scoped build MCP: recipe=%s build=%s offline=%s smoke=%s modelCalls=%d; Docker/model fixtures, no public downloads", finalRecipe, finalBuild, finalOffline, finalSmoke, calls.Load())
}
