package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
	"github.com/shengjuntu/rundesk/internal/store"
)

// The existing test executable implements only the SDK hello exchange here.
// This checks a real process/protocol boundary without requiring a model or an
// external Kun installation. It is not a claim of end-to-end agent evaluation.
func init() {
	if os.Getenv("RUNDESK_SETUP_TEST_WORKER") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var request p.Envelope
		if json.Unmarshal(scan.Bytes(), &request) != nil {
			os.Exit(1)
		}
		info := p.Hello{Engine: "kun", EngineVersion: p.EngineVersion, ProtocolVersion: p.Version, PID: os.Getpid(), Capabilities: map[string]bool{}}
		if os.Getenv("RUNDESK_SETUP_TEST_BAD_PROTOCOL") == "1" {
			info.ProtocolVersion++
		}
		_ = json.NewEncoder(os.Stdout).Encode(p.Envelope{Version: p.Version, ID: request.ID, Result: p.JSON(info)})
	}
	os.Exit(0)
}

func setupAgentFixture(t *testing.T) (*Manager, SetupKunStatus) {
	t.Helper()
	m := testManager(t)
	m.Demo = false
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m.Kun = executable
	t.Setenv("RUNDESK_SETUP_TEST_WORKER", "1")
	endpoint := "http://127.0.0.1:8000/v1"
	evidence := setupModelTestEvidence{SetupModelTestResult: SetupModelTestResult{Endpoint: endpoint, Model: "fixture-model", InferenceOK: true, ToolsOK: true, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: "ready"}, CredentialHash: keyHash("")}
	if err = m.Store.Put("setup-model-test", setupModelTestKey(endpoint, "fixture-model", ""), evidence); err != nil {
		t.Fatal(err)
	}
	value, err := m.saveSetupKun(context.Background(), SetupKunInput{Endpoint: endpoint, Model: "fixture-model", KunPath: executable})
	if err != nil || !value.Ready || !value.Available {
		t.Fatalf("setup: %#v %v", value, err)
	}
	return m, value
}

func setupRepairFixture(t *testing.T) (*Manager, SetupRepairRun, string) {
	t.Helper()
	m, _ := setupAgentFixture(t)
	failed := CodexInstallJob{ID: store.ID(), Status: "failed", Input: CodexInstallInput{Version: "0.157.1", Method: "npm", Confirm: true}, Result: CodexInstallResult{Version: "0.157.1", Error: "npm not found", Log: "npm not found"}}
	if err := m.Store.Put("codex-install", failed.ID, codexInstallRecord{CodexInstallJob: failed}); err != nil {
		t.Fatal(err)
	}
	in := SetupRepairInput{Kind: "codex-install", InstallID: failed.ID, RequestID: store.ID()}
	run, err := m.CreateSetupRepair(in, "http://127.0.0.1:3210/api/setup/agent-mcp")
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.CreateSetupRepair(in, "http://127.0.0.1:3210/api/setup/agent-mcp")
	if err != nil || !reflect.DeepEqual(run, again) {
		t.Fatalf("retry changed run: %#v %#v %v", run, again, err)
	}
	secret, err := os.ReadFile(m.setupRepairSecretPath(run.ID))
	if err != nil {
		t.Fatal(err)
	}
	return m, run, string(secret)
}

func TestSetupKunSeparateIdentityDraftAndVerifiedWorker(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	m.Codex = "missing-codex"
	before, _ := m.Instance()
	missing := filepath.Join(t.TempDir(), "missing-kun")
	value, err := m.saveSetupKun(context.Background(), SetupKunInput{Endpoint: "http://127.0.0.1:8000", Model: "test-model", KunPath: missing})
	if err != nil || !value.Configured || value.Available || value.Ready || value.Endpoint != "http://127.0.0.1:8000/v1" {
		t.Fatalf("invalid draft: %#v %v", value, err)
	}
	after, _ := m.Instance()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("initialization changed the default instance")
	}
	i, _ := m.Instance(setupKunInstanceID)
	if !i.AgentRuntime.AllowWrite || i.AgentRuntime.AllowBash || i.Permissions.Sandbox != "workspace-write" {
		t.Fatal("unexpected setup permissions", i)
	}
	if _, err = m.saveSetupKun(context.Background(), SetupKunInput{Endpoint: "http://127.0.0.1:8000", Model: "test-model", KunPath: missing}); err == nil {
		t.Fatal("stale setup form overwrote the newer revision")
	}
}

