package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetupWorksWithoutCodexAndPreservesRevision(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	m.Codex = "rundesk-no-such-codex"
	h := NewHandler(m, "", true)
	r := v1Request(h, "GET", "/setup", "", "")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	w := m.Workspaces()[0]
	r = v1Request(h, "POST", "/setup/check", fmt.Sprintf(`{"kind":"codex","workspaceId":%q}`, w.ID), "")
	if r.Code != 200 || object(t, r)["ok"] != false {
		t.Fatal(r.Body.String())
	}
	r = v1Request(h, "PUT", "/setup", fmt.Sprintf(`{"revision":0,"codex":"rundesk-no-such-codex","workspaceId":%q}`, w.ID), "")
	if r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	m.Demo = true
	body := fmt.Sprintf(`{"revision":0,"codex":"codex","workspaceId":%q,"model":"test-model","completed":true}`, w.ID)
	r = v1Request(h, "PUT", "/setup", body, "")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	state, e := m.setupState()
	if e != nil || !state.Completed || state.Revision != 1 {
		t.Fatal(state, e)
	}
	r = v1Request(h, "PUT", "/setup", body, "")
	if r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	i, _ := m.Instance()
	if i.DefaultModel != "test-model" {
		t.Fatal(i)
	}
}
func TestSetupTemplatePinnedAndAdminOnly(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	r := v1Request(h, "GET", "/setup/docker-template?version=latest", "", "")
	if r.Code != 400 {
		t.Fatal(r.Code)
	}
	r = v1Request(h, "GET", "/setup/docker-template?version=0.159.2", "", "")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	z, e := zip.NewReader(bytes.NewReader(r.Body.Bytes()), int64(r.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range z.File {
		if f.Name == "Dockerfile" {
			in, _ := f.Open()
			b, _ := io.ReadAll(in)
			in.Close()
			found = strings.Contains(string(b), "ARG CODEX_VERSION=0.159.2\n")
		}
	}
	if !found {
		t.Fatal("template missing pinned Codex version")
	}
	conn, e := m.ConnectApplication("setup-test", ApplicationConnect{Name: "Setup Test", InstallationID: "setup-test"})
	if e != nil {
		t.Fatal(e)
	}
	_, key, e := m.CreateApplicationKey(conn.Application.AppID, KeyInput{Name: "worker", WorkspaceIDs: []string{conn.Application.WorkspaceID}, Scopes: []string{"read", "run"}})
	if e != nil {
		t.Fatal(e)
	}
	admin := NewHandler(m, "administrator-secret", true)
	for _, route := range []string{"/setup", "/setup/docker-template?version=0.159.2"} {
		r := appRequest(admin, "GET", "/api/v1"+route, "", key, "")
		if r.Code != 403 {
			t.Fatal(route, r.Code, r.Body.String())
		}
	}
	// A missing login never opens an unauthenticated setup endpoint remotely.
	req := httptest.NewRequest("GET", "http://localhost/api/v1/setup", nil)
	rec := httptest.NewRecorder()
	admin.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal(rec.Code)
	}
}
func TestSetupProviderRejectsSecretURL(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	p := map[string]any{"workspaceId": m.Workspaces()[0].ID, "baseUrl": "https://user:secret@example.com/v1", "model": "model"}
	b, _ := json.Marshal(p)
	r := v1Request(h, "POST", "/setup/provider", string(b), "")
	if r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestBuildDraftDoesNotRequireRuntimeSwitch(t *testing.T) {
	m := testManager(t)
	a, e := m.ConnectApplication("draft-first", ApplicationConnect{Name: "Draft first", InstallationID: "draft-first"})
	if e != nil {
		t.Fatal(e)
	}
	i, _ := m.Instance(a.Application.InstanceID)
	if i.Execution.normalized().Mode != "local" {
		t.Fatal(i)
	}
	v, e := m.CreateBuild(i.ID, "base", "Dockerfile", 1, false, bytes.NewReader(buildZip(t, map[string]string{"Dockerfile": "FROM scratch"})))
	if e != nil {
		t.Fatal(e)
	}
	if v.Status != "draft" {
		t.Fatal(v)
	}
	i, _ = m.Instance(i.ID)
	if i.Execution.normalized().Mode != "local" {
		t.Fatal("draft changed execution mode")
	}
}
