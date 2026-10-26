package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

// This fixture exercises Docker admission and queue behavior, not a real
// daemon, model inference, upstream tools, or actual media rendering.
type buildCenterDockerFixture struct {
	base      *fakeDocker
	mu        sync.Mutex
	calls     [][]string
	deleted   []string
	smokeFail bool
	probeFail bool
}

func (f *buildCenterDockerFixture) record(args []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{}, args...))
}
func (f *buildCenterDockerFixture) Run(ctx context.Context, args ...string) ([]byte, error) {
	f.record(args)
	if len(args) > 1 && args[0] == "image" && args[1] == "rm" {
		if len(args) != 4 || args[2] != "--no-prune" || !strings.HasPrefix(args[3], "sha256:") {
			return nil, errors.New("unsafe image removal fixture call")
		}
		f.mu.Lock()
		f.deleted = append(f.deleted, args[3])
		f.mu.Unlock()
		f.base.mu.Lock()
		f.base.missingImage = args[3]
		f.base.mu.Unlock()
		return []byte("deleted"), nil
	}
	if len(args) > 2 && args[0] == "container" && args[1] == "inspect" {
		// Docker ls --no-trunc and inspect return the same full identity.
		f.base.mu.Lock()
		name := args[2]
		for key, c := range f.base.containers {
			if c.ID == name {
				name = key
				break
			}
		}
		f.base.mu.Unlock()
		return f.base.Run(ctx, "container", "inspect", name)
	}
	if len(args) > 0 && args[0] == "rm" {
		return nil, nil // cleanup of the unique temporary probe only
	}
	f.mu.Lock()
	probeFail := f.probeFail
	f.mu.Unlock()
	if probeFail && len(args) > 2 && args[0] == "exec" && args[len(args)-1] == "__container_probe" {
		return nil, errors.New("fixture missing Codex")
	}
	return f.base.Run(ctx, args...)
}
func (f *buildCenterDockerFixture) Command(args ...string) *exec.Cmd {
	f.record(args)
	f.mu.Lock()
	fail := f.smokeFail
	f.mu.Unlock()
	if fail {
		return exec.Command("sh", "-c", "printf '%s\n' 'fixture declared smoke failure'; exit 7")
	}
	return exec.Command("sh", "-c", "printf '%s\n' 'fixture smoke runner; no real Docker, model, or media rendering'")
}
func buildCenterTestApp(t *testing.T) (*Manager, *buildCenterDockerFixture, Instance, Workspace, ImageVersion) {
	t.Helper()
	m, base, i, w := dockerTestApp(t)
	f := &buildCenterDockerFixture{base: base}
	m.docker = f
	v, err := m.RegisterImage(i.ID, "news:v1")
	if err != nil {
		t.Fatal(err)
	}
	return m, f, i, w, v
}
func buildCenterFixtureCalls(f *buildCenterDockerFixture) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string{}, f.calls...)
}

