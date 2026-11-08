package tracequery

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shengjuntu/rundesk/internal/store"
)

func TestObservationSnapshotPagingAndBytePrecision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.db")
	journal, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for _, sid := range []string{"source", "foreign"} {
		if err = journal.Put("session", sid, map[string]string{"id": sid}); err != nil {
			t.Fatal(err)
		}
	}
	request := append([]byte(`{"tools":[{"name":"lookup","parameters":{"type":"object"}}],"input":{"count":9007199254740993,"query":"中文🙂"}}`), 0xff, 0x00)
	response := []byte("event: response.output_item.added\ndata: {\"arguments\":\"{\\\"count\\\":9007199254740993}\"}\n\n")
	record := func(run string) map[string]any {
		return map[string]any{
			"id": "capture-1", "labels": map[string]string{"sessionId": "source", "runId": run}, "started": "2026-10-17T01:00:00Z", "ended": "2026-10-17T01:00:00.125Z", "association": "request-start", "incomplete": false,
			"request":  map[string]any{"method": "POST", "url": "https://example.invalid/v1/responses", "headers": map[string][]string{"Authorization": {"[redacted]"}}, "redactedHeaders": []string{"Authorization"}, "body": map[string]any{"bytes": request, "size": len(request), "complete": true, "truncated": false, "chunks": []any{map[string]any{"offset": 0, "size": 1, "elapsedMicros": json.Number("9007199254740993")}, map[string]any{"offset": 1, "size": len(request) - 1, "elapsedMicros": 2}}}},
			"response": map[string]any{"status": 200, "headers": map[string][]string{"Content-Type": {"text/event-stream"}}, "body": map[string]any{"bytes": response, "size": len(response), "complete": true, "truncated": false, "chunks": []any{map[string]any{"offset": 0, "size": len(response), "elapsedMicros": 4}}}},
		}
	}
	add := func(sid, run string) store.Event {
		event, e := journal.Add(sid, "internal", observationMethod, record(run))
		if e != nil {
			t.Fatal(e)
		}
		return event
	}
	first := add("source", "r1")
	second := add("source", "r1")
	otherRun := add("source", "r2")
	foreign := add("foreign", "r1")
	reader, err := Open(path, "source", otherRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	future := add("source", "r1")
	value, err := reader.Call("trace_list_llm_requests", Args{RunID: "r1", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	page := value.(map[string]any)
	requests := page["requests"].([]map[string]any)
	if len(requests) != 1 || page["total"] != 2 || page["nextOffset"] != 1 || !page["hasMore"].(bool) || requests[0]["eventId"].(json.Number).String() != json.Number(stringify(first.ID)).String() || !requests[0]["complete"].(bool) || requests[0]["durationMs"] != int64(125) {
		t.Fatal(page)
	}
	if _, ok := requests[0]["request"]; ok {
		t.Fatal("listing returned body")
	}
	value, err = reader.Call("trace_list_llm_requests", Args{RunID: "r1", Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	page = value.(map[string]any)
	if page["hasMore"].(bool) || page["requests"].([]map[string]any)[0]["eventId"] != json.Number(stringify(second.ID)) {
		t.Fatal(page)
	}
	for _, args := range []Args{{EventID: foreign.ID}, {EventID: future.ID}, {EventID: first.ID, RunID: "r2"}, {EventID: first.ID, Section: "websocket_client"}, {EventID: first.ID, Section: "unknown"}, {EventID: first.ID, Section: "request", Offset: len(request) + 1}} {
		if _, err = reader.Call("trace_read_llm_request", args); err == nil {
			t.Fatal("accepted out of scope or invalid request", args)
		}
	}
	for _, sample := range []struct {
		section string
		body    []byte
	}{{"request", request}, {"response", response}} {
		for _, limit := range []int{1, 2, 3, 4, 17, 16000} {
			var actual []byte
			for offset := 0; ; {
				value, err = reader.Call("trace_read_llm_request", Args{EventID: first.ID, Section: sample.section, Offset: offset, Limit: limit})
				if err != nil {
					t.Fatal(err)
				}
				chunk := value.(map[string]any)
				decoded, e := base64.StdEncoding.DecodeString(chunk["bodyBase64"].(string))
				if e != nil {
					t.Fatal(e)
				}
				actual = append(actual, decoded...)
				if chunk["redacted"].(bool) || !chunk["headersRedacted"].(bool) {
					t.Fatal("body/header redaction conflated", chunk)
				}
				if !chunk["hasMore"].(bool) {
					break
				}
				next := chunk["nextOffset"].(int)
				if next <= offset {
					t.Fatal("stalled bytes page")
				}
				offset = next
			}
			if !bytes.Equal(actual, sample.body) {
				t.Fatalf("section=%s limit=%d byte fidelity lost", sample.section, limit)
			}
		}
	}
	value, err = reader.Call("trace_read_llm_request", Args{EventID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	meta := value.(map[string]any)
	requestMeta := meta["request"].(map[string]any)
	bodyMeta := requestMeta["body"].(map[string]any)
	if bodyMeta["bytes"] != nil || bodyMeta["chunks"] != nil || bodyMeta["chunkCount"] != 2 || requestMeta["headers"] == nil {
		t.Fatal(meta)
	}
	value, err = reader.Call("trace_read_llm_request", Args{EventID: first.ID, Section: "request_chunks", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	chunks := value.(map[string]any)
	if !chunks["hasMore"].(bool) || chunks["chunks"].([]map[string]any)[0]["elapsedMicros"] != json.Number("9007199254740993") {
		t.Fatal(chunks)
	}
	value, err = reader.Call("trace_read_llm_request", Args{EventID: first.ID, Section: "request_chunks", Offset: 1, Limit: 1})
	if err != nil || value.(map[string]any)["hasMore"].(bool) {
		t.Fatal(value, err)
	}
	// Ordinary session event polling keeps stable IDs and compact manifests only.
	events, err := journal.Events("source", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].ID != first.ID || strings.Contains(string(events[0].Data), base64.StdEncoding.EncodeToString(request)) || !strings.Contains(string(events[0].Data), `"payloadOmitted":true`) {
		t.Fatal(events[0])
	}
	full, err := journal.Event("source", first.ID)
	if err != nil || !strings.Contains(string(full.Data), base64.StdEncoding.EncodeToString(request)) {
		t.Fatal("stored evidence mutated", err)
	}
}
func stringify(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestObservationTruncatedWebsocketAndLargeBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.db")
	journal, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	journal.Put("session", "source", map[string]string{"id": "source"})
	payload := bytes.Repeat([]byte("abc"), 3<<20) // exceeds the legacy event parsing limit
	event, err := journal.Add("source", "internal", observationMethod, map[string]any{"id": "ws", "labels": map[string]string{"sessionId": "source"}, "connectionStartLabels": map[string]string{"runId": "r1"}, "incomplete": true, "captureLoss": "body limit", "request": map[string]any{"body": map[string]any{"bytes": []byte{}, "complete": true}}, "response": map[string]any{"body": map[string]any{"bytes": []byte{}, "complete": true}}, "websocket": map[string]any{"encoding": "websocket-frames", "toClient": map[string]any{"bytes": payload, "size": len(payload) + 100, "complete": false, "truncated": true, "chunkMetadataTruncated": true, "chunks": []any{map[string]any{"offset": 0, "size": len(payload)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(path, "source", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	value, err := reader.Call("trace_read_llm_request", Args{EventID: event.ID, Section: "websocket_client", Offset: len(payload) - 7, Limit: 7})
	if err != nil {
		t.Fatal(err)
	}
	page := value.(map[string]any)
	decoded, _ := base64.StdEncoding.DecodeString(page["bodyBase64"].(string))
	if !bytes.Equal(decoded, payload[len(payload)-7:]) || page["complete"].(bool) || !page["truncated"].(bool) {
		t.Fatal(page)
	}
	value, err = reader.Call("trace_read_llm_request", Args{EventID: event.ID, Section: "websocket_client_chunks"})
	if err != nil || !value.(map[string]any)["chunkMetadataTruncated"].(bool) {
		t.Fatal(value, err)
	}
	value, err = reader.Call("trace_list_llm_requests", Args{RunID: "r1"})
	if err != nil || value.(map[string]any)["total"] != 0 {
		t.Fatal("connection start label claimed run ownership", value, err)
	}
}

func TestObservationProposalRequiresExplicitRunEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proposal.db")
	journal, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	journal.Put("session", "source", map[string]string{"id": "source"})
	journal.Add("source", "internal", "run/input", map[string]any{"runId": "r1", "input": map[string]string{"text": "inspect"}})
	same, _ := journal.Add("source", "internal", observationMethod, map[string]any{"labels": map[string]string{"runId": "r1"}})
	other, _ := journal.Add("source", "internal", observationMethod, map[string]any{"labels": map[string]string{"runId": "r2"}})
	connection, _ := journal.Add("source", "internal", observationMethod, map[string]any{"labels": map[string]string{}, "connectionStartLabels": map[string]string{"runId": "r1"}})
	reader, err := Open(path, "source", connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, sample := range []struct {
		id    int64
		valid bool
	}{{same.ID, true}, {other.ID, false}, {connection.ID, false}} {
		_, err = reader.Call("trace_propose", Args{RunID: "r1", Proposal: &ProposalInput{Kind: "inspect", Reason: "captured request", Text: "Inspect the retained request", EvidenceIDs: []int64{sample.id}}})
		if (err == nil) != sample.valid {
			t.Fatalf("event %d valid=%v error=%v", sample.id, sample.valid, err)
		}
	}
}

func TestObservationDecodedWebSocketMessagesAndPayload(t *testing.T) {
	frame := func(first byte, payload []byte, masked bool) []byte {
		out := []byte{first, byte(len(payload))}
		mask := []byte{7, 9, 11, 13}
		if masked {
			out[1] |= 0x80
			out = append(out, mask...)
		}
		for i, b := range payload {
			if masked {
				b ^= mask[i%4]
			}
			out = append(out, b)
		}
		return out
	}
	text := []byte(`{"count":9007199254740993,"text":"中文🙂"}`)
	raw := frame(0x01, text[:13], true)
	raw = append(raw, frame(0x89, []byte("ping"), false)...)
	raw = append(raw, frame(0x80, text[13:], true)...)
	raw = append(raw, frame(0x81, []byte(`{"type":"response.completed"}`), false)...)
	compressed := []byte{0xff, 0x01, 0x00}
	raw = append(raw, frame(0xc1, compressed, true)...)
	path := filepath.Join(t.TempDir(), "websocket.db")
	journal, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	journal.Put("session", "source", map[string]string{"id": "source"})
	event, err := journal.Add("source", "internal", observationMethod, map[string]any{"labels": map[string]string{"sessionId": "source"}, "websocket": map[string]any{"toUpstream": map[string]any{"bytes": raw, "complete": true, "truncated": false}}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(path, "source", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	value, err := reader.Call("trace_read_llm_request", Args{EventID: event.ID, Section: "websocket_upstream_messages", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	page := value.(map[string]any)
	messages := page["messages"].([]map[string]any)
	if page["total"] != 4 || !page["hasMore"].(bool) || messages[0]["type"] != "text" || messages[0]["payloadBytes"] != len(text) || messages[1]["type"] != "ping" || messages[0]["payload"] != nil || !page["decode"].(map[string]any)["compressionUnsupported"].(bool) {
		t.Fatal(page)
	}
	var actual []byte
	for offset := 0; ; {
		value, err = reader.Call("trace_read_llm_request", Args{EventID: event.ID, Section: "websocket_upstream_payload", Offset: offset, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		chunk := value.(map[string]any)
		part, e := base64.StdEncoding.DecodeString(chunk["bodyBase64"].(string))
		if e != nil {
			t.Fatal(e)
		}
		actual = append(actual, part...)
		if !chunk["complete"].(bool) || chunk["compressed"].(bool) {
			t.Fatal(chunk)
		}
		if !chunk["hasMore"].(bool) {
			break
		}
		offset = chunk["nextOffset"].(int)
	}
	if !bytes.Equal(actual, text) {
		t.Fatalf("decoded payload changed: %q", actual)
	}
	index := 3
	value, err = reader.Call("trace_read_llm_request", Args{EventID: event.ID, Section: "websocket_upstream_payload", MessageIndex: &index})
	if err != nil {
		t.Fatal(err)
	}
	payload := value.(map[string]any)
	part, _ := base64.StdEncoding.DecodeString(payload["bodyBase64"].(string))
	if !bytes.Equal(part, compressed) || payload["text"] != "" || payload["payloadEncoding"] != "permessage-deflate" || !payload["compressed"].(bool) {
		t.Fatal("compressed bytes shown as model text", payload)
	}
	index = 0
	for _, a := range []Args{{EventID: event.ID, Section: "request", MessageIndex: &index}, {EventID: event.ID, Section: "websocket_upstream_messages", MessageIndex: &index}, {EventID: event.ID, Section: "request_messages"}, {EventID: event.ID, Section: "websocket_upstream_messages", Limit: 51}} {
		if _, err = reader.Call("trace_read_llm_request", a); err == nil {
			t.Fatal("invalid message selector accepted", a)
		}
	}
	// Reading the derived view leaves the masked wire evidence unchanged.
	value, err = reader.Call("trace_read_llm_request", Args{EventID: event.ID, Section: "websocket_upstream", Limit: 16000})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := base64.StdEncoding.DecodeString(value.(map[string]any)["bodyBase64"].(string))
	if !bytes.Equal(wire, raw) {
		t.Fatal("derived decoding mutated raw frames")
	}
}