func TestSetupKunReadinessUsesActualWorkerAndModelEvidence(t *testing.T) {
	m, value := setupAgentFixture(t)
	if value.Worker.ProtocolVersion != p.Version || value.Worker.Engine != "kun" {
		t.Fatal("worker handshake was not recorded", value)
	}
	var evidence setupModelTestEvidence
	_ = m.Store.Get("setup-model-test", setupModelTestKey(value.Endpoint, value.Model, ""), &evidence)
	evidence.ToolsOK = false
	_ = m.Store.Put("setup-model-test", setupModelTestKey(value.Endpoint, value.Model, ""), evidence)
	value, err := m.setupKunStatus()
	if err != nil || value.Ready || !value.Available {
		t.Fatalf("tool-less provider became agent-ready: %#v %v", value, err)
	}
	evidence.ToolsOK = true
	evidence.CheckedAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	_ = m.Store.Put("setup-model-test", setupModelTestKey(value.Endpoint, value.Model, ""), evidence)
	value, _ = m.setupKunStatus()
	if value.Ready || value.ModelTest != nil {
		t.Fatal("expired model evidence accepted", value)
	}
	t.Setenv("RUNDESK_SETUP_TEST_BAD_PROTOCOL", "1")
	revision := value.Revision
	value, err = m.saveSetupKun(context.Background(), SetupKunInput{Endpoint: value.Endpoint, Model: value.Model, KunPath: m.Kun, ExpectedRevision: &revision})
	if err != nil || value.Available || value.Ready || !strings.Contains(value.Error, "handshake") {
		t.Fatalf("bad protocol became available: %#v %v", value, err)
	}
}

func TestSetupKunAcceptsAliasOfExplicitWorker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link creation is not normally available for this fixture on Windows")
	}
	m, before := setupAgentFixture(t)
	alias := filepath.Join(t.TempDir(), "kun-alias")
	if err := os.Symlink(m.Kun, alias); err != nil {
		t.Fatal(err)
	}
	value, err := m.saveSetupKun(context.Background(), SetupKunInput{Endpoint: before.Endpoint, Model: before.Model, KunPath: alias, ExpectedRevision: &before.Revision})
	if err != nil || !value.Ready || !value.Available {
		t.Fatal("same explicit executable was rejected by its alias", value, err)
	}
}

