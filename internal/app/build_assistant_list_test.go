package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/your-org/rundesk/internal/store"
)

func TestBuildAssistantRunsExactSourceHydrationAndReadOnly(t *testing.T) {
	m := testManager(t)
	m.queueMu.Lock()
	m.queue.Paused = true
	m.queueMu.Unlock()
	sourceID, foreignID, emptyID := store.ID(), store.ID(), store.ID()
	for _, id := range []string{sourceID, foreignID, emptyID} {
		if err := m.Store.Put("image-build", id, ImageBuild{ID: id, Status: "failed"}); err != nil {
			t.Fatal(err)
		}
	}
	for n := 1; n <= 24; n++ {
		id := fmt.Sprintf("%024x", n)
		source, created := sourceID, "2026-11-06T10:00:00Z"
		if n == 22 {
			created = "2026-11-05T10:00:00Z"
		}
		if n == 21 {
			created = "2026-11-07T10:00:00Z"
		}
		if n == 23 {
			source = foreignID
		}
		if n == 24 {
			source = "" // An original build request is not this source's repair.
		}
		run := BuildAssistantRun{ID: id, SourceBuildID: source, Created: created, TaskID: "task-" + id, Status: "queued", ExpiresAt: "2020-01-01T00:00:00Z"}
		if n == 21 {
			run.ProtocolVersion = 1
		}
		record := buildAssistantRecord{BuildAssistantRun: run, CapabilityHash: "private-hash-" + id, RuntimeFingerprint: "private-runtime", WorkspacePath: "/private/workspace", Endpoint: "http://private-callback.invalid", Fingerprint: "private-fingerprint"}
		if err := m.Store.Put("build-assistant", id, record); err != nil {
			t.Fatal(err)
		}
		// These deliberately invalid/expired capabilities must remain unchanged
		// by this read-only recovery query.
		if err := m.Store.Put("build-assistant-capability", record.CapabilityHash, buildAssistantCapability{RunID: id, ExpiresAt: run.ExpiresAt}); err != nil {
			t.Fatal(err)
		}
		m.queueMu.Lock()
		m.tasks[run.TaskID] = Task{ID: run.TaskID, Status: "completed", Reason: "Current task status"}
		m.queueMu.Unlock()
	}
	beforeCapabilities, err := m.Store.List("build-assistant-capability")
	if err != nil {
		t.Fatal(err)
	}
	beforeTasks, beforeWorkspaces, beforeSessions := len(m.Tasks()), len(m.Workspaces()), len(m.Sessions())
	h := NewHandler(m, "", true)
	for _, prefix := range []string{"/api", "/api/v1"} {
		r := request(h, "GET", prefix+"/build-center/assistant-runs?sourceBuildId="+sourceID, "", "", "")
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		var result BuildAssistantRunList
		if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Items) != 20 || !result.HasMore || result.Items[0].ID != fmt.Sprintf("%024x", 21) || result.Items[1].ID != fmt.Sprintf("%024x", 20) || result.Items[19].ID != fmt.Sprintf("%024x", 2) {
			t.Fatal("scope, timestamp order, deterministic tie or bound incorrect", result)
		}
		if view := result.Items[0].Maintenance; view == nil || view.Phase != "blocked" || view.VerifiedBuildID != "" {
			t.Fatal("terminal task without verified candidate must remain unverified", result.Items[0])
		}
		for _, run := range result.Items {
			if run.SourceBuildID != sourceID || run.Status != "completed" || run.Error != "Current task status" || run.RevokedAt != "" {
				t.Fatal("public run was not hydrated safely", run)
			}
		}
		for _, secret := range []string{"private-hash", "private-runtime", "/private/workspace", "private-callback", "private-fingerprint"} {
			if strings.Contains(r.Body.String(), secret) {
				t.Fatal("stored assistant metadata leaked", secret)
			}
		}
	}
	result, err := m.BuildAssistantRuns(emptyID)
	if err != nil || result.Items == nil || len(result.Items) != 0 || result.HasMore {
		t.Fatal("empty source must return items array", result, err)
	}
	afterCapabilities, err := m.Store.List("build-assistant-capability")
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(beforeCapabilities)
	afterJSON, _ := json.Marshal(afterCapabilities)
	if !bytes.Equal(beforeJSON, afterJSON) || beforeTasks != len(m.Tasks()) || beforeWorkspaces != len(m.Workspaces()) || beforeSessions != len(m.Sessions()) {
		t.Fatal("repair lookup changed capabilities or admitted work")
	}
}

func TestBuildAssistantRunsStrictRequiredSourceQuery(t *testing.T) {
	m := testManager(t)
	id := store.ID()
	if err := m.Store.Put("image-build", id, ImageBuild{ID: id, Status: "failed"}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(m, "", true)
	for _, suffix := range []string{
		"", "?sourceBuildId=", "?sourceBuildId=bad", "?sourceBuildId=" + id[:8],
		"?sourceBuildId=" + id + "&limit=20", "?sourceBuildId=" + id + "&unknown=1",
		"?sourceBuildId=" + id + "&sourceBuildId=" + id,
		"?sourceBuildId=" + id + ";unknown=1", "?sourceBuildId=%zz", "?other=" + id,
	} {
		r := v1Request(h, "GET", "/build-center/assistant-runs"+suffix, "", "")
		if r.Code != 400 {
			t.Fatalf("nonexact or extraneous source query %q accepted: %d %s", suffix, r.Code, r.Body.String())
		}
	}
	r := v1Request(h, "GET", "/build-center/assistant-runs?sourceBuildId="+store.ID(), "", "")
	if r.Code != 404 || object(t, r)["code"] != "build_not_found" {
		t.Fatal("unknown source must not become an unscoped inventory", r.Code, r.Body.String())
	}
}

func TestBuildAssistantRunsChronologicalFractionalDates(t *testing.T) {
	m := testManager(t)
	sourceID := store.ID()
	if err := m.Store.Put("image-build", sourceID, ImageBuild{ID: sourceID}); err != nil {
		t.Fatal(err)
	}
	dates := []string{
		"2026-11-06T10:00:00.1Z", "2026-11-06T10:00:00.11Z", "2026-11-06T10:00:00Z",
		"2026-11-06T10:00:00.100000000Z", "malformed-a", "malformed-z", "2026-11-06T11:00:00.11+01:00",
	}
	for n, created := range dates {
		id := fmt.Sprintf("%024x", n+1)
		if err := m.Store.Put("build-assistant", id, buildAssistantRecord{BuildAssistantRun: BuildAssistantRun{ID: id, SourceBuildID: sourceID, Created: created}}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := m.BuildAssistantRuns(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range []int{7, 2, 4, 1, 3, 6, 5} {
		if result.Items[i].ID != fmt.Sprintf("%024x", n) {
			t.Fatal("fractional/equivalent timestamps or malformed fallback ordered incorrectly", result.Items)
		}
	}
}