func buildCenterFixtureAssistantGrant(t *testing.T, m *Manager, iid string, w Workspace, runID string) {
	t.Helper()
	session, err := m.CreateSession(w.ID, "Build scope fixture", "", iid)
	if err != nil {
		t.Fatal(err)
	}
	id := store.ID()
	task := Task{ID: id, SessionID: session.ID, Status: "queued", Spec: TaskSpec{InstanceID: iid, WorkspaceID: w.ID, NotBefore: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}}
	session.TaskID = id
	m.mu.Lock()
	m.sessions[session.ID].TaskID = id
	m.mu.Unlock()
	m.queueMu.Lock()
	m.tasks[id] = task
	m.queueMu.Unlock()
	capability := buildAssistantCapability{RunID: runID, SessionID: session.ID, TaskID: id, InstanceID: iid, WorkspaceID: w.ID, WorkspacePath: w.Path, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	record := buildAssistantRecord{BuildAssistantRun: BuildAssistantRun{ID: runID, InstanceID: iid, WorkspaceID: w.ID, SessionID: session.ID, TaskID: id, ExpiresAt: capability.ExpiresAt}, CapabilityHash: "fixture-" + runID, WorkspacePath: w.Path}
	if err = m.Store.PutMany(store.Record{Kind: "build-assistant", ID: runID, Value: record}, store.Record{Kind: "build-assistant-capability", ID: record.CapabilityHash, Value: capability}, store.Record{Kind: "task", ID: id, Value: task}, store.Record{Kind: "session", ID: session.ID, Value: session}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCenterImagesCatalogDeduplicatesWithoutHostScan(t *testing.T) {
	m, f, i, _, registered := buildCenterTestApp(t)
	j, err := m.CreateInstance(InstancePatch{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.RegisterImage(j.ID, "other:v2"); err != nil {
		t.Fatal(err)
	}
	b := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: "recipe", RecipeID: "custom-example", RecipeRevision: 3, Name: "Reusable capability", Status: "succeeded", ImageID: registered.ImageID, Reference: "tools:stable", Created: store.Now()}
	if err = m.Store.Put("image-build", b.ID, b); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	catalog, err := m.BuildCenterImages()
	if err != nil {
		t.Fatal(err)
	}
	rows := catalog["items"].([]BuildCenterImage)
	if len(rows) != 1 || len(rows[0].Registrations) != 2 || len(rows[0].Sources) != 3 || len(rows[0].References) != 3 {
		t.Fatalf("catalog did not aggregate immutable identity: %+v", rows)
	}
	if rows[0].Key != BuildCenterImageKey(registered.ImageID) || rows[0].ImageID != registered.ImageID {
		t.Fatal("catalog identity changed")
	}
	if len(buildCenterFixtureCalls(f)) != before {
		t.Fatal("catalog unexpectedly queried Docker")
	}
	data, _ := json.Marshal(rows)
	if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), i.CodexHome) {
		t.Fatal("catalog leaked unrelated image config or HOME")
	}
}

func TestBuildCenterImagesUnavailableCheckIsNotDeletionOrRenderProof(t *testing.T) {
	m, f, _, _, v := buildCenterTestApp(t)
	f.base.mu.Lock()
	f.base.missingImage = v.ImageID
	f.base.mu.Unlock()
	checked, err := m.CheckBuildCenterImage(BuildCenterImageKey(v.ImageID))
	if err != nil || checked.Availability != "unavailable" || checked.CheckedAt == "" || checked.Error == "" || checked.LastTest != nil {
		t.Fatalf("missing image was not reported accurately: %+v %v", checked, err)
	}
	_, err = m.DeleteBuildCenterImage(checked.Key, BuildCenterImageDeleteInput{ImageID: v.ImageID, RequestID: store.ID()})
	expectDockerCode(t, err, "image_delete_unconfirmed")
	if len(f.deleted) != 0 {
		t.Fatal("missing image was treated as a successful deletion")
	}
}

func TestBuildCenterImagesFailedBuildArtifactsRemainManageable(t *testing.T) {
	m, f, _, _, _ := buildCenterTestApp(t)
	b := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: "recipe", Status: "failed", ImageID: "sha256:333333", Reference: "failed:smoke", Name: "Failed actual verification", Error: "declared render command failed", Created: store.Now()}
	if err := m.Store.Put("image-build", b.ID, b); err != nil {
		t.Fatal(err)
	}
	key := BuildCenterImageKey(b.ImageID)
	image, err := m.BuildCenterImage(key)
	if err != nil || image.Availability != "unverified" || image.Error == "" || len(image.Sources) != 1 || image.Sources[0].Status != "failed" {
		t.Fatalf("failed build artifact was hidden or marked verified: %+v %v", image, err)
	}
	before := len(m.Instances())
	_, err = m.LaunchBuildCenterImage(key, BuildCenterImageLaunchInput{Name: "Cannot silently deploy failed build", RequestID: store.ID()})
	expectDockerCode(t, err, "image_build_unverified")
	if len(m.Instances()) != before {
		t.Fatal("created an instance from a failed-only artifact")
	}
	result, err := m.DeleteBuildCenterImage(key, BuildCenterImageDeleteInput{ImageID: b.ImageID, RequestID: store.ID()})
	if err != nil || !result.Deleted || result.RegistrationCount != 0 || len(f.deleted) != 1 || f.deleted[0] != b.ImageID {
		t.Fatalf("failed build artifact could not be cleaned up: %+v %v", result, err)
	}
}

func TestBuildCenterImagesArchivedLastBuildSourceKeepsImageManageable(t *testing.T) {
	for _, scope := range []string{"setup", "recipe"} {
		t.Run(scope, func(t *testing.T) {
			m, f, _, _, _ := buildCenterTestApp(t)
			b := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: scope, Status: "succeeded", ImageID: "sha256:333333", Reference: "archived:local", Name: "/private/context Authorization: Bearer do-not-preserve", Dockerfile: "/private/context/Dockerfile", Error: "password=do-not-preserve", Created: store.Now(), RecipeSmoke: &RecipeSmokeSpec{Command: []string{"echo", "secret=do-not-preserve"}}}
			if scope == "setup" {
				b.ProfileID = "news2douyin"
			} else {
				b.RecipeID, b.RecipeRevision = "custom-"+store.ID(), 1
			}
			if err := m.Store.Put("image-build", b.ID, b); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(m.buildDir(b.ID), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(m.buildDir(b.ID), "build.log"), []byte("secret=do-not-preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := m.DeleteBuild(DefaultInstance, b.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Build(DefaultInstance, b.ID); err == nil {
				t.Fatal("build history was not deleted")
			}
			if _, err := os.Stat(m.buildDir(b.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("build directory was retained: %v", err)
			}
			var archive buildCenterArchivedImageSource
			if err := m.Store.Get("build-center-image-source", b.ID, &archive); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(archive)
			if strings.Contains(string(data), "do-not-preserve") || strings.Contains(string(data), "/private/context") || strings.Contains(string(data), "recipeSmoke") {
				t.Fatalf("archive retained private build details: %s", data)
			}
			key := BuildCenterImageKey(b.ImageID)
			image, err := m.BuildCenterImage(key)
			if err != nil || image.ImageID != b.ImageID || image.Availability != "unverified" || len(image.Sources) != 1 || image.Sources[0].Kind != "archived-build" || image.Sources[0].Status != "succeeded" {
				t.Fatalf("last source deletion lost the artifact: %+v %v", image, err)
			}
			_, err = m.StartBuildCenterImageTest(key, BuildCenterImageTestInput{Kind: "smoke", RequestID: store.ID()})
			expectDockerCode(t, err, "image_test_unavailable")
			deleted, err := m.DeleteBuildCenterImage(key, BuildCenterImageDeleteInput{ImageID: b.ImageID, RequestID: store.ID()})
			if err != nil || !deleted.Deleted || deleted.RegistrationCount != 0 || len(f.deleted) != 1 || f.deleted[0] != b.ImageID {
				t.Fatalf("archived artifact could not be explicitly deleted: %+v %v", deleted, err)
			}
		})
	}
}

func TestBuildCenterImagesDeleteProtectsForeignContainersAndPinnedSelection(t *testing.T) {
	m, f, i, _, v := buildCenterTestApp(t)
	key := BuildCenterImageKey(v.ImageID)
	f.base.mu.Lock()
	f.base.image = "sha256:222222" // the unpinned instance now selects a different image
	f.base.containers["foreign"] = &fakeContainer{ID: strings.Repeat("c", 64), Image: v.ImageID, State: "exited", Labels: map[string]string{}}
	f.base.mu.Unlock()
	_, err := m.DeleteBuildCenterImage(key, BuildCenterImageDeleteInput{ImageID: v.ImageID, RequestID: store.ID()})
	expectDockerCode(t, err, "image_in_use")
	if len(f.deleted) != 0 {
		t.Fatal("deleted image referenced by a stopped foreign container")
	}
	f.base.mu.Lock()
	delete(f.base.containers, "foreign")
	f.base.mu.Unlock()
	if _, err = m.SelectImage(i.ID, v.ID, i.Revision); err != nil {
		t.Fatal(err)
	}
	_, err = m.DeleteBuildCenterImage(key, BuildCenterImageDeleteInput{ImageID: v.ImageID, RequestID: store.ID()})
	expectDockerCode(t, err, "image_in_use")
}

func TestBuildCenterImagesDeleteUsesImmutableIDAndPreservesData(t *testing.T) {
	m, f, i, w, v := buildCenterTestApp(t)
	for _, path := range []string{filepath.Join(i.CodexHome, "keep.txt"), filepath.Join(w.Path, "keep.txt")} {
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.base.mu.Lock()
	f.base.image = "sha256:222222" // the historical tag has moved elsewhere
	f.base.containers["foreign"] = &fakeContainer{ID: strings.Repeat("d", 64), Image: f.base.image, State: "running", Labels: map[string]string{}}
	f.base.mu.Unlock()
	input := BuildCenterImageDeleteInput{ImageID: v.ImageID, RequestID: store.ID()}
	deleted, err := m.DeleteBuildCenterImage(BuildCenterImageKey(v.ImageID), input)
	if err != nil || !deleted.Deleted || deleted.ImageID != v.ImageID || deleted.RegistrationCount != 1 {
		t.Fatalf("delete result: %+v %v", deleted, err)
	}
	if len(f.deleted) != 1 || f.deleted[0] != v.ImageID {
		t.Fatal("deleted a moved tag target or duplicated mutation")
	}
	if _, err = m.DeleteBuildCenterImage(BuildCenterImageKey(v.ImageID), input); err != nil || len(f.deleted) != 1 {
		t.Fatalf("duplicate request was not a read: %v", err)
	}
	input.ImageID = "sha256:222222"
	_, err = m.DeleteBuildCenterImage(BuildCenterImageKey(v.ImageID), input)
	expectDockerCode(t, err, "request_id_conflict")
	for _, path := range []string{filepath.Join(i.CodexHome, "keep.txt"), filepath.Join(w.Path, "keep.txt")} {
		if data, e := os.ReadFile(path); e != nil || string(data) != "keep" {
			t.Fatalf("user data changed: %s %v", path, e)
		}
	}
	checked, err := m.BuildCenterImage(BuildCenterImageKey(v.ImageID))
	if err != nil || checked.Availability != "deleted" || len(checked.Registrations) != 1 {
		t.Fatalf("historical registration should remain visible: %+v %v", checked, err)
	}
	if _, err = m.inspectImage("news:v1"); err != nil {
		t.Fatalf("new tag target was deleted: %v", err)
	}
	for _, args := range buildCenterFixtureCalls(f) {
		if len(args) > 1 && args[0] == "container" && args[1] == "ls" && strings.Contains(strings.Join(args, " "), "-a") && !strings.Contains(strings.Join(args, " "), "--no-trunc") {
			t.Fatal("container identity check used truncated IDs")
		}
	}
}

func TestBuildCenterImagesDeleteBlocksQueuedTagRebuildAndEnvironmentSnapshot(t *testing.T) {
	m, f, i, w, v := buildCenterTestApp(t)
	f.base.mu.Lock()
	f.base.image = "sha256:222222"
	f.base.mu.Unlock()
	b := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: "setup", Status: "canceling", Reference: v.Reference, Created: store.Now()}
	// canceling is active for admission, but the worker cannot take this fixture.
	m.buildMu.Lock()
	if err := m.Store.Put("image-build", b.ID, b); err != nil {
		t.Fatal(err)
	}
	m.buildMu.Unlock()
	_, err := m.DeleteBuildCenterImage(BuildCenterImageKey(v.ImageID), BuildCenterImageDeleteInput{ImageID: v.ImageID, RequestID: store.ID()})
	expectDockerCode(t, err, "image_in_use")
	m.buildMu.Lock()
	_ = m.Store.Delete("image-build", b.ID)
	m.buildMu.Unlock()
	i, err = m.SelectImage(i.ID, v.ID, i.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.EnsureApplicationEnvironment(i.ID, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "local"}); err != nil {
		t.Fatal(err)
	}
	_, err = m.DeleteBuildCenterImage(BuildCenterImageKey(v.ImageID), BuildCenterImageDeleteInput{ImageID: v.ImageID, RequestID: store.ID()})
	expectDockerCode(t, err, "image_in_use")
}

func TestBuildCenterImagesLaunchCreatesDedicatedResourcesAndDoesNotRunModel(t *testing.T) {
	m, f, _, _, v := buildCenterTestApp(t)
	beforeInstances, beforeWorkspaces := len(m.Instances()), len(m.Workspaces())
	input := BuildCenterImageLaunchInput{Name: "Reusable tools", RequestID: store.ID()}
	result, err := m.LaunchBuildCenterImage(BuildCenterImageKey(v.ImageID), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.ID == DefaultInstance || result.Instance.Execution.ImageID != v.ImageID || result.Environment.State != "running" || result.Environment.ImageID != v.ImageID || !within(filepath.Join(m.Data, "workspaces"), result.Workspace.Path) {
		t.Fatalf("invalid launch binding: %+v", result)
	}
	if len(m.Instances()) != beforeInstances+1 || len(m.Workspaces()) != beforeWorkspaces+1 {
		t.Fatal("did not create exactly one dedicated instance/workspace")
	}
	replayed, err := m.LaunchBuildCenterImage(BuildCenterImageKey(v.ImageID), input)
	if err != nil || replayed.Instance.ID != result.Instance.ID || len(m.Instances()) != beforeInstances+1 {
		t.Fatalf("launch replay duplicated resources: %+v %v", replayed, err)
	}
	for _, args := range buildCenterFixtureCalls(f) {
		if len(args) > 0 && (args[0] == "run" || strings.Contains(strings.Join(args, " "), "app-server")) {
			t.Fatalf("launch unexpectedly sent an agent task: %q", args)
		}
	}
	if len(m.Sessions()) != 0 {
		t.Fatal("launch created a model task")
	}
}

func TestBuildCenterImagesLaunchRejectsApplicationReservationsAndKeepsPartialFailure(t *testing.T) {
	m, f, i, w, v := buildCenterTestApp(t)
	i, err := m.SelectImage(i.ID, v.ID, i.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.RegisterApplication("reserved", ApplicationInput{Name: "Reserved app", InstanceID: i.ID, WorkspaceID: w.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = m.LaunchBuildCenterImage(BuildCenterImageKey(v.ImageID), BuildCenterImageLaunchInput{Name: "Borrow", InstanceID: i.ID, WorkspaceID: w.ID, RequestID: store.ID()})
	expectDockerCode(t, err, "application_binding_conflict")
	f.mu.Lock()
	f.probeFail = true
	f.mu.Unlock()
	input := BuildCenterImageLaunchInput{Name: "Missing Codex fixture", RequestID: store.ID()}
	partial, err := m.LaunchBuildCenterImage(BuildCenterImageKey(v.ImageID), input)
	expectDockerCode(t, err, "docker_runtime_invalid")
	if partial.Instance.ID == "" || partial.Workspace.ID == "" || !strings.Contains(err.Error(), partial.Instance.ID) || !strings.Contains(err.Error(), partial.Workspace.ID) {
		t.Fatalf("failed launch lost created resource identities: %+v %v", partial, err)
	}
	count := len(m.Instances())
	replayed, replayErr := m.LaunchBuildCenterImage(BuildCenterImageKey(v.ImageID), input)
	if replayErr == nil || replayed.Instance.ID != partial.Instance.ID || replayed.Workspace.ID != partial.Workspace.ID || len(m.Instances()) != count {
		t.Fatalf("failed launch retry duplicated resources or lost identities: %+v %v", replayed, replayErr)
	}
}

func TestBuildCenterImagesAsyncTestSharesQueueAndFreezesOwnerScope(t *testing.T) {
	m, f, i, w, v := buildCenterTestApp(t)
	foreign := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: "recipe", Status: "succeeded", Name: "foreign", ImageID: v.ImageID, Created: store.Now(), OwnerRunID: "foreign", RecipeSmoke: &RecipeSmokeSpec{Command: []string{"echo", "foreign"}, TimeoutSeconds: 10}}
	owned := foreign
	owned.ID, owned.Name, owned.OwnerRunID = store.ID(), "owned", "owned-run"
	owned.RecipeSmoke = &RecipeSmokeSpec{Command: []string{"python3", "-c", "print('owned')"}, TimeoutSeconds: 10, ExpectedFiles: []string{"/tmp/owned.mp4"}}
	buildCenterFixtureAssistantGrant(t, m, i.ID, w, owned.OwnerRunID)
	if err := m.Store.PutMany(store.Record{Kind: "image-build", ID: foreign.ID, Value: foreign}, store.Record{Kind: "image-build", ID: owned.ID, Value: owned}); err != nil {
		t.Fatal(err)
	}
	input := BuildCenterImageTestInput{Kind: "smoke", RequestID: store.ID(), OwnerRunID: owned.OwnerRunID, SourceBuildID: owned.ID}
	job, err := m.StartBuildCenterImageTest(BuildCenterImageKey(v.ImageID), input)
	if err != nil || job.Scope != "image-test" || job.OwnerRunID != owned.OwnerRunID || job.RecipeSmoke.Command[2] != "print('owned')" {
		t.Fatalf("test did not freeze its permitted source: %+v %v", job, err)
	}
	// Changing another build's recipe cannot change this queued test snapshot.
	owned.RecipeSmoke.Command[2] = "print('changed')"
	if err = m.Store.Put("image-build", owned.ID, owned); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		job, err = m.BuildCenterImageTest(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !buildActive(job.Status) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != "succeeded" || job.OfflineReady || job.RecipeSmoke.Command[2] != "print('owned')" {
		t.Fatalf("incorrect test completion/proof: %+v", job)
	}
	log, err := m.BuildCenterImageTestLog(job.ID, 0)
	if err != nil || !strings.Contains(log.(map[string]any)["text"].(string), "no real Docker") {
		t.Fatalf("missing test log: %+v %v", log, err)
	}
	foundRun := false
	for _, args := range buildCenterFixtureCalls(f) {
		if len(args) == 0 || args[0] != "run" {
			continue
		}
		foundRun = true
		joined := strings.Join(args, " ")
		for _, required := range []string{"--read-only", "--network none", "--cap-drop ALL", "--user 1000:1000", "--memory-swap 2048m", "--pids-limit 256", "--cpus 2", "rundesk-image-smoke-", "Codex app-server initialize passed"} {
			if !strings.Contains(joined, required) {
				t.Fatalf("probe lacks restriction %q: %s", required, joined)
			}
		}
		if strings.Contains(joined, "--mount") || strings.Contains(joined, "docker.sock") || strings.Contains(joined, "changed") || strings.Contains(joined, "echo foreign") {
			t.Fatal("probe borrowed host mounts, mutable recipe, or another helper's smoke")
		}
	}
	if !foundRun {
		t.Fatal("queue did not execute a probe")
	}
	input.RequestID, input.SourceBuildID = store.ID(), foreign.ID
	_, err = m.StartBuildCenterImageTest(BuildCenterImageKey(v.ImageID), input)
	expectDockerCode(t, err, "image_test_unavailable")
}

func TestBuildCenterImagesSmokeFailureAndUnsafeSpecAreNotGreen(t *testing.T) {
	m, f, _, _, v := buildCenterTestApp(t)
	f.mu.Lock()
	f.smokeFail = true
	f.mu.Unlock()
	job, err := m.StartBuildCenterImageTest(BuildCenterImageKey(v.ImageID), BuildCenterImageTestInput{Kind: "offline", RequestID: store.ID()})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		job, err = m.BuildCenterImageTest(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !buildActive(job.Status) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != "failed" || job.Error == "" || job.OfflineReady {
		t.Fatalf("smoke failure was treated as success: %+v", job)
	}
	before := len(buildCenterFixtureCalls(f))
	err = m.runBuildCenterSmoke(context.Background(), v.ImageID, RecipeSmokeSpec{Command: []string{"true"}, ExpectedFiles: []string{"../output.mp4"}}, os.Stderr)
	if err == nil {
		t.Fatal("unsafe output path accepted")
	}
	for _, args := range buildCenterFixtureCalls(f)[before:] {
		if len(args) > 0 && args[0] == "run" {
			t.Fatal("invalid smoke recipe started a container")
		}
	}
}

func TestBuildCenterImagesSmokeProgramActuallyChecksHandshakeAndOutputs(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python unavailable for isolated local protocol fixture")
	}
	dir := t.TempDir()
	trace := filepath.Join(dir, "methods.txt")
	fakeCodex := "#!" + python + "\n" + `import json,os,sys,time
if sys.argv[1:]==['--version']:
 print('codex local protocol fixture')
elif sys.argv[1:]==['app-server']:
 message=json.loads(sys.stdin.readline())
 with open(os.environ['RUNDESK_FIXTURE_METHODS'],'a') as f: f.write(message['method']+'\n')
 print(json.dumps({'id':message['id'],'result':{}}),flush=True)
 time.sleep(60)
else: sys.exit(8)
`
	if err = os.WriteFile(filepath.Join(dir, "codex"), []byte(fakeCodex), 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "render-proof.txt")
	spec := RecipeSmokeSpec{Command: []string{python, "-c", "import pathlib,sys;pathlib.Path(sys.argv[1]).write_text('fixture output')", output}, ExpectedFiles: []string{output}, TimeoutSeconds: 10}
	run := func(spec RecipeSmokeSpec) ([]byte, error) {
		data, _ := json.Marshal(spec)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, python, "-c", buildCenterSmokeProgram, string(data))
		cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "CODEX_HOME="+filepath.Join(dir, "codex-home"), "RUNDESK_FIXTURE_METHODS="+trace)
		return cmd.CombinedOutput()
	}
	if out, err := run(spec); err != nil || !strings.Contains(string(out), "initialize passed; no model turn") {
		t.Fatalf("local smoke fixture failed: %s %v", out, err)
	}
	if err = os.Remove(output); err != nil {
		t.Fatal(err)
	}
	spec.Command = nil
	if out, err := run(spec); err == nil || !strings.Contains(string(out), "missing or empty smoke output") {
		t.Fatalf("missing output was treated as successful: %s %v", out, err)
	}
	methods, err := os.ReadFile(trace)
	if err != nil || string(methods) != "initialize\ninitialize\n" {
		t.Fatalf("probe sent more than basic protocol initialization: %q %v", methods, err)
	}
}
