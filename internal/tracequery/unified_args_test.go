package tracequery

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnifiedTraceArguments(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"trace_list_observations", `{"after":0,"source":"codex_app_server","recordKind":"tool","limit":200}`},
		{"trace_list_activity", `{"offset":0,"source":"kun_sdk","recordKind":"model"}`},
	} {
		if _, err := ParseArgs(tc.name, json.RawMessage(tc.raw)); err != nil {
			t.Fatal(tc, err)
		}
	}
	for _, tc := range []struct{ name, raw string }{
		{"trace_list_observations", `{"source":"invalid"}`},
		{"trace_list_observations", `{"recordKind":"invalid"}`},
		{"trace_list_observations", `{"offset":0}`},
		{"trace_list_observations", `{"after":-1}`},
		{"trace_list_observations", `{"limit":201}`},
		{"trace_list_observations", `{"limit":0}`},
		{"trace_list_activity", `{"after":0}`},
		{"trace_list_activity", `{"source":null}`},
		{"trace_list_activity", `{"runId":"` + strings.Repeat("x", 257) + `"}`},
	} {
		if _, err := ParseArgs(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Fatal("accepted", tc)
		}
	}
}
