package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestObservationTargetPreservesSelectedProvider(t *testing.T) {
	var cfg map[string]any
	input := `{"model_provider":"unused","profile":"work","profiles":{"work":{"model_provider":"fixture"}},"model_providers":{"fixture":{"name":"Native provider","wire_api":"responses","base_url":"https://example.test/v1","env_key":"EXAMPLE_KEY","request_max_retries":2}}}`
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(cfg)
	provider, upstream, key, err := observationTarget(cfg)
	if err != nil || provider != "fixture" || upstream != "https://example.test/v1" || key != `model_providers.fixture.base_url` {
		t.Fatalf("wrong endpoint selection: %q %q %q %v", provider, upstream, key, err)
	}
	after, _ := json.Marshal(cfg)
	if string(before) != string(after) {
		t.Fatal("observation changed provider config")
	}
}

func TestObservationDoesNotInventUpstreamOrRewriteCredentialURLs(t *testing.T) {
	for _, endpoint := range []string{"", "https://secret:password@example.test/v1", "https://example.test/v1?api_key=secret", "https://example.test/v1#fragment", "file:///tmp/request"} {
		config := map[string]any{"model_provider": "custom", "model_providers": map[string]any{"custom": map[string]any{"base_url": endpoint}}}
		if _, _, _, err := observationTarget(config); err == nil {
			t.Fatalf("accepted %q", endpoint)
		}
	}
}

func TestObservationAdvertiseRejectsQueryDelimiter(t *testing.T) {
	// Appending the capability route after '?' would put it in the query,
	// preventing the container from reaching the registered upstream route.
	m := &Manager{}
	if err := m.EnableHTTPObservation(ObservationOptions{AdvertiseOrigin: "http://host.docker.internal:3211?"}); err == nil {
		m.closeHTTPObservation()
		t.Fatal("accepted an advertise origin with an empty query delimiter")
	}
}

func TestCaptureRedactionPreservesArgumentPrecision(t *testing.T) {
	raw := json.RawMessage(`{"params":{"item":{"type":"mcpToolCall","arguments":{"recordId":9007199254740993,"values":[9223372036854775807,1.234567890123456789]}}},"config":{"model_providers.x.base_url":"http://127.0.0.1:1234/_rundesk_observe/private-secret"},"authorization":"Bearer test"}`)
	data, err := json.Marshal(redact(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, exact := range []string{"9007199254740993", "9223372036854775807", "1.234567890123456789"} {
		if !strings.Contains(string(data), exact) {
			t.Fatalf("lost numeric precision %s: %s", exact, data)
		}
	}
	if strings.Contains(string(data), "private-secret") || strings.Contains(string(data), "Bearer test") {
		t.Fatalf("credential leaked: %s", data)
	}
}

func TestObservationNativeOpenAIRouting(t *testing.T) {
	provider, upstream, key, err := observationTarget(map[string]any{"openai_base_url": "https://example.test/backend-api/codex"})
	if err != nil || provider != "openai" || upstream != "https://example.test/backend-api/codex" || key != "openai_base_url" {
		t.Fatalf("builtin override: %s %s %s %v", provider, upstream, key, err)
	}
	for _, tc := range []struct{ raw, target string }{
		{`{"account":{"type":"apiKey"}}`, "https://api.openai.com/v1"},
		{`{"account":{"type":"chatgpt"}}`, "https://chatgpt.com/backend-api/codex"},
		{`{"account":{"type":"chatgpt"},"workspaceRouting":{"backendOrigin":"https://chatgpt.com","accountRoutingOverride":"NO_CONSTRAINT"}}`, "https://chatgpt.com/backend-api/codex"},
		{`{"account":{"type":"chatgpt"},"workspaceRouting":{"backendOrigin":"https://managed.example","accountRoutingOverride":"us"}}`, ""},
		{`{"account":{"type":"amazonBedrock"}}`, ""},
		{`{"account":null}`, ""},
	} {
		got, err := observationAccountUpstream(json.RawMessage(tc.raw))
		if got != tc.target || (err != nil) != (tc.target == "") {
			t.Fatalf("account %s -> %q, %v", tc.raw, got, err)
		}
	}
}
