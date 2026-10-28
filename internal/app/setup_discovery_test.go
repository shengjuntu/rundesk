package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSetupProbeProxyKeepsRemoteConfigurationAndBypassesLoopback(t *testing.T) {
	proxyURL, _ := url.Parse("http://configured-proxy:3128")
	calls := 0
	choose := setupProbeProxy(func(req *http.Request) (*url.URL, error) { calls++; return proxyURL, nil })
	for _, address := range []string{"http://localhost:8000/v1", "http://LOCALHOST.:8000/v1", "http://127.0.0.2:8000/v1", "http://[::1]:8000/v1", "http://[::ffff:127.0.0.1]:8000/v1", "http://[::1%25lo]:8000/v1"} {
		req, e := http.NewRequest(http.MethodGet, address, nil)
		if e != nil {
			t.Fatal(e)
		}
		selected, e := choose(req)
		if e != nil || selected != nil || calls != 0 {
			t.Fatalf("loopback proxy leak for %s: %v %v", address, selected, e)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://llm.example/v1/models", nil)
	selected, e := choose(req)
	if e != nil || selected != proxyURL || calls != 1 {
		t.Fatal("remote service proxy was discarded", selected, e, calls)
	}
	client := setupProbeClient(time.Second)
	defer client.CloseIdleConnections()
	if client.Transport.(*http.Transport).Proxy == nil {
		t.Fatal("production probes must retain proxy selection")
	}
}

func TestSetupModelEndpointNormalization(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"http://127.0.0.1:8000", "http://127.0.0.1:8000/v1"}, {"http://[::1]:8080/v1/", "http://[::1]:8080/v1"}, {"https://llm.example/openai/v1/models", "https://llm.example/openai/v1"}, {"https://llm.example/v1/chat/completions", "https://llm.example/v1"}} {
		got, e := setupModelEndpoint(tc.in)
		if e != nil || got != tc.want {
			t.Fatalf("%q: %q %v", tc.in, got, e)
		}
	}
	for _, in := range []string{"file:///etc/passwd", "http://user:secret@llm.example", "http://llm.example/v1?key=secret", "http://llm.example/#fragment", "http://localhost:99999", "http://localhost:0", "http://bad host/", "http://localhost/\n"} {
		if _, e := setupModelEndpoint(in); e == nil {
			t.Fatalf("accepted unsafe endpoint %q", in)
		}
	}
}

func TestSetupDiscoverServiceRequiresValidModelsAndDoesNotInfer(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			t.Errorf("unexpected inference request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"local-qwen"},{"id":"local-qwen"},{"id":""}]}`)
	}))
	defer server.Close()
	client := setupProbeClient(time.Second)
	defer client.CloseIdleConnections()
	result := setupDiscoverService(context.Background(), client, server.URL+"/v1", "explicit", "")
	if result.Status != "listed" || len(result.Models) != 1 || result.Models[0].ID != "local-qwen" || result.InferenceTested || calls.Load() != 1 {
		t.Fatalf("%+v calls=%d", result, calls.Load())
	}
	for _, tc := range []struct {
		status     int
		body, want string
	}{{401, `secret-token`, "auth_required"}, {403, `denied`, "auth_required"}, {200, `{"ok":true}`, "incompatible"}, {200, `{"data":[]}`, "empty"}, {200, `<html>OK</html>`, "incompatible"}} {
		t.Run(tc.want+fmt.Sprint(tc.status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer s.Close()
			r := setupDiscoverService(context.Background(), client, s.URL+"/v1", "explicit", "")
			if r.Status != tc.want || strings.Contains(r.Error, "secret-token") {
				t.Fatal(r)
			}
		})
	}
}

