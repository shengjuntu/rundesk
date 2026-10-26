package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildCenterAndNativePluginsRemainAdministratorResources(t *testing.T) {
	m, u, member, _, _, shared, _ := userFixture(t)
	_, key, e := m.CreateApplicationKey("news-members", KeyInput{Name: "no-build-access", WorkspaceIDs: []string{shared.WorkspaceID}, Scopes: []string{"read", "run", "files"}})
	if e != nil {
		t.Fatal(e)
	}
	h := NewHandler(m, userTestAdmin, true)
	for _, token := range []string{member, key} {
		for _, prefix := range []string{"/api", "/api/v1"} {
			for _, p := range []struct{ method, path string }{
				{"GET", "/build-center/recipes"}, {"POST", "/build-center/recipes"},
				{"GET", "/build-center/builds"}, {"POST", "/build-center/builds/aaaaaaaaaaaaaaaaaaaaaaaa/rebuild"},
				{"GET", "/build-center/images"}, {"DELETE", "/build-center/images/aaaaaaaaaaaaaaaaaaaaaaaa"},
				{"POST", "/build-center/images/aaaaaaaaaaaaaaaaaaaaaaaa/launch"},
				{"POST", "/build-center/images/aaaaaaaaaaaaaaaaaaaaaaaa/tests"},
				{"GET", "/build-center/tests/aaaaaaaaaaaaaaaaaaaaaaaa"},
				{"POST", "/build-center/assistant-runs"}, {"GET", "/build-center/assistant-runs/aaaaaaaaaaaaaaaaaaaaaaaa"},
				{"GET", "/instances/" + shared.InstanceID + "/plugins"}, {"POST", "/plugins/inspect"},
			} {
				r := appRequest(h, p.method, prefix+p.path, `{}`, token, "")
				if r.Code != 403 {
					t.Fatalf("%s %s: %d %s", p.method, prefix+p.path, r.Code, r.Body.String())
				}
			}
		}
	}
	r := appRequest(h, "GET", "/api/v1/member/"+u.Grants[0].ID+"/build-center/images", "", member, "")
	if r.Code != 403 {
		t.Fatalf("member proxy escaped boundary: %d", r.Code)
	}
}

func TestBuildMCPIsTheOnlyTaskCapabilityAuthenticationBoundary(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "admin-secret-01234567890123456789", true)
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, p := range []struct{ method, path string }{
			{"POST", "/build-center/mcp"}, {"GET", "/build-center/mcp"},
			{"POST", "/build-center/mcp/extra"}, {"POST", "/build-center/assistant-runs"},
			{"GET", "/build-center/images"}, {"GET", "/build-center/recipes"},
		} {
			req := httptest.NewRequest(p.method, "http://127.0.0.1:3210"+prefix+p.path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			r := httptest.NewRecorder()
			h.ServeHTTP(r, req)
			if r.Code != 401 {
				t.Fatalf("unauthorized %s %s: %d %s", p.method, prefix+p.path, r.Code, r.Body.String())
			}
		}
	}
	// Task credentials do not relax existing browser origin checks.
	req := httptest.NewRequest("POST", "http://127.0.0.1:3210/api/build-center/mcp", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://foreign.example")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req)
	if r.Code != 403 {
		t.Fatalf("MCP bypassed origin boundary: %d", r.Code)
	}
}

func TestBuildCenterOpenAPIKeepsCapabilitySeparateFromAdministratorAPI(t *testing.T) {
	m := testManager(t)
	r := v1Request(NewHandler(m, "", true), "GET", "/openapi.json", "", "")
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	spec := object(t, r)
	paths := spec["paths"].(map[string]any)
	for _, p := range []string{"/build-center/recipes", "/build-center/images", "/build-center/assistant-runs", "/plugins/inspect", "/instances/{iid}/plugins"} {
		ops, ok := paths[p].(map[string]any)
		if !ok {
			t.Fatalf("missing contract: %s", p)
		}
		for method, value := range ops {
			op := value.(map[string]any)
			if op["x-administrator-only"] != true {
				t.Fatalf("non-admin contract %s %s", method, p)
			}
		}
	}
	mcp := paths["/build-center/mcp"].(map[string]any)["post"].(map[string]any)
	if mcp["x-build-task-capability-only"] != true || mcp["x-administrator-only"] != false {
		t.Fatal("MCP task capability boundary missing", mcp)
	}
	security := mcp["security"].([]any)
	if len(security) != 1 || security[0].(map[string]any)["BuildTaskCapability"] == nil {
		t.Fatal("MCP incorrectly shares administrator credentials", security)
	}
}
