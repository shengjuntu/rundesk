package tracequery

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/observation"
	"github.com/shengjuntu/rundesk/internal/store"
)

func unifiedFixture(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "unified.db")
	journal, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	for _, session := range []string{"s", "foreign"} {
		if err = journal.Put("session", session, map[string]any{"id": session, "runId": "mutable-wrong-run", "taskId": "mutable-wrong-task"}); err != nil {
			t.Fatal(err)
		}
	}
	return journal, path
}
func unifiedAdd(t *testing.T, j *store.Store, method, direction string, value any) store.Event {
	t.Helper()
	e, err := j.Add("s", direction, method, value)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func unifiedRead(t *testing.T, path string, through int64) *Reader {
	t.Helper()
	r, err := Open(path, "s", through)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}
func unifiedEventsPage(t *testing.T, r *Reader, a UnifiedArgs) observation.EventPage {
	t.Helper()
	v, err := r.Unified(context.Background(), "observations", a)
	if err != nil {
		t.Fatal(err)
	}
	return v.(observation.EventPage)
}
func unifiedActivityPage(t *testing.T, r *Reader, a UnifiedArgs) observation.ActivityPage {
	t.Helper()
	v, err := r.Unified(context.Background(), "activity", a)
	if err != nil {
		t.Fatal(err)
	}
	return v.(observation.ActivityPage)
}

func TestUnifiedMixedSourcesIdentityAndEvidence(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r1", "taskId": "task-one", "input": map[string]any{"text": "private prompt"}})
	unifiedAdd(t, j, "turn/started", "in", map[string]any{"params": map[string]any{"threadId": "thread-one", "turn": map[string]any{"id": "turn-one"}}})
	item := func(method, status string) store.Event {
		return unifiedAdd(t, j, method, "in", map[string]any{"params": map[string]any{"threadId": "thread-one", "turnId": "turn-one", "item": map[string]any{"id": "tool", "type": "commandExecution", "command": "secret-command --token private-token", "status": status}}})
	}
	start := item("item/started", "running")
	model := func(method string, seq int64) store.Event {
		return unifiedAdd(t, j, method, "in", map[string]any{"runId": "r1", "sequence": seq, "revision": 12, "data": map[string]any{"step": 2, "purpose": "act", "usage": map[string]any{"input_tokens": 17}}})
	}
	modelStart := model("kun/model.started", 10)
	transport := unifiedAdd(t, j, observationMethod, "internal", map[string]any{"id": "attempt-one", "labels": map[string]any{"runId": "r1", "captureId": "lease-one", "turnId": "turn-one"}, "started": "2026-10-20T01:00:00Z", "ended": "2026-10-20T01:00:01Z", "request": map[string]any{"method": "POST", "url": "https://invalid/?api_key=private-key", "headers": map[string]any{"Authorization": "Bearer private-key"}, "body": map[string]any{"bytes": []byte("private-body"), "complete": true}}, "response": map[string]any{"status": 200, "body": map[string]any{"complete": true}}})
	modelEnd := model("kun/model.completed", 11)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r2", "taskId": "task-two"})
	unifiedAdd(t, j, "turn/started", "in", map[string]any{"params": map[string]any{"turn": map[string]any{"id": "turn-two"}}})
	end := item("item/completed", "completed") // late r1 completion after r2 started
	usage := unifiedAdd(t, j, "thread/tokenUsage/updated", "in", map[string]any{"params": map[string]any{"threadId": "thread-one", "turnId": "turn-one", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 25}}}})
	j.Add("foreign", "internal", "run/input", map[string]any{"runId": "r1", "taskId": "foreign-task"})
	r := unifiedRead(t, path, usage.ID)
	future := unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r1", "taskId": "future-task"})
	page := unifiedEventsPage(t, r, UnifiedArgs{RunID: "r1", Limit: 200})
	seen := map[int64]observation.Event{}
	for _, e := range page.Observations {
		seen[e.EventID] = e
		if e.TaskID != "task-one" || e.RunID != "r1" || e.EventID >= future.ID {
			t.Fatalf("historical identity leak: %+v", e)
		}
	}
	if seen[start.ID].Native.TurnID != "turn-one" || seen[end.ID].Association != "reconstructed" {
		t.Fatal(seen)
	}
	if seen[modelStart.ID].Clock != "kun_worker" || seen[modelEnd.ID].Native.WorkerSequence != 11 || seen[modelEnd.ID].Native.WorkerRevision != 12 {
		t.Fatal(seen[modelEnd.ID])
	}
	if seen[transport.ID].Native.TransportID != "attempt-one" || seen[transport.ID].Native.CaptureID != "lease-one" || seen[transport.ID].Evidence[0].ReadWith != "llm_request" {
		t.Fatal(seen[transport.ID])
	}
	if seen[usage.ID].Usage.Aggregation != "cumulative" || seen[modelEnd.ID].Usage.Aggregation != "delta" {
		t.Fatal("usage semantics lost")
	}
	activity := unifiedActivityPage(t, r, UnifiedArgs{RunID: "r1", Limit: 200})
	models, transports, tools := 0, 0, 0
	for _, a := range activity.Activity {
		switch a.Kind {
		case "model":
			models++
			if len(a.Evidence) != 2 || a.Evidence[0].EventID != modelStart.ID || a.Evidence[1].EventID != modelEnd.ID || a.Usage == nil {
				t.Fatal(a)
			}
		case "transport":
			transports++
			if a.Status != "completed" {
				t.Fatal(a)
			}
		case "tool":
			tools++
			if len(a.Evidence) != 2 || a.Evidence[1].EventID != end.ID {
				t.Fatal(a)
			}
		case "run":
			if a.Type == "run" && a.Clock != "mixed_host_and_worker" {
				t.Fatal(a)
			}
		}
	}
	if models != 1 || transports != 1 || tools != 1 {
		t.Fatalf("model/transport merged or duplicated: %d %d %d", models, transports, tools)
	}
	for _, v := range []any{page, activity} {
		encoded, _ := json.Marshal(v)
		for _, secret := range []string{"private prompt", "private-key", "private-body", "private-token", "secret-command", "future-task", "mutable-wrong"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("metadata exposed %q", secret)
			}
		}
	}
}