func TestSetupDiscoveryTimeoutRedirectAndIPv6(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	client := setupProbeClient(30 * time.Millisecond)
	defer client.CloseIdleConnections()
	started := time.Now()
	r := setupDiscoverService(context.Background(), client, slow.URL+"/v1", "explicit", "")
	if r.Status != "unreachable" || time.Since(started) > time.Second {
		t.Fatal(r, time.Since(started))
	}
	client.CloseIdleConnections()
	client = setupProbeClient(time.Second)
	defer client.CloseIdleConnections()
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	r = setupDiscoverService(context.Background(), client, redirect.URL+"/v1", "explicit", "private")
	if redirected.Load() != 0 || r.Status != "incompatible" {
		t.Fatal(r, redirected.Load())
	}
	listener, e := net.Listen("tcp6", "[::1]:0")
	if e != nil {
		t.Skip("IPv6 loopback unavailable")
	}
	ipv6 := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[{"id":"ipv6-model"}]}`) }))
	ipv6.Listener = listener
	ipv6.Start()
	defer ipv6.Close()
	endpoint, e := setupModelEndpoint(ipv6.URL)
	if e != nil {
		t.Fatal(e)
	}
	r = setupDiscoverService(context.Background(), client, endpoint, "explicit", "")
	if r.Status != "listed" {
		t.Fatal(r)
	}
}

func setupTestModelServer(t *testing.T, tools bool, credential string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s %s", r.Method, r.URL.Path)
		}
		if credential != "" && r.Header.Get("Authorization") != "Bearer "+credential {
			w.WriteHeader(401)
			return
		}
		var input map[string]any
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		if input["model"] != "test-model" {
			t.Errorf("wrong model: %v", input["model"])
		}
		if input["tools"] != nil && input["tool_choice"] != "auto" {
			t.Errorf("probe must test automatic agent tool choice: %v", input["tool_choice"])
		}
		if input["tools"] != nil && tools {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_setup","type":"function","function":{"name":"rundesk_setup_ping","arguments":"{\"value\":\"OK\"}"}}]}}]}`)
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
		}
	}))
}
func TestSetupModelProbePreservesUsefulErrorWithoutCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"tool_choice auto requires --enable-auto-tool-choice; rejected credential-actual-value"}}`)
	}))
	defer server.Close()
	client := setupProbeClient(time.Second)
	defer client.CloseIdleConnections()
	result := setupTestModel(context.Background(), client, SetupModelTestInput{Endpoint: server.URL + "/v1", Model: "test-model"}, "credential-actual-value")
	if result.InferenceOK || result.ToolsOK || !strings.Contains(result.Error, "--enable-auto-tool-choice") || strings.Contains(result.Error, "credential-actual-value") {
		t.Fatal(result)
	}
}

func TestSetupModelTestSeparatesInferenceAndToolCalling(t *testing.T) {
	client := setupProbeClient(time.Second)
	defer client.CloseIdleConnections()
	for _, tools := range []bool{true, false} {
		s := setupTestModelServer(t, tools, "credential-value")
		r := setupTestModel(context.Background(), client, SetupModelTestInput{Endpoint: s.URL + "/v1", Model: "test-model"}, "credential-value")
		s.Close()
		if !r.InferenceOK || r.ToolsOK != tools || r.RoundTripOK != tools || (r.Status == "ready") != tools {
			t.Fatal(r)
		}
		serialized, _ := json.Marshal(r)
		if strings.Contains(string(serialized), "credential-value") {
			t.Fatal("credential exposed")
		}
	}
}
func TestSetupModelTestRequiresCompleteToolResultRoundTrip(t *testing.T) {
	for _, mode := range []string{"valid", "tool-role-rejected", "empty-final", "more-tool-calls"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input map[string]any
				if json.NewDecoder(r.Body).Decode(&input) != nil {
					t.Error("invalid request")
					w.WriteHeader(400)
					return
				}
				switch calls.Add(1) {
				case 1:
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
				case 2:
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"original-ping-id","type":"function","function":{"name":"rundesk_setup_ping","arguments":"{\"value\":\"OK\"}"}}]}}]}`)
				case 3:
					messages, _ := input["messages"].([]any)
					if len(messages) != 4 {
						t.Error("round trip did not retain conversation", input)
						w.WriteHeader(400)
						return
					}
					assistant, _ := messages[1].(map[string]any)
					tool, _ := messages[2].(map[string]any)
					if assistant["role"] != "assistant" || tool["role"] != "tool" || tool["tool_call_id"] != "original-ping-id" || tool["content"] != `{"value":"OK"}` || input["tools"] != nil || input["tool_choice"] != nil {
						t.Error("invalid tool-result round trip", input)
					}
					switch mode {
					case "valid":
						fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Ping received and completed."}}]}`)
					case "tool-role-rejected":
						w.WriteHeader(400)
						fmt.Fprint(w, `{"error":{"message":"chat template rejects tool role"}}`)
					case "empty-final":
						fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":""}}]}`)
					case "more-tool-calls":
						fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Again","tool_calls":[{"id":"extra","type":"function","function":{"name":"rundesk_setup_ping","arguments":"{}"}}]}}]}`)
					}
				default:
					t.Error("unexpected additional model request")
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			client := setupProbeClient(time.Second)
			defer client.CloseIdleConnections()
			result := setupTestModel(context.Background(), client, SetupModelTestInput{Endpoint: server.URL + "/v1", Model: "test-model"}, "")
			expected := mode == "valid"
			if !result.InferenceOK || result.ToolsOK != expected || result.RoundTripOK != expected || (result.Status == "ready") != expected || calls.Load() != 3 {
				t.Fatal(result, calls.Load())
			}
		})
	}
}

