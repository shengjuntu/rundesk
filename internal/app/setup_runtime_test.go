package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	p "github.com/shengjuntu/rundesk/internal/kunproto"
)

func TestSetupKunIndependentAndAtomic(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	m.Codex = "missing-codex-setup-test"
	m.Kun = buildKunTestBinary(t)
	i, _ := m.Instance()
	input := SetupInput{WorkspaceID: m.Workspaces()[0].ID, Completed: true, InstanceRevision: &i.Revision, AgentRuntime: &p.Config{Kind: "kun", Endpoint: "http://127.0.0.1:12345/v1", Model: "fixture-model"}}
	h := NewHandler(m, "", true)
	data, _ := json.Marshal(input)
	r := v1Request(h, "PUT", "/setup", string(data), "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	saved, err := m.setupState()
	if err != nil || !saved.Completed || saved.RuntimeKind != "kun" {
		t.Fatal(saved, err)
	}
	current, _ := m.Instance()
	if current.AgentRuntime.Kind != "kun" || current.DefaultModel != "fixture-model" {
		t.Fatal(current)
	}
	env, err := m.setupEnvironment(DefaultInstance, input.WorkspaceID)
	if err != nil || !env.Ready || env.InferenceTested {
		t.Fatal(env, err)
	}
	for _, check := range env.Checks {
		if check.ID == "codex" && check.Required {
			t.Fatal("Kun requires Codex", check)
		}
	}
	// Stale instance revision with a fresh setup revision must not change either.
	input.Revision = saved.Revision
	input.AgentRuntime.Model = "must-not-save"
	_, err = m.saveSetup(input)
	if err == nil {
		t.Fatal("stale instance revision accepted")
	}
	current, _ = m.Instance()
	after, _ := m.setupState()
	if current.AgentRuntime.Model != "fixture-model" || after.Revision != saved.Revision {
		t.Fatal("partial save", current, after)
	}
	// A fresh manager sees the same backend without a Codex installation.
	m2, err := New(m.Data, m.Codex, false)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	i2, _ := m2.Instance()
	if i2.AgentRuntime.Kind != "kun" {
		t.Fatal(i2)
	}
}

func TestSetupKunMissingCredentialAndDraft(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	m.Kun = buildKunTestBinary(t)
	t.Setenv("RUNDESK_SETUP_TEST_KEY", "")
	i, _ := m.Instance()
	in := SetupInput{WorkspaceID: m.Workspaces()[0].ID, Completed: true, InstanceRevision: &i.Revision, AgentRuntime: &p.Config{Kind: "kun", Endpoint: "http://localhost:12345/v1", Model: "fixture", APIKeyEnv: "RUNDESK_SETUP_TEST_KEY"}}
	if _, err := m.saveSetup(in); err == nil {
		t.Fatal("missing key accepted as completed")
	}
	in.Completed = false
	if _, err := m.saveSetup(in); err != nil {
		t.Fatal(err)
	}
	env, err := m.setupEnvironment(DefaultInstance, in.WorkspaceID)
	if err != nil || env.Ready {
		t.Fatal(env, err)
	}
	t.Setenv("RUNDESK_SETUP_TEST_KEY", "secret-never-return-this")
	env, _ = m.setupEnvironment(DefaultInstance, in.WorkspaceID)
	b, _ := json.Marshal(env)
	if !env.Ready || strings.Contains(string(b), "secret-never-return-this") {
		t.Fatal(string(b))
	}
}

func TestSetupBackendCatalogAndAdminBoundary(t *testing.T) {
	if _, err := agentBackend("unknown"); err == nil {
		t.Fatal("unknown backend fell back")
	}
	for _, b := range backendCatalog() {
		if b.Kind == "codex" && strings.Contains(strings.Join(b.Capabilities, ","), "step") {
			t.Fatal("Codex advertises step")
		}
	}
	m := testManager(t)
	_, _, key := setupKey(t, m, "setup-catalog", m.Workspaces()[0].ID, "read", "run")
	h := NewHandler(m, "admin-secret", true)
	for _, path := range []string{"/backends", "/setup/environment"} {
		r := appRequest(h, "GET", "/api/v1"+path, "", key, "")
		if r.Code != 403 {
			t.Fatal(path, r.Code, r.Body.String())
		}
	}
	r := appRequest(h, "POST", "/api/v1/setup/codex-install", `{"version":"1.2.3","confirm":true}`, key, "install-admin-boundary-1")
	if r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestSetupCodexInstallPinnedAndIdempotent(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("fixture executable is Linux amd64")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node unavailable")
	}
	dir := t.TempDir()
	js := filepath.Join(dir, "npm-cli.js")
	fixture := `const fs=require('fs'),path=require('path');const args=process.argv.slice(2),root=args[args.indexOf('--prefix')+1];if(!args.includes('--ignore-scripts')||!args.includes('@openai/codex@1.2.3'))process.exit(2);const bin=path.join(root,'node_modules/@openai/codex-linux-x64/vendor/x86_64-unknown-linux-musl/codex/codex');fs.mkdirSync(path.dirname(bin),{recursive:true});fs.writeFileSync(bin,'#!/bin/sh\nprintf "codex-cli 1.2.3\\n"\n',{mode:0o700});console.log('fixture installation');`
	if err := os.WriteFile(js, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(js, filepath.Join(dir, "npm")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(js, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := testManager(t)
	m.Demo = false
	h := NewHandler(m, "", true)
	for _, version := range []string{"latest", "1.2.3;echo bad", "../1.2.3"} {
		r := v1Request(h, "POST", "/setup/codex-install", fmt.Sprintf(`{"version":%q,"confirm":true}`, version), "install-invalid-"+fmt.Sprint(len(version)))
		if r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	body := `{"version":"1.2.3","confirm":true}`
	r := v1Request(h, "POST", "/setup/codex-install", body, "install-pinned-test-01")
	if r.Code != 200 || object(t, r)["ok"] != true {
		t.Fatal(r.Code, r.Body.String())
	}
	var result CodexInstallResult
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Codex == "" || !strings.HasPrefix(result.Codex, m.Data) {
		t.Fatal(result)
	}
	selected, _ := m.setupState()
	if selected.Codex != "" {
		t.Fatal("installer activated itself")
	}
	retry := v1Request(h, "POST", "/setup/codex-install", body, "install-pinned-test-01")
	if retry.Code != 200 || retry.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(retry.Code, retry.Body.String())
	}
	items, err := os.ReadDir(filepath.Join(m.Data, "tools", "codex"))
	if err != nil || len(items) != 1 {
		t.Fatal("installation replay executed twice", items, err)
	}
}
