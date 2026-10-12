package app

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestApplicationRuntimeReusesContainerAndRejectsPendingUpdate(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	_, e := m.RegisterApplication("news", ApplicationInput{Name: "News", InstanceID: i.ID, WorkspaceID: w.ID})
	if e != nil {
		t.Fatal(e)
	}
	in := ApplicationRuntimeInput{InstanceID: i.ID, WorkspaceID: w.ID}
	var wg sync.WaitGroup
	for n := 0; n < 3; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := m.PrepareApplicationRuntime("news", in)
			if e != nil || v["executionMode"] != "docker" {
				t.Errorf("%v %v", v, e)
			}
		}()
	}
	wg.Wait()
	if len(f.containers) != 1 {
		t.Fatal(f.containers)
	}
	creates, starts := 0, 0
	for _, c := range f.calls {
		if strings.Join(c[:2], " ") == "container create" {
			creates++
		}
		if strings.Join(c[:2], " ") == "container start" {
			starts++
		}
	}
	if creates != 1 || starts != 1 {
		t.Fatal(creates, starts)
	}
	// An existing config client/lease is not disconnected by a second launch.
	_, e = m.ConfigCall(w.ID, "account/read", map[string]any{}, i.ID)
	if e != nil {
		t.Fatal(e)
	}
	commands := f.commands
	in.Probe = true
	v, e := m.PrepareApplicationRuntime("news", in)
	if e != nil || v["accountReady"] != true || f.commands != commands {
		t.Fatal(v, e, f.commands, commands)
	}
	i, _ = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "docker", Image: "news:v2"})
	_, e = m.PrepareApplicationRuntime("news", in)
	expectDockerCode(t, e, "environment_update_required")
	if len(f.containers) != 1 {
		t.Fatal("replaced environment")
	}
}

func TestApplicationRuntimeScopedAPIAndNoSecrets(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	a, e := m.RegisterApplication("news", ApplicationInput{Name: "News", InstanceID: i.ID, WorkspaceID: w.ID})
	if e != nil {
		t.Fatal(e)
	}
	_, token, e := m.CreateApplicationKey(a.AppID, KeyInput{Name: "app", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read", "run"}})
	if e != nil {
		t.Fatal(e)
	}
	h := NewHandler(m, "admin-secret-01234567890123456789", true)
	in, _ := json.Marshal(ApplicationRuntimeInput{InstanceID: i.ID, WorkspaceID: w.ID})
	r := appRequest(h, "POST", "/api/v1/applications/news/runtime/prepare", string(in), token, "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, secret := range []string{m.Data, i.CodexHome, "PRIVATE", "token", "containerName"} {
		if strings.Contains(r.Body.String(), secret) {
			t.Fatal("leaked", secret)
		}
	}
	for _, path := range []string{"/applications/other/runtime/prepare", "/environments", "/applications/news/connect"} {
		r = appRequest(h, "POST", "/api/v1"+path, string(in), token, "")
		if r.Code != 403 {
			t.Fatal(path, r.Code, r.Body.String())
		}
	}
	_, readToken, _ := m.CreateApplicationKey(a.AppID, KeyInput{Name: "readonly", WorkspaceIDs: []string{w.ID}, Scopes: []string{"read"}})
	r = appRequest(h, "POST", "/api/v1/applications/news/runtime/prepare", string(in), readToken, "")
	if r.Code != 403 {
		t.Fatal(r.Code)
	}
	r = appRequest(h, "GET", "/api/v1/applications/news/runtime", "", readToken, "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	bad := `{"instanceId":"wrong","workspaceId":"` + w.ID + `"}`
	r = appRequest(h, "POST", "/api/v1/applications/news/runtime/prepare", bad, token, "")
	if r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	if len(f.containers) != 1 {
		t.Fatal("foreign environment created")
	}
}

func TestApplicationRuntimeRequiresDockerAndKeepsOwnerGuard(t *testing.T) {
	m, f, i, w := dockerTestApp(t)
	m.RegisterApplication("news", ApplicationInput{Name: "News", InstanceID: i.ID, WorkspaceID: w.ID})
	in := ApplicationRuntimeInput{InstanceID: i.ID, WorkspaceID: w.ID}
	_, e := m.PrepareApplicationRuntime("news", in)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range f.containers {
		c.Labels["io.rundesk.owner"] = "foreign"
	}
	_, e = m.PrepareApplicationRuntime("news", in)
	expectDockerCode(t, e, "container_owner_mismatch")
	i, _ = m.SetExecution(i.ID, i.Revision, ExecutionSpec{Mode: "local"})
	_, e = m.PrepareApplicationRuntime("news", in)
	expectDockerCode(t, e, "docker_configuration_required")
}
