package kunproto

import (
	"encoding/json"
	"testing"
)

func TestKnownDurableEngineVersionsOnly(t *testing.T) {
	for _, version := range []string{"0.16.0", "0.17.0"} {
		if !CompatibleEngineVersion(version) {
			t.Fatalf("known durable format rejected: %s", version)
		}
	}
	for _, version := range []string{"", "0.15.0", "0.16.1", "0.17.1", "0.18.0", "v0.17.0"} {
		if CompatibleEngineVersion(version) {
			t.Fatalf("unknown durable format accepted: %s", version)
		}
	}
	if Version != 16 || EngineVersion != "0.17.0" {
		t.Fatal("unexpected release contract")
	}
}
func TestDiagnosticProviderPreservesLegacyWireField(t *testing.T) {
	source := DiagnosticSource{DiagnosticScope: DiagnosticScope{SessionID: "source", RunID: "run", Through: 7}, Provider: &MCPServer{Name: "trace", Command: "trace-tool", StartupTimeout: 10, ToolTimeout: 10}}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip DiagnosticSource
	if err := json.Unmarshal(raw, &roundtrip); err != nil || roundtrip.Provider == nil || roundtrip.SnapshotPath != "" {
		t.Fatalf("provider contract lost: %s %v", raw, err)
	}
	if err := json.Unmarshal([]byte(`{"sessionId":"source","runId":"run","through":7,"snapshotPath":"/legacy/trace.db"}`), &roundtrip); err != nil || roundtrip.SnapshotPath != "/legacy/trace.db" {
		t.Fatal("legacy field unreadable")
	}
}
