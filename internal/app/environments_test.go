package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shengjuntu/rundesk/internal/tracequery"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeContainer struct {
	ID, Image, State string
	Labels           map[string]string
	Env              []string
	Workspace        string
	OOM              bool
}
type fakeDocker struct {
	mu           sync.Mutex
	containers   map[string]*fakeContainer
	calls        [][]string
	imageLabels  map[string]string
	missingImage string
	image        string
	unavailable  bool
	commands     int
	rootless     bool
	failCommand  bool
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{containers: map[string]*fakeContainer{}, image: "sha256:111111"}
}
func (f *fakeDocker) Run(ctx context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{}, args...))
	if f.unavailable {
		return nil, failure(503, "docker_unavailable", "Docker daemon unavailable")
	}
	j := func(v any) ([]byte, error) { return json.Marshal(v) }
	switch strings.Join(args[:2], " ") {
	case "version --format":
		return j(map[string]string{"Version": "fixture", "Os": "linux", "Arch": runtime.GOARCH})
	case "info --format":
		if f.rootless {
			return j([]string{"name=rootless"})
		}
		return j([]string{"name=seccomp"})
	case "image inspect":
		id := args[2]
		if id == f.missingImage {
			return nil, errors.New("fixture image missing")
		}
		if !strings.HasPrefix(id, "sha256:") {
			id = f.image
		}
		return j([]map[string]any{{"Id": id, "Os": "linux", "Architecture": runtime.GOARCH, "Created": "2026-10-01T12:00:00Z", "Size": 12345678, "RepoDigests": []string{"news@sha256:registry-manifest"}, "Config": map[string]any{"Labels": f.imageLabels, "Env": []string{"PRIVATE=do-not-expose"}}}})
	case "container ls":
		name := ""
		running := false
		for _, a := range args {
			if strings.HasPrefix(a, "name=^/") {
				name = strings.TrimSuffix(strings.TrimPrefix(a, "name=^/"), "$")
			}
			if a == "status=running" {
				running = true
			}
		}
		ids := []string{}
		for key, c := range f.containers {
			if (name == "" || name == key) && (!running || c.State == "running") {
				ids = append(ids, c.ID)
			}
		}
		return []byte(strings.Join(ids, "\n")), nil
	case "container inspect":
		c := f.containers[args[2]]
		if c == nil {
			return nil, errors.New("fixture missing container")
		}
		exit := 0
		if c.OOM {
			exit = 137
		}
		return j([]map[string]any{{"Id": c.ID, "Image": c.Image, "Config": map[string]any{"Labels": c.Labels}, "State": map[string]any{"Status": c.State, "OOMKilled": c.OOM, "ExitCode": exit}}})
	case "container create":
		name := ""
		c := &fakeContainer{Labels: map[string]string{}, State: "created"}
		for n, a := range args {
			if n+1 >= len(args) {
				continue
			}
			switch a {
			case "--name":
				name = args[n+1]
			case "--label":
				k, v, _ := strings.Cut(args[n+1], "=")
				c.Labels[k] = v
			case "--env":
				c.Env = append(c.Env, args[n+1])
			case "--workdir":
				c.Workspace = args[n+1]
			}
		}
		c.Image = args[len(args)-2]
		c.ID = fmt.Sprint("container-", len(f.containers)+1)
		f.containers[name] = c
		return []byte(c.ID), nil
	case "container start":
		c := f.containers[args[2]]
		c.State = "running"
		return []byte(c.ID), nil
	case "container stop":
		name := args[len(args)-1]
		if c := f.containers[name]; c != nil {
			c.State = "exited"
		}
		return []byte(name), nil
	case "container rm":
		delete(f.containers, args[2])
		return []byte(args[2]), nil
	}
	if args[0] == "exec" {
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected Docker call: %q", args)
}
func (f *fakeDocker) Command(args ...string) *exec.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{}, args...))
	f.commands++
	if f.failCommand {
		return exec.Command("/not-a-real-rundesk-executable")
	}
	var c *fakeContainer
	for _, a := range args {
		if candidate := f.containers[a]; candidate != nil {
			c = candidate
			break
		}
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "__demo_agent")
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Dir = c.Workspace
	return cmd
}
func dockerTestApp(t *testing.T) (*Manager, *fakeDocker, Instance, Workspace) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux containers")
	}
	m := testManager(t)
	f := newFakeDocker()
	m.docker = f
	i, e := m.CreateInstance(InstancePatch{Name: "News"})
	if e != nil {
		t.Fatal(e)
	}
	i, e = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "docker", Image: "news:v1"})
	if e != nil {
		t.Fatal(e)
	}
	return m, f, i, m.Workspaces()[0]
}
func expectDockerCode(t *testing.T, e error, want string) {
	t.Helper()
	var typed *apiError
	if !errors.As(e, &typed) || typed.Code != want {
		t.Fatalf("want %s, got %v", want, e)
	}
}
func TestDockerEnvironmentBindingsAndSafeMounts(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	s, e := m.CreateSession(w.ID, "A", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	s2, e := m.CreateSession(w.ID, "B", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	if s.EnvironmentID == "" || s.EnvironmentID != s2.EnvironmentID || s.ExecutionMode != "docker" {
		t.Fatal("sessions did not reuse environment")
	}
	if len(f.calls) != 0 {
		t.Fatal("metadata started Docker")
	}
	other, e := m.CreateWorkspace("other", "")
	if e != nil {
		t.Fatal(e)
	}
	third, e := m.CreateSession(other.ID, "C", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	if third.EnvironmentID == s.EnvironmentID {
		t.Fatal("project environments shared")
	}
	if _, e = m.Start(s.ID, Input{Text: "hello"}); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, s.ID, "completed")
	if _, e = m.Start(s2.ID, Input{Text: "hello"}); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, s2.ID, "completed")
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.containers) != 1 || f.commands != 2 {
		t.Fatalf("wanted one container/two connections: %d/%d", len(f.containers), f.commands)
	}
	for _, args := range f.calls {
		if strings.Join(args[:2], " ") != "container create" {
			continue
		}
		joined := strings.Join(args, " ")
		for _, required := range []string{"--init", "--read-only", "--cap-drop ALL", "no-new-privileges:true", "--memory 2048m", "--pids-limit 256", "--log-driver local", "--restart no", "type=bind,src=" + w.Path} {
			if !strings.Contains(joined, required) {
				t.Fatal("missing", required)
			}
		}
		for n, a := range args {
			if a == "--mount" {
				v := args[n+1]
				if strings.Contains(v, "state.db") || strings.Contains(v, "docker.sock") || strings.Contains(v, "src="+m.Data+",") {
					t.Fatal("unsafe mount", v)
				}
			}
		}
		if strings.Contains(joined, "--privileged") || strings.Contains(joined, "RUNDESK_TOKEN") {
			t.Fatal("unsafe runtime")
		}
	}
}
func TestDockerPersistentDataAndPinnedImage(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	v, e := m.EnsureApplicationEnvironment(i.ID, w.ID)
	if e != nil {
		t.Fatal(e)
	}
	root, _, e := m.skillRoot(w.ID, []string{i.ID, "instance"})
	if e != nil || root != v.CodexHome || root == i.CodexHome {
		t.Fatal("incorrect skills home", root, e)
	}
	file := filepath.Join(root, "auth.json")
	if e = os.WriteFile(file, []byte("example-private-state"), 0600); e != nil {
		t.Fatal(e)
	}
	v, e = m.EnvironmentAction(v.ID, "start", v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	pinned := v.ImageID
	f.mu.Lock()
	f.image = "sha256:222222"
	f.mu.Unlock()
	v, e = m.EnvironmentAction(v.ID, "remove", v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	v, e = m.EnvironmentAction(v.ID, "start", v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if v.ImageID != pinned {
		t.Fatal("tag change replaced pinned image")
	}
	v, e = m.EnvironmentAction(v.ID, "recreate", v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if v.ImageID == pinned {
		t.Fatal("recreate did not resolve updated tag")
	}
	b, e := os.ReadFile(file)
	if e != nil || string(b) != "example-private-state" {
		t.Fatal("data lost", e)
	}
	_, e = m.EnvironmentAction(v.ID, "stop", v.Revision-1)
	expectDockerCode(t, e, "revision_conflict")
}
func TestDockerBusyEnvironmentAndSessionCancellation(t *testing.T) {
	m, _, i, w := dockerTestApp(t)
	s, e := m.CreateSession(w.ID, "approval", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(s.ID, Input{Text: "审批"}); e != nil {
		t.Fatal(e)
	}
	s = waitState(t, m, s.ID, "waiting")
	v := m.Environments(i.ID)[0]
	_, e = m.EnvironmentAction(v.ID, "stop", v.Revision)
	expectDockerCode(t, e, "environment_busy")
	_, e = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "local"})
	expectDockerCode(t, e, "execution_busy")
	sibling, e := m.CreateSession(w.ID, "sibling", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(sibling.ID, Input{Text: "审批"}); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, sibling.ID, "waiting")
	if e = m.StopRun(s.ID, s.RunID); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, s.ID, "interrupted")
	still, _ := m.Session(sibling.ID)
	if still.Status != "waiting" {
		t.Fatal("stopping one session affected sibling")
	}
	a := m.Approvals(sibling.ID)
	if len(a) != 1 {
		t.Fatal("approval lost")
	}
	if e = m.Approve(sibling.ID, a[0].ID, "accept", nil, nil); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, sibling.ID, "completed")
	v = m.Environments(i.ID)[0]
	v, e = m.EnvironmentAction(v.ID, "stop", v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if v.State != "exited" || m.environmentRefs(v.ID).Load() != 0 {
		t.Fatal("stop leaked connections")
	}
	files, e := os.ReadDir(filepath.Join(m.environmentRoot(v.ID), "leases"))
	if e != nil || len(files) != 0 {
		t.Fatal("leases leaked", e)
	}
}
func TestDockerNativeCompatibilityAndHistoryBinding(t *testing.T) {
	m := testManager(t)
	f := newFakeDocker()
	m.docker = f
	w := m.Workspaces()[0]
	d, _ := m.Instance()
	_, e := m.SetExecution(DefaultInstance, d.Revision, ExecutionSpec{Mode: "docker", Image: "news:v1"})
	expectDockerCode(t, e, "assistant_requires_local")
	i, e := m.CreateInstance(InstancePatch{Name: "legacy"})
	if e != nil {
		t.Fatal(e)
	}
	old, e := m.CreateSession(w.ID, "old", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	i, e = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "docker", Image: "news:v1"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.Start(old.ID, Input{Text: "hello"})
	expectDockerCode(t, e, "session_execution_changed")
	if len(f.calls) != 0 {
		t.Fatal("wrong history launched Docker")
	}
	fresh, e := m.CreateSession(w.ID, "new", "", i.ID)
	if e != nil || fresh.EnvironmentID == "" {
		t.Fatal("new session not bound", e)
	}
	_, e = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "local"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(old.ID, Input{Text: "hello"}); e != nil {
		t.Fatal(e)
	}
	waitState(t, m, old.ID, "completed")
}
func TestDockerOwnershipIsolationAndDaemonErrors(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	v, e := m.EnsureApplicationEnvironment(i.ID, w.ID)
	if e != nil {
		t.Fatal(e)
	}
	other, e := m.CreateInstance(InstancePatch{Name: "other"})
	if e != nil {
		t.Fatal(e)
	}
	other, e = m.SetExecution(other.ID, other.Revision, ExecutionSpec{Mode: "docker", Image: "other:v1"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.CreateSession(w.ID, "bad", "", other.ID)
	expectDockerCode(t, e, "workspace_already_bound")
	f.mu.Lock()
	f.unavailable = true
	f.mu.Unlock()
	_, e = m.EnvironmentAction(v.ID, "start", v.Revision)
	expectDockerCode(t, e, "docker_unavailable")
	v = m.Environments(i.ID)[0]
	if v.Error == "" {
		t.Fatal("error not retained")
	}
	f.mu.Lock()
	f.unavailable = false
	f.containers[v.ContainerName] = &fakeContainer{ID: "alien", State: "running", Labels: map[string]string{}}
	f.mu.Unlock()
	_, e = m.EnvironmentAction(v.ID, "remove", v.Revision)
	expectDockerCode(t, e, "container_owner_mismatch")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.containers[v.ContainerName] == nil {
		t.Fatal("foreign container removed")
	}
}
func TestDockerTraceSnapshotScope(t *testing.T) {
	m, _, i, w := dockerTestApp(t)
	s, e := m.CreateSession(w.ID, "source", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(s.ID, Input{Text: "hello"}); e != nil {
		t.Fatal(e)
	}
	s = waitState(t, m, s.ID, "completed")
	other, e := m.CreateSession(w.ID, "unrelated", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	m.event(other.ID, "internal", "private", map[string]string{"secret": "unrelated-record"})
	analysis, e := m.CreateTraceAnalysis(TraceSelection{SessionID: s.ID, RunID: s.RunID})
	if e != nil {
		t.Fatal(e)
	}
	params := map[string]any{}
	if e = m.configureTraceAnalysis(analysis, params); e != nil {
		t.Fatal(e)
	}
	config := params["config"].(map[string]any)["mcp_servers.rundesk_trace"].(map[string]any)
	args := config["args"].([]string)
	if config["command"] != "/opt/rundesk/bin/rundesk" || args[1] == filepath.Join(m.Data, "state.db") {
		t.Fatal("full database exposed")
	}
	reader, e := tracequery.Open(args[1], s.ID, analysis.TraceOrigin.Through)
	if e != nil {
		t.Fatal(e)
	}
	runs, e := reader.Runs()
	reader.Close()
	if e != nil || len(runs) != 1 {
		t.Fatal("snapshot unreadable", e)
	}
	if r, e := tracequery.Open(args[1], other.ID, analysis.TraceOrigin.Through); e == nil {
		r.Close()
		t.Fatal("other session exposed")
	}
	m.event(s.ID, "internal", "later", map[string]string{"text": "later-event"})
	b, e := os.ReadFile(args[1])
	if e != nil || strings.Contains(string(b), "unrelated-record") || strings.Contains(string(b), "later-event") {
		t.Fatal("snapshot scope violated", e)
	}
}
func TestDockerChangedSpecKeepsOldEnvironmentUntilExplicitRecreate(t *testing.T) {
	m, _, i, w := dockerTestApp(t)
	v, e := m.EnsureApplicationEnvironment(i.ID, w.ID)
	if e != nil {
		t.Fatal(e)
	}
	spec := i.Execution
	spec.Image = "news:v2"
	i, e = m.SetExecution(i.ID, i.Revision, spec)
	if e != nil {
		t.Fatal(e)
	}
	_, _, cleanup, e := m.prepareCommand(w, i)
	if cleanup != nil {
		cleanup()
	}
	if e != nil {
		t.Fatal(e)
	}
	v, e = m.EnvironmentAction(v.ID, "start", v.Revision)
	if e != nil || v.Spec.Image != "news:v1" {
		t.Fatal("old environment changed", e)
	}
	v, e = m.EnvironmentAction(v.ID, "recreate", v.Revision)
	if e != nil || v.Spec.Image != "news:v2" {
		t.Fatal("recreate did not use desired image", e)
	}
}
func TestDockerLeaseCleanupOnRPCStartFailure(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	f.failCommand = true
	s, e := m.CreateSession(w.ID, "failure", "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(s.ID, Input{Text: "hello"}); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, _ = m.Session(s.ID)
		if s.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Status != "failed" {
		t.Fatal(s.Status)
	}
	if m.environmentRefs(s.EnvironmentID).Load() != 0 {
		t.Fatal("failed RPC leaked lease")
	}
}
func TestDockerIdleStopAndOOMStatus(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	v, e := m.EnsureApplicationEnvironment(i.ID, w.ID)
	if e != nil {
		t.Fatal(e)
	}
	v, e = m.EnvironmentAction(v.ID, "start", v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	m.environmentMu.Lock()
	v.LastUsed = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	m.saveEnvironmentLocked(&v)
	m.environmentMu.Unlock()
	m.reapEnvironments()
	v = m.Environments(i.ID)[0]
	if v.State != "exited" {
		t.Fatal("idle container not stopped")
	}
	f.mu.Lock()
	f.containers[v.ContainerName].OOM = true
	f.mu.Unlock()
	v, e = m.EnvironmentAction(v.ID, "inspect", 0)
	if e != nil || !v.OOMKilled || v.ExitCode != 137 {
		t.Fatal("OOM unavailable", e)
	}
}
func TestDockerUnsafeMountsAndCLIEndpoint(t *testing.T) {
	m, _, i, _ := dockerTestApp(t)
	w, e := m.CreateWorkspace("unsafe", m.Data)
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.CreateSession(w.ID, "bad", "", i.ID)
	expectDockerCode(t, e, "unsafe_docker_workspace")
	if _, e = mountArg("/tmp/a,b", "/project", false); e == nil {
		t.Fatal("mount option injection accepted")
	}
	t.Setenv("RUNDESK_DOCKER_HOST", "tcp://other:2375")
	_, e = (dockerCLI{}).Run(context.Background(), "version")
	expectDockerCode(t, e, "docker_endpoint_unsupported")
}

func TestDockerAPIIsAdminOnlyAndHasStructuredErrors(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	a, e := m.RegisterApplication("docker-news", ApplicationInput{Name: "Docker News", InstanceID: i.ID, WorkspaceID: w.ID})
	if e != nil {
		t.Fatal(e)
	}
	_, secret, e := m.CreateApplicationKey(a.AppID, KeyInput{Name: "app", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read", "run"}})
	if e != nil {
		t.Fatal(e)
	}
	h := NewHandler(m, "admin-token-012345678901234567890", true)
	for _, p := range []string{"/docker/status", "/environments", "/instances/" + i.ID + "/execution"} {
		method := "GET"
		body := ""
		if strings.HasSuffix(p, "/execution") {
			method = "PUT"
			body = `{"revision":0,"spec":{"mode":"local"}}`
		}
		r := appRequest(h, method, "/api/v1"+p, body, secret, "")
		if r.Code != 403 {
			t.Fatal("application controlled Docker", p, r.Code, r.Body.String())
		}
	}
	admin := NewHandler(m, "", true)
	r := v1Request(admin, "PUT", "/instances/"+i.ID+"/execution", `{"spec":{"mode":"docker","image":"news:v1"}}`, "")
	if r.Code != 400 || object(t, r)["code"] != "revision_required" {
		t.Fatal(r.Code, r.Body.String())
	}
	f.mu.Lock()
	f.unavailable = true
	f.mu.Unlock()
	r = v1Request(admin, "GET", "/docker/status", "", "")
	if r.Code != 503 || object(t, r)["requestId"] == "" || object(t, r)["details"] == nil {
		t.Fatal("Docker failure missing details", r.Code, r.Body.String())
	}
}
func TestDockerEnvironmentRestartAndQueueBinding(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux containers")
	}
	data := t.TempDir()
	m, e := New(data, "", true)
	if e != nil {
		t.Fatal(e)
	}
	m.docker = newFakeDocker()
	w := m.Workspaces()[0]
	i, e := m.CreateInstance(InstancePatch{Name: "restart"})
	if e != nil {
		t.Fatal(e)
	}
	i, e = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "docker", Image: "news:v1"})
	if e != nil {
		t.Fatal(e)
	}
	q := m.Queue()
	q.Paused = true
	if _, e = m.SaveQueue(q); e != nil {
		t.Fatal(e)
	}
	task, e := m.Enqueue(TaskSpec{WorkspaceID: w.ID, InstanceID: i.ID, Input: Input{Text: "hello"}})
	if e != nil {
		t.Fatal(e)
	}
	s, _ := m.Session(task.SessionID)
	v := m.Environments(i.ID)[0]
	if s.EnvironmentID != v.ID {
		t.Fatal("queue lost environment binding")
	}
	lease := filepath.Join(m.environmentRoot(v.ID), "leases", "stale")
	os.WriteFile(lease, nil, 0600)
	m.Close()
	next, e := New(data, "", true)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	next.docker = newFakeDocker()
	restored := next.Environments(i.ID)[0]
	if restored.ID != v.ID || restored.CodexHome != v.CodexHome || restored.State != "unknown" {
		t.Fatal("environment identity lost")
	}
	if _, e = os.Stat(lease); !os.IsNotExist(e) {
		t.Fatal("stale lease survived restart")
	}
	q = next.Queue()
	q.Paused = false
	if _, e = next.SaveQueue(q); e != nil {
		t.Fatal(e)
	}
	waitState(t, next, s.ID, "completed")
}

func TestDockerUserMapping(t *testing.T) {
	user, e := containerUser(1000, 1001, []string{"name=seccomp"})
	if e != nil || user != "1000:1001" {
		t.Fatal(user, e)
	}
	user, e = containerUser(1000, 1001, []string{"name=rootless"})
	if e != nil || user != "0:0" {
		t.Fatal(user, e)
	}
	_, e = containerUser(1000, 1001, []string{"name=userns"})
	expectDockerCode(t, e, "docker_userns_unsupported")
}
func TestDockerExecutionAPIRequiresExplicitMode(t *testing.T) {
	m, _, i, _ := dockerTestApp(t)
	h := NewHandler(m, "", true)
	for _, body := range []string{fmt.Sprintf(`{"revision":%d}`, i.Revision), fmt.Sprintf(`{"revision":%d,"spec":{}}`, i.Revision)} {
		r := v1Request(h, "PUT", "/instances/"+i.ID+"/execution", body, "")
		if r.Code != 400 {
			t.Fatal("omitted mode changed runtime", r.Code, r.Body.String())
		}
	}
	actual, _ := m.Instance(i.ID)
	if actual.Execution.Mode != "docker" {
		t.Fatal("runtime changed")
	}
}