func TestSetupModelTestRejectsMalformedToolArguments(t *testing.T) {
	var n int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"rundesk_setup_ping","arguments":"{\"value\":\"OK\",\"command\":\"rm\"}"}}]}}]}`)
		}
	}))
	defer server.Close()
	client := setupProbeClient(time.Second)
	defer client.CloseIdleConnections()
	r := setupTestModel(context.Background(), client, SetupModelTestInput{Endpoint: server.URL + "/v1", Model: "test-model"}, "")
	if !r.InferenceOK || r.ToolsOK || r.Status != "inference_only" {
		t.Fatal(r)
	}
}
func TestSetupModelTestEvidenceExpiresAndCredentialChangeInvalidates(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	t.Setenv("RUNDESK_TEST_LLM_KEY", "first-secret")
	server := setupTestModelServer(t, true, "first-secret")
	defer server.Close()
	mux := http.NewServeMux()
	s := &Server{Manager: m}
	s.setupDiscoveryRoutes(mux)
	input := fmt.Sprintf(`{"endpoint":%q,"model":"test-model","apiKeyEnv":"RUNDESK_TEST_LLM_KEY"}`, server.URL)
	request := httptest.NewRequest("POST", "/api/setup/model-test", strings.NewReader(input))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 200 || strings.Contains(response.Body.String(), "first-secret") {
		t.Fatal(response.Code, response.Body.String())
	}
	evidence, e := m.setupModelEvidence(server.URL, "test-model", "RUNDESK_TEST_LLM_KEY")
	if e != nil || !evidence.InferenceOK || !evidence.ToolsOK || !evidence.RoundTripOK {
		t.Fatal(evidence, e)
	}
	t.Setenv("RUNDESK_TEST_LLM_KEY", "second-secret")
	if _, e = m.setupModelEvidence(server.URL, "test-model", "RUNDESK_TEST_LLM_KEY"); e == nil {
		t.Fatal("credential change reused readiness")
	}
	t.Setenv("RUNDESK_TEST_LLM_KEY", "first-secret")
	evidence.CheckedAt = time.Now().Add(-31 * time.Minute).Format(time.RFC3339Nano)
	if e = m.Store.Put("setup-model-test", setupModelTestKey(server.URL+"/v1", "test-model", "RUNDESK_TEST_LLM_KEY"), evidence); e != nil {
		t.Fatal(e)
	}
	if _, e = m.setupModelEvidence(server.URL, "test-model", "RUNDESK_TEST_LLM_KEY"); e == nil {
		t.Fatal("stale test reused readiness")
	}
}
