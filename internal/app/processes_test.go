package app

import (
	"encoding/json"
	"testing"
)

func TestProcessRegistryAndAuthorization(t *testing.T) {
	m := testManager(t)
	w := m.Workspaces()[0]
	if _, e := m.ConfigCall(w.ID, "model/list", map[string]any{}); e != nil {
		t.Fatal(e)
	}
	rows := m.Processes()
	if len(rows) != 1 || rows[0].PID <= 0 || rows[0].Kind != "configuration" || rows[0].WorkspaceID != w.ID {
		t.Fatal(rows)
	}
	h := NewHandler(m, "", true)
	r := v1Request(h, "GET", "/processes", "", "")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var data map[string]any
	json.Unmarshal(r.Body.Bytes(), &data)
	if data["scope"] != "app-server-connections" {
		t.Fatal(data)
	}
	_, _, secret := setupKey(t, m, "process-worker", w.ID, "read", "run")
	h = NewHandler(m, "admin-secret-01234567890123456789", true)
	if appRequest(h, "GET", "/api/v1/processes", "", secret, "").Code != 403 {
		t.Fatal("app can inspect host PIDs")
	}
	if _, e := m.ReloadInstance(DefaultInstance); e != nil {
		t.Fatal(e)
	}
	if len(m.Processes()) != 0 {
		t.Fatal("closed connection still registered")
	}
}
