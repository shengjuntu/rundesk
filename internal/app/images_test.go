package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageCatalogTagHistoryAndPinnedSelection(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	f.imageLabels = map[string]string{"org.opencontainers.image.version": "1.0", "org.opencontainers.image.source": "https://example.test/source", "org.opencontainers.image.revision": "commit-a", "io.rundesk.codex.version": "fixture", "io.rundesk.capabilities.tools": "python3, git", "secret": "do-not-expose"}
	first, e := m.RegisterImage(i.ID, "news:v1")
	if e != nil {
		t.Fatal(e)
	}
	if first.Version != "1.0" || first.CodexVersion != "fixture" || first.Created == "" || first.BuildCreated != "" {
		t.Fatal("metadata or unknown build time wrong", first)
	}
	b, _ := json.Marshal(first)
	if strings.Contains(string(b), "do-not-expose") {
		t.Fatal("unrelated config exposed")
	}
	again, e := m.RegisterImage(i.ID, " news:v1 ")
	if e != nil || again.ID != first.ID {
		t.Fatal("duplicate registration", e)
	}
	i, e = m.SelectImage(i.ID, first.ID, i.Revision)
	if e != nil {
		t.Fatal(e)
	}
	v, e := m.EnsureApplicationEnvironment(i.ID, w.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, e := m.Images(i.ID)
	if e != nil || c.PendingUpdates != 0 {
		t.Fatal("new pinned environment unexpectedly needs update", e)
	}
	v, e = m.EnvironmentAction(v.ID, "start", v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	keep := filepath.Join(v.CodexHome, "keep.txt")
	if e = os.WriteFile(keep, []byte("persist"), 0600); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	f.image = "sha256:222222"
	f.mu.Unlock()
	second, e := m.RegisterImage(i.ID, "news:v1")
	if e != nil || second.ID == first.ID {
		t.Fatal("tag overwrite lost history", e)
	}
	c, e = m.Images(i.ID)
	if e != nil || len(c.Versions) != 2 || c.Execution.ImageID != first.ImageID || c.PendingUpdates != 0 {
		t.Fatal("registration changed target", e)
	}
	// Selecting while a connection exists must not disconnect it or replace containers.
	m.environmentRefs(v.ID).Add(1)
	i, e = m.SelectImage(i.ID, second.ID, i.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if m.environmentRefs(v.ID).Load() != 1 {
		t.Fatal("selection closed connection")
	}
	_, e = m.EnvironmentAction(v.ID, "recreate", v.Revision, i.Revision)
	expectDockerCode(t, e, "environment_busy")
	m.environmentRefs(v.ID).Add(-1)
	c, e = m.Images(i.ID)
	if e != nil || c.PendingUpdates != 1 || c.Environments[0].ImageID != first.ImageID {
		t.Fatal("upgrade impact incorrect", e)
	}
	_, e = m.EnvironmentAction(v.ID, "recreate", v.Revision, i.Revision-1)
	expectDockerCode(t, e, "revision_conflict")
	f.mu.Lock()
	f.image = "sha256:333333"
	f.mu.Unlock()
	v, e = m.EnvironmentAction(v.ID, "recreate", v.Revision, i.Revision)
	if e != nil || v.ImageID != second.ImageID || v.DeployedAt == "" {
		t.Fatal("update followed mutable tag", v, e)
	}
	b, e = os.ReadFile(keep)
	if e != nil || string(b) != "persist" {
		t.Fatal("update lost data", e)
	}
	c, e = m.Images(i.ID)
	if e != nil || c.PendingUpdates != 0 {
		t.Fatal("updated environment remains pending", e)
	}
	i, e = m.SelectImage(i.ID, first.ID, i.Revision)
	if e != nil {
		t.Fatal(e)
	}
	v, e = m.EnvironmentAction(v.ID, "recreate", v.Revision, i.Revision)
	if e != nil || v.ImageID != first.ImageID {
		t.Fatal("previous version could not be redeployed", e)
	}
}
func TestImagePreflightFailurePreservesRunningContainer(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	v, e := m.EnsureApplicationEnvironment(i.ID, w.ID)
	if e != nil {
		t.Fatal(e)
	}
	v, e = m.EnvironmentAction(v.ID, "start", v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	spec := i.Execution
	spec.Image = "news:missing"
	i, e = m.SetExecution(i.ID, i.Revision, spec)
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	f.missingImage = spec.Image
	f.calls = nil
	f.mu.Unlock()
	_, e = m.EnvironmentAction(v.ID, "recreate", v.Revision, i.Revision)
	expectDockerCode(t, e, "docker_image_unavailable")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.containers[v.ContainerName].State != "running" {
		t.Fatal("old container stopped before preflight")
	}
	for _, args := range f.calls {
		if strings.Join(args[:2], " ") == "container stop" || strings.Join(args[:2], " ") == "container rm" {
			t.Fatal("destructive call on preflight failure")
		}
	}
}
func TestImageAvailabilityAndApplicationOwnership(t *testing.T) {
	m, f, i, _ := dockerTestApp(t)
	v, e := m.RegisterImage(i.ID, "news:v1")
	if e != nil {
		t.Fatal(e)
	}
	other, e := m.CreateInstance(InstancePatch{Name: "other"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.SelectImage(other.ID, v.ID, other.Revision)
	expectDockerCode(t, e, "image_version_not_found")
	_, e = m.RegisterImage(DefaultInstance, "news:v1")
	expectDockerCode(t, e, "assistant_requires_local")
	_, e = m.RegisterImage(i.ID, "--format=bad")
	expectDockerCode(t, e, "invalid_image_reference")
	spec := i.Execution
	spec.ImageID = v.ImageID
	spec.ImageVersionID = "unknown"
	_, e = m.SetExecution(i.ID, i.Revision, spec)
	expectDockerCode(t, e, "image_version_not_found")
	f.mu.Lock()
	f.unavailable = true
	f.mu.Unlock()
	v, e = m.CheckImage(i.ID, v.ID)
	if e != nil || v.Availability != "unavailable" || v.Error == "" {
		t.Fatal("offline status not retained", e)
	}
	c, e := m.Images(i.ID)
	if e != nil || len(c.Versions) != 1 || c.Versions[0].ImageID != v.ImageID {
		t.Fatal("offline catalog unavailable", e)
	}
	f.mu.Lock()
	f.unavailable = false
	f.mu.Unlock()
	v, e = m.CheckImage(i.ID, v.ID)
	if e != nil || v.Availability != "available" || v.Error != "" {
		t.Fatal("availability did not recover", e)
	}
}
func TestImageAPIRejectsApplicationKeysAndStaleRevision(t *testing.T) {
	m, _, i, w := dockerTestApp(t)
	a, e := m.RegisterApplication("news-images", ApplicationInput{Name: "News", InstanceID: i.ID, WorkspaceID: w.ID})
	if e != nil {
		t.Fatal(e)
	}
	_, token, e := m.CreateApplicationKey(a.AppID, KeyInput{Name: "app", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read", "run"}})
	if e != nil {
		t.Fatal(e)
	}
	v, e := m.RegisterImage(i.ID, "news:v1")
	if e != nil {
		t.Fatal(e)
	}
	h := NewHandler(m, "administrator-token-123456789", true)
	root := "/api/v1/instances/" + i.ID + "/images"
	for _, req := range []struct{ method, path, body string }{{"GET", root, ""}, {"POST", root, `{"reference":"news:v1"}`}, {"POST", root + "/" + v.ID + "/check", ""}, {"POST", root + "/" + v.ID + "/select", `{"revision":0}`}} {
		r := appRequest(h, req.method, req.path, req.body, token, "")
		if r.Code != 403 {
			t.Fatal("app accessed image administration", r.Code, r.Body.String())
		}
	}
	admin := NewHandler(m, "", true)
	p := "/instances/" + i.ID + "/images/" + v.ID + "/select"
	for _, body := range []string{`{}`, `{"revision":-1}`} {
		r := v1Request(admin, "POST", p, body, "")
		if r.Code != 400 && r.Code != 409 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
}

func TestImageOpenAPIContract(t *testing.T) {
	var spec struct {
		Components struct {
			Schemas map[string]struct{ Properties map[string]any }
		}
		Paths map[string]map[string]map[string]any
	}
	if e := json.Unmarshal(openAPISpec, &spec); e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"imageId", "imageVersionId"} {
		if spec.Components.Schemas["ExecutionSpec"].Properties[field] == nil {
			t.Fatal("missing ExecutionSpec field", field)
		}
	}
	for _, p := range []string{"/instances/{iid}/images", "/instances/{iid}/images/{vid}/check", "/instances/{iid}/images/{vid}/select"} {
		if len(spec.Paths[p]) == 0 {
			t.Fatal("missing image route", p)
		}
		for _, op := range spec.Paths[p] {
			if op["x-administrator-only"] != true {
				t.Fatal("missing admin constraint", p)
			}
		}
	}
}