func TestSetupRepairScopedTaskAndNoHostBash(t *testing.T) {
	m, run, token := setupRepairFixture(t)
	record, err := m.setupRepairAuthorized(token)
	if err != nil || record.Version != "0.157.1" {
		t.Fatal(record, err)
	}
	s, _ := m.Session(run.SessionID)
	w, _ := m.Workspace(run.WorkspaceID)
	request, err := m.configureSetupAgent(s, w, p.Start{Config: p.Config{AllowBash: true, AllowWrite: true}, MCP: []p.MCPServer{{Name: "foreign", URL: "http://127.0.0.1:1"}}})
	if err != nil || request.Config.AllowBash || len(request.MCP) != 1 || request.MCP[0].Name != "rundesk_setup" || len(request.Skills) != 1 {
		t.Fatal("repair transport/skill preparation", request, err)
	}
	if strings.Contains(request.Input, token) || !strings.Contains(request.MCP[0].Headers["Authorization"], token) {
		t.Fatal("capability is not isolated to the MCP transport")
	}
	if _, err = m.configureSetupAgent(s, w, p.Start{Config: p.Config{AllowWrite: false}}); err == nil {
		t.Fatal("setup preparation overrode denied workspace write permission")
	}
	if _, err = m.callSetupRepairTool(context.Background(), token, "setup_install_get", setupRepairToolArgs{InstallID: store.ID()}); err == nil {
		t.Fatal("repair read a foreign install record")
	}
	value, err := m.callSetupRepairTool(context.Background(), token, "setup_install_get", setupRepairToolArgs{})
	if err != nil || value.(CodexInstallJob).ID != run.SourceInstallID {
		t.Fatal(value, err)
	}
	if _, err = m.callSetupRepairTool(context.Background(), token, "setup_codex_install", setupRepairToolArgs{Method: "native", SourceGroup: "international", NPMSourceID: "not-in-catalog", RequestID: store.ID()}); err == nil {
		t.Fatal("repair accepted an unregistered source")
	}
	current, _ := m.Instance(run.InstanceID)
	readOnly := Permissions{Sandbox: "read-only", ApprovalPolicy: "never"}
	if _, err = m.PatchInstance(current.ID, InstancePatch{Name: current.Name, DefaultModel: current.DefaultModel, Permissions: &readOnly, Revision: current.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.setupRepairAuthorized(token); err == nil {
		t.Fatal("changed instance policy retained the earlier installation grant")
	}
	if ready, _ := m.setupKunStatus(); ready.Ready {
		t.Fatal("read-only initialization instance remained ready")
	}
	if _, err = m.CancelTask(run.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.setupRepairAuthorized(token); err == nil {
		t.Fatal("canceled task retained installation authority")
	}
}

func TestSetupRepairAutomaticGrantBindsKunConfiguration(t *testing.T) {
	m, old, _ := setupRepairFixture(t)
	var configuration setupKunConfig
	if err := m.Store.Get("system", "setup-kun", &configuration); err != nil {
		t.Fatal(err)
	}
	input := SetupRepairInput{Kind: "codex-install", InstallID: old.SourceInstallID, RequestID: store.ID(), ExpectedSetupRevision: configuration.Revision + 1, ExpectedSetupFingerprint: buildAssistantDigest(configuration)}
	before := len(m.Tasks())
	if _, err := m.CreateSetupRepair(input, "http://127.0.0.1:3210/api/setup/agent-mcp"); err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatal("automatic handoff ignored its Kun configuration binding", err)
	}
	if len(m.Tasks()) != before {
		t.Fatal("rejected automatic handoff still created a task")
	}
	input.ExpectedSetupRevision = configuration.Revision
	run, err := m.CreateSetupRepair(input, "http://127.0.0.1:3210/api/setup/agent-mcp")
	if err != nil || run.ID == "" {
		t.Fatal("unchanged automatic handoff was rejected", run, err)
	}
}

func TestSetupRepairRetriesVersionScopeAndCrashRecovery(t *testing.T) {
	m, run, token := setupRepairFixture(t)
	catalog, err := m.BuildSources()
	if err != nil {
		t.Fatal(err)
	}
	var group, source string
	for _, g := range catalog.Groups {
		for _, s := range g.Sources {
			if s.Type == "npm" {
				group, source = g.ID, s.ID
				break
			}
		}
		if source != "" {
			break
		}
	}
	requestID := store.ID()
	in := CodexInstallInput{Version: "0.157.1", Confirm: true, Method: "native", SourceGroup: group, CatalogRevision: catalog.Revision, NPMSourceID: source, RequestID: buildAssistantDigest("setup-repair-install:" + run.ID + ":" + requestID)[:24]}
	// Simulate the crash window after the managed job was committed but before
	// its result was bound back to the repair. Replay must return that exact job.
	jobID := buildAssistantDigest("codex-install:" + in.RequestID)[:24]
	job := CodexInstallJob{ID: jobID, Status: "failed", Input: in, Result: CodexInstallResult{Version: in.Version, Error: "fixture download failed"}}
	if err = m.Store.Put("codex-install", jobID, codexInstallRecord{CodexInstallJob: job, RequestHash: buildAssistantDigest(in)}); err != nil {
		t.Fatal(err)
	}
	value, err := m.callSetupRepairTool(context.Background(), token, "setup_codex_install", setupRepairToolArgs{Method: "native", SourceGroup: group, NPMSourceID: source, RequestID: requestID})
	if err != nil || value.(CodexInstallJob).ID != jobID {
		t.Fatal("replay did not recover committed installation", value, err)
	}
	if _, err = m.callSetupRepairTool(context.Background(), token, "setup_codex_install", setupRepairToolArgs{Method: "npm", SourceGroup: group, NPMSourceID: source, RequestID: requestID}); err == nil {
		t.Fatal("same request ID accepted changed installation method")
	}
	var record setupRepairRecord
	_ = m.Store.Get("setup-repair", run.ID, &record)
	record.Attempts[store.ID()] = setupRepairAttempt{InstallID: store.ID()}
	record.Attempts[store.ID()] = setupRepairAttempt{InstallID: store.ID()}
	_ = m.Store.Put("setup-repair", run.ID, record)
	if _, err = m.callSetupRepairTool(context.Background(), token, "setup_codex_install", setupRepairToolArgs{Method: "native", SourceGroup: group, NPMSourceID: source, RequestID: store.ID()}); err == nil || !strings.Contains(err.Error(), "three") {
		t.Fatal("fourth install attempt was accepted", err)
	}
}

func TestSetupRepairToolArgumentsAreExplicit(t *testing.T) {
	for _, raw := range []string{`{"method":"native","sourceGroup":"international","npmSourceId":"x","requestId":"0123456789abcdef01234567","command":"sudo evil"}`, `{"method":"native"}`, `null`, `{"installId":"../foreign"}`} {
		name := "setup_codex_install"
		if strings.Contains(raw, "installId") {
			name = "setup_install_get"
		}
		if _, err := parseSetupRepairTool(name, json.RawMessage(raw)); err == nil {
			t.Fatalf("unsupported tool arguments accepted: %s", raw)
		}
	}
	if _, err := parseSetupRepairTool("host_shell", json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown tool accepted")
	}
	if _, err := parseSetupRepairTool("setup_codex_verify", json.RawMessage(fmt.Sprintf(`{"installId":%q}`, store.ID()))); err != nil {
		t.Fatal(err)
	}
}

func TestSetupRepairHTTPHasIndependentCapabilityBoundary(t *testing.T) {
	m, run, capability := setupRepairFixture(t)
	admin := "administrator-token-is-not-a-setup-capability"
	h := NewHandler(m, admin, true)
	for _, path := range []string{"/api/setup/kun", "/api/setup/repairs", "/api/setup/repairs/" + run.ID} {
		if r := request(h, "GET", path, "", "", ""); r.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d %s", path, r.Code, r.Body.String())
		}
		if r := request(h, "GET", path, "", admin, ""); r.Code != http.StatusOK {
			t.Fatalf("admin %s: %d %s", path, r.Code, r.Body.String())
		}
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	for _, token := range []string{"", admin, "rd-setup-bogus"} {
		r := request(h, "POST", "/api/setup/agent-mcp", body, token, "")
		if r.Code != http.StatusUnauthorized {
			t.Fatalf("non-capability got MCP access: %d %s", r.Code, r.Body.String())
		}
	}
	r := request(h, "POST", "/api/setup/agent-mcp", body, capability, "")
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "setup_codex_install") || strings.Contains(r.Body.String(), capability) {
		t.Fatal("task capability could not list its bounded tools", r.Code, r.Body.String())
	}
	// The transport capability must never grant the administrator API namespace.
	if r = request(h, "GET", "/api/setup/kun", "", capability, ""); r.Code != http.StatusUnauthorized {
		t.Fatal("MCP credential became an administrator", r.Code, r.Body.String())
	}
	var record setupRepairRecord
	_ = m.Store.Get("setup-repair", run.ID, &record)
	record.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	_ = m.Store.Put("setup-repair", run.ID, record)
	if r = request(h, "POST", "/api/setup/agent-mcp", body, capability, ""); r.Code != http.StatusUnauthorized {
		t.Fatal("expired transport accepted", r.Code, r.Body.String())
	}
}

func setupRepairRunningInstall(t *testing.T) (*Manager, SetupRepairRun, string, string) {
	t.Helper()
	m, run, token := setupRepairFixture(t)
	id := store.ID()
	job := CodexInstallJob{ID: id, Status: "running", Input: CodexInstallInput{Version: "0.157.1", Confirm: true}, Result: CodexInstallResult{Version: "0.157.1"}}
	if err := m.Store.Put("codex-install", id, codexInstallRecord{CodexInstallJob: job}); err != nil {
		t.Fatal(err)
	}
	activeCodexInstalls.Store(codexInstallKey(m, id), true)
	t.Cleanup(func() { activeCodexInstalls.Delete(codexInstallKey(m, id)) })
	var record setupRepairRecord
	_ = m.Store.Get("setup-repair", run.ID, &record)
	record.Attempts[store.ID()] = setupRepairAttempt{InstallID: id}
	record.AttemptIDs = append(record.AttemptIDs, id)
	if err := m.Store.Put("setup-repair", run.ID, record); err != nil {
		t.Fatal(err)
	}
	return m, run, token, id
}

func TestSetupRepairWaitBoundScopeCancellationAndRevocation(t *testing.T) {
	t.Run("deadline-and-foreign-scope", func(t *testing.T) {
		m, _, token, id := setupRepairRunningInstall(t)
		started := time.Now()
		if _, err := m.callSetupRepairTool(t.Context(), token, "setup_install_wait", setupRepairToolArgs{InstallID: store.ID(), WaitSeconds: 45}); err == nil || time.Since(started) > 200*time.Millisecond {
			t.Fatal("foreign installation was not rejected immediately", err)
		}
		started = time.Now()
		value, err := m.callSetupRepairTool(t.Context(), token, "setup_install_wait", setupRepairToolArgs{InstallID: id, WaitSeconds: 1})
		if err != nil || value.(CodexInstallJob).Status != "running" || time.Since(started) < 900*time.Millisecond || time.Since(started) > 2*time.Second {
			t.Fatal("wait did not return the still-running job at its deadline", value, err, time.Since(started))
		}
	})
	t.Run("request-canceled", func(t *testing.T) {
		m, _, token, id := setupRepairRunningInstall(t)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		if _, err := m.callSetupRepairTool(ctx, token, "setup_install_wait", setupRepairToolArgs{InstallID: id, WaitSeconds: 45}); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
			t.Fatal("wait ignored request cancellation", err, time.Since(started))
		}
	})
	t.Run("revoked-without-holding-admission-lock", func(t *testing.T) {
		m, run, token, id := setupRepairRunningInstall(t)
		done := make(chan error, 1)
		go func() {
			_, err := m.callSetupRepairTool(t.Context(), token, "setup_install_wait", setupRepairToolArgs{InstallID: id, WaitSeconds: 45})
			done <- err
		}()
		time.Sleep(50 * time.Millisecond)
		deadline := time.Now().Add(200 * time.Millisecond)
		locked := false
		for time.Now().Before(deadline) {
			if setupAgentMu.TryLock() {
				locked = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !locked {
			t.Fatal("wait retained the global setup admission lock")
		}
		var record setupRepairRecord
		_ = m.Store.Get("setup-repair", run.ID, &record)
		record.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
		err := m.Store.Put("setup-repair", run.ID, record)
		setupAgentMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("expired capability remained usable during wait")
			}
		case <-time.After(time.Second):
			t.Fatal("wait did not observe revoked authority within one poll interval")
		}
	})
	t.Run("manager-stopped", func(t *testing.T) {
		m, _, token, id := setupRepairRunningInstall(t)
		done := make(chan error, 1)
		go func() {
			_, err := m.callSetupRepairTool(t.Context(), token, "setup_install_wait", setupRepairToolArgs{InstallID: id, WaitSeconds: 45})
			done <- err
		}()
		m.cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("unexpected manager cancellation result", err)
			}
		case <-time.After(time.Second):
			t.Fatal("wait ignored manager shutdown")
		}
	})
	for _, raw := range []string{`{"installId":"0123456789abcdef01234567","waitSeconds":0}`, `{"installId":"0123456789abcdef01234567","waitSeconds":46}`, `{"installId":"0123456789abcdef01234567","waitSeconds":1.5}`} {
		if _, err := parseSetupRepairTool("setup_install_wait", json.RawMessage(raw)); err == nil {
			t.Fatal("invalid waitSeconds accepted", raw)
		}
	}
}