func TestUnifiedCursorFiltersUnknownLargePayloadAndWebSocket(t *testing.T) {
	j, path := unifiedFixture(t)
	run := unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r", "taskId": "task"})
	unknown := unifiedAdd(t, j, "future/extension", "in", map[string]any{"id": json.Number("9007199254740993"), "params": map[string]any{"payload": strings.Repeat("s", 9<<20)}})
	ws := unifiedAdd(t, j, observationMethod, "internal", map[string]any{"id": "ws-id", "labels": map[string]any{"runId": "r", "turnId": "opening-turn"}, "connectionStartLabels": map[string]any{"runId": "r"}, "websocket": map[string]any{"toClient": map[string]any{"bytes": []byte("private-data"), "truncated": true}}, "captureLoss": "sensitive error details", "incomplete": true})
	r := unifiedRead(t, path, ws.ID)
	firstPage := unifiedEventsPage(t, r, UnifiedArgs{Limit: 1})
	if len(firstPage.Observations) != 1 || firstPage.NextAfter != run.ID || !firstPage.HasMore {
		t.Fatal(firstPage)
	}
	second := unifiedEventsPage(t, r, UnifiedArgs{After: firstPage.NextAfter, Source: observation.CodexAppServer, Kind: "event", Limit: 1})
	if len(second.Observations) != 1 || second.Observations[0].EventID != unknown.ID || second.Observations[0].Native.RPCID != "9007199254740993" || second.HasMore {
		t.Fatal(second)
	}
	third := unifiedEventsPage(t, r, UnifiedArgs{After: unknown.ID})
	if len(third.Observations) != 1 || third.Observations[0].RunID != "" || third.Observations[0].Native.TurnID != "" || !third.Observations[0].Transport.Truncated || !third.Observations[0].Transport.CaptureLoss {
		t.Fatal(third)
	}
	if page := unifiedEventsPage(t, r, UnifiedArgs{RunID: "r", Source: observation.ObserveProxy}); len(page.Observations) != 0 {
		t.Fatal("WebSocket incorrectly owned by opening run", page)
	}
	if page := unifiedActivityPage(t, r, UnifiedArgs{Source: observation.ObserveProxy}); page.Total != 1 || page.Activity[0].RunID != "" || page.Activity[0].Status != "incomplete" {
		t.Fatal(page)
	}
	zero := unifiedRead(t, path, 0)
	if page := unifiedEventsPage(t, zero, UnifiedArgs{}); len(page.Observations) != 0 || page.NextAfter != 0 || page.HasMore {
		t.Fatal(page)
	}
	if page := unifiedActivityPage(t, zero, UnifiedArgs{}); page.Total != 0 || len(page.Activity) != 0 {
		t.Fatal(page)
	}
}

func TestUnifiedFailuresAmbiguityAndStrictSelectors(t *testing.T) {
	j, path := unifiedFixture(t)
	unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r", "taskId": "task-a"})
	unifiedAdd(t, j, "turn/started", "in", map[string]any{"params": map[string]any{"turn": map[string]any{"id": "turn"}}})
	failed := unifiedAdd(t, j, "item/completed", "in", map[string]any{"params": map[string]any{"turnId": "turn", "item": map[string]any{"id": "tool", "type": "commandExecution", "exitCode": 1}}})
	kun := unifiedAdd(t, j, "kun/tool.completed", "in", map[string]any{"runId": "r", "sequence": 1, "data": map[string]any{"step": 1, "call": map[string]any{"id": "call"}, "isError": true}})
	missing := unifiedAdd(t, j, observationMethod, "internal", map[string]any{"id": "no-completion", "labels": map[string]any{"runId": "r"}})
	last := unifiedAdd(t, j, "run/input", "internal", map[string]any{"runId": "r", "taskId": "task-b"})
	r := unifiedRead(t, path, last.ID)
	page := unifiedEventsPage(t, r, UnifiedArgs{Limit: 200})
	for _, e := range page.Observations {
		if e.RunID == "r" && (e.TaskID != "" || !strings.Contains(strings.Join(e.Issues, ","), "ambiguous_task_identity")) {
			t.Fatal(e)
		}
		if (e.EventID == failed.ID || e.EventID == kun.ID) && e.Status != "failed" {
			t.Fatal(e)
		}
		if e.EventID == missing.ID && e.Status != "unknown" {
			t.Fatal(e)
		}
	}
	for _, sample := range []struct {
		kind string
		args UnifiedArgs
	}{{"observations", UnifiedArgs{Offset: 1}}, {"activity", UnifiedArgs{After: 1}}, {"observations", UnifiedArgs{Limit: 201}}, {"observations", UnifiedArgs{Source: "unknown"}}, {"activity", UnifiedArgs{Kind: "invented"}}, {"observations", UnifiedArgs{After: last.ID + 1}}} {
		if _, err := r.Unified(context.Background(), sample.kind, sample.args); err == nil {
			t.Fatalf("accepted invalid selectors: %+v", sample)
		}
	}
	activity := unifiedActivityPage(t, r, UnifiedArgs{Limit: 1})
	if !activity.HasMore || activity.NextOffset != 1 || len(activity.Activity) != 1 {
		t.Fatal(activity)
	}
	empty := unifiedActivityPage(t, r, UnifiedArgs{Offset: activity.Total + 10})
	if empty.Offset != activity.Total || empty.HasMore || len(empty.Activity) != 0 {
		t.Fatal(empty)
	}
}

func TestUnifiedKnownLifecycleSemantics(t *testing.T) {
	j, path := unifiedFixture(t)
	samples := []struct {
		method        string
		data          map[string]any
		phase, status string
	}{
		{"approval/pending", map[string]any{"id": "approval"}, "start", "pending"},
		{"approval/resolved", map[string]any{"id": "approval", "decision": "accept"}, "end", "accepted"},
		{"kun/approval.requested", map[string]any{"runId": "r", "data": map[string]any{"callId": "c"}}, "start", "pending"},
		{"kun/run.paused", map[string]any{"runId": "r"}, "point", "paused"},
		{"kun/mcp.failed", map[string]any{"runId": "r"}, "point", "failed"},
		{"kun/tool.completed", map[string]any{"runId": "r", "data": map[string]any{"isError": true, "replay": map[string]any{"mode": "recorded", "executed": false}}}, "point", "replayed"},
		{"kun/tool.completed", map[string]any{"runId": "r", "data": map[string]any{"status": "reused", "reusedFrom": "prior-call"}}, "point", "reused"},
		{"extension/completed", map[string]any{}, "point", "recorded"},
	}
	var through int64
	for _, sample := range samples {
		through = unifiedAdd(t, j, sample.method, "internal", sample.data).ID
	}
	page := unifiedEventsPage(t, unifiedRead(t, path, through), UnifiedArgs{})
	for i, e := range page.Observations {
		if e.Phase != samples[i].phase || e.Status != samples[i].status {
			t.Fatalf("%s got %s/%s expected %s/%s", e.Method, e.Phase, e.Status, samples[i].phase, samples[i].status)
		}
	}
}

func TestUnifiedTransportLossCannotClaimComplete(t *testing.T) {
	j, path := unifiedFixture(t)
	e := unifiedAdd(t, j, observationMethod, "internal", map[string]any{
		"captureLoss": "recorder overflow", "labels": map[string]string{"runId": "r"},
		"request":  map[string]any{"body": map[string]bool{"complete": true}},
		"response": map[string]any{"status": 200, "body": map[string]bool{"complete": true}},
	})
	page := unifiedEventsPage(t, unifiedRead(t, path, e.ID), UnifiedArgs{})
	if len(page.Observations) != 1 || page.Observations[0].Status != "incomplete" || page.Observations[0].Transport.Complete {
		t.Fatal(page)
	}
}
