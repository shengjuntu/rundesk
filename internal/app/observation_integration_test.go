package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	d "github.com/shengjuntu/rundesk/internal/debugapi"
	"github.com/shengjuntu/rundesk/internal/observeproxy"
	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

// This child process models only the App Server control protocol. Real Codex
// HTTP and WebSocket endpoint overrides are covered by the separate CLI smoke.
func TestObservationRPCFixture(t *testing.T) {
	if os.Getenv("RUNDESK_OBSERVATION_RPC_FIXTURE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request rpc.Message
		if json.Unmarshal(scanner.Bytes(), &request) != nil || len(request.ID) == 0 {
			continue
		}
		response := rpc.Message{ID: request.ID, Result: json.RawMessage(`{}`)}
		switch request.Method {
		case "config/read":
			if os.Getenv("RUNDESK_OBSERVATION_CONFIG_FAILURE") == "1" {
				response.Result = nil
				response.Error = &rpc.Error{Code: -32603, Message: "synthetic configuration unavailable"}
			} else {
				response.Result = json.RawMessage(`{"config":` + os.Getenv("RUNDESK_OBSERVATION_CONFIG") + `}`)
			}
		case "account/read":
			response.Result = json.RawMessage(`{"account":{"type":"apiKey"}}`)
		case "thread/start", "thread/resume":
			response.Result = request.Params
		}
		if encoder.Encode(response) != nil {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

func observationRPCFixture(t *testing.T, config map[string]any, fail bool) *rpc.Client {
	t.Helper()
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestObservationRPCFixture$")
	command.Env = append(os.Environ(), "RUNDESK_OBSERVATION_RPC_FIXTURE=1", "RUNDESK_OBSERVATION_CONFIG="+string(encoded))
	if fail {
		command.Env = append(command.Env, "RUNDESK_OBSERVATION_CONFIG_FAILURE=1")
	}
	client, err := rpc.Start(command, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); <-client.Done() })
	return client
}

func waitObservationRecords(t *testing.T, manager *Manager, sid string, count int) []store.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, err := manager.Store.QueryEvents(sid, 0, 100, store.EventFilter{Method: "llm/exchange"})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) >= count {
			return events
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("model observation was not persisted")
	return nil
}

func TestObservationManagerEndpointToStoredQuery(t *testing.T) {
	for _, provider := range []string{"fixture", "openai"} {
		t.Run(provider, func(t *testing.T) {
			requestBody := []byte("{\n \"model\":\"native-model\",\"input\":[{\"text\":\"中文🙂\"}],\"count\":9007199254740993,\"tools\":[{\"type\":\"function\",\"parameters\":{\"type\":\"object\"}}]}\n")
			responseBody := []byte("data: {\"type\":\"response.created\"}\n\ndata: {\"type\":\"response.completed\",\"count\":9007199254740993}\n\n")
			type incoming struct {
				path, auth, header string
				body               []byte
			}
			seen := make(chan incoming, 4)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				seen <- incoming{r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("X-Native-Provider"), body}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("X-Request-Id", "native-request")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write(responseBody)
			}))
			defer upstream.Close()
			basePath, overrideKey := "/v1", "model_providers.fixture.base_url"
			nativeConfig := map[string]any{"model_provider": provider, "model": "native-model", "model_providers": map[string]any{
				"fixture": map[string]any{"name": "Native provider", "base_url": upstream.URL + basePath, "wire_api": "responses", "env_key": "NATIVE_KEY", "request_max_retries": 2, "http_headers": map[string]any{"X-Native-Provider": "keep-me"}},
			}}
			if provider == "openai" {
				basePath, overrideKey = "/backend-api/codex", "openai_base_url"
				nativeConfig = map[string]any{"model_provider": "openai", "model": "native-model", "openai_base_url": upstream.URL + basePath}
			}
			beforeConfig, _ := json.Marshal(nativeConfig)
			manager := testManager(t)
			workspace := manager.Workspaces()[0]
			session, err := manager.CreateSession(workspace.ID, "capture fixture", "native-model")
			if err != nil {
				t.Fatal(err)
			}
			manager.Demo = false
			if err = manager.EnableHTTPObservation(ObservationOptions{MaxBodyBytes: 1 << 20}); err != nil {
				t.Fatal(err)
			}
			client := observationRPCFixture(t, nativeConfig, false)
			params := map[string]any{"model": "native-model", "approvalPolicy": "on-request", "sandbox": "read-only", "config": map[string]any{"model_reasoning_effort": "high", "mcp_servers.fixture.enabled": false}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			manager.configureHTTPObservation(ctx, client, session, workspace, params)
			overrides := params["config"].(map[string]any)
			route, _ := overrides[overrideKey].(string)
			if route == "" || !strings.HasSuffix(route, basePath) || len(overrides) != 3 {
				t.Fatalf("incorrect native override: %#v", overrides)
			}
			if params["model"] != "native-model" || params["approvalPolicy"] != "on-request" || params["sandbox"] != "read-only" || overrides["model_reasoning_effort"] != "high" || overrides["mcp_servers.fixture.enabled"] != false {
				t.Fatal("observation changed task parameters or permissions")
			}
			afterConfig, _ := json.Marshal(nativeConfig)
			if !bytes.Equal(beforeConfig, afterConfig) {
				t.Fatal("observation mutated native provider configuration")
			}
			// Exercise the existing RPC serialization boundary with the overrides.
			echo, err := client.Call(ctx, "thread/start", params)
			if err != nil {
				t.Fatal(err)
			}
			var received map[string]any
			if json.Unmarshal(echo, &received) != nil || !reflect.DeepEqual(received, params) {
				t.Fatal("thread parameters changed over App Server RPC")
			}
			for index, runID := range []string{"run-first", "run-second"} {
				if err := manager.update(session.ID, func(s *Session) { s.RunID, s.ThreadID, s.TurnID = runID, "thread-native", "turn-"+runID }); err != nil {
					t.Fatal(err)
				}
				request, _ := http.NewRequestWithContext(ctx, http.MethodPost, route+"/responses?stream=true", bytes.NewReader(requestBody))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer synthetic-native-key")
				request.Header.Set("X-Native-Provider", "keep-me")
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != http.StatusAccepted || !bytes.Equal(body, responseBody) || response.Header.Get("X-Request-Id") != "native-request" {
					t.Fatalf("native response changed: status=%d error=%v", response.StatusCode, err)
				}
				incoming := <-seen
				if incoming.path != basePath+"/responses?stream=true" || incoming.auth != "Bearer synthetic-native-key" || incoming.header != "keep-me" || !bytes.Equal(incoming.body, requestBody) {
					t.Fatal("upstream path, authentication, headers or body changed")
				}
				events := waitObservationRecords(t, manager, session.ID, index+1)
				if !strings.Contains(string(events[index].Data), `"payloadOmitted":true`) {
					t.Fatal("session polling should expose only the bounded observation manifest")
				}
				event, err := manager.Store.Event(session.ID, events[index].ID)
				if err != nil {
					t.Fatal(err)
				}
				var record observeproxy.Record
				if err := json.Unmarshal(event.Data, &record); err != nil {
					t.Fatal(err)
				}
				if record.Labels.SessionID != session.ID || record.Labels.RunID != runID || record.Labels.ThreadID != "thread-native" || record.Labels.TurnID != "turn-"+runID || record.Response.Status != http.StatusAccepted {
					t.Fatalf("wrong persisted association: %+v", record.Labels)
				}
				if !bytes.Equal(record.Request.Body.Bytes, requestBody) || !bytes.Equal(record.Response.Body.Bytes, responseBody) || !record.Request.Body.Complete || !record.Response.Body.Complete || record.Incomplete {
					t.Fatalf("persisted payload incomplete/changed: request=%+v response=%+v incomplete=%v error=%s", record.Request.Body, record.Response.Body, record.Incomplete, record.Error)
				}
				if strings.Contains(string(event.Data), "synthetic-native-key") || record.Request.Headers["Authorization"][0] != "[redacted]" {
					t.Fatal("stored copy did not redact authorization")
				}
				service := DebugService{manager: manager}
				for section, expected := range map[string][]byte{"request": requestBody, "response": responseBody} {
					value, err := service.Query(ctx, session.ID, d.Query{Kind: "llm_request", RunID: runID, EventID: event.ID, Section: section, Through: &event.ID})
					if err != nil || value.Source != "http_observation" {
						t.Fatalf("debug query failed: %v", err)
					}
					chunk := value.Data.(map[string]any)
					decoded, err := base64.StdEncoding.DecodeString(chunk["bodyBase64"].(string))
					if err != nil || !bytes.Equal(decoded, expected) || chunk["complete"] != true {
						t.Fatal("query did not return the retained exact body")
					}
				}
			}
		})
	}
}

func TestObservationManagerUnavailableLeavesNativeParameters(t *testing.T) {
	for _, sample := range []struct {
		name                                            string
		enabled, configFailure, missingEndpoint, docker bool
		state                                           string
	}{
		{name: "disabled", state: "disabled"},
		{name: "configuration-failure", enabled: true, configFailure: true, state: "unavailable"},
		{name: "unknown-endpoint", enabled: true, missingEndpoint: true, state: "unavailable"},
		{name: "docker-without-listener", enabled: true, docker: true, state: "unavailable"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			manager := testManager(t)
			workspace := manager.Workspaces()[0]
			session, err := manager.CreateSession(workspace.ID, "unchanged fixture", "native-model")
			if err != nil {
				t.Fatal(err)
			}
			manager.Demo = false
			if sample.docker {
				session.ExecutionMode = "docker"
			}
			if sample.enabled {
				if err := manager.EnableHTTPObservation(ObservationOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			cfg := map[string]any{"model_provider": "fixture", "model_providers": map[string]any{"fixture": map[string]any{"name": "Native"}}}
			if !sample.missingEndpoint {
				cfg["model_providers"].(map[string]any)["fixture"].(map[string]any)["base_url"] = "http://127.0.0.1:1/v1"
			}
			client := observationRPCFixture(t, cfg, sample.configFailure)
			params := map[string]any{"model": "native-model", "approvalPolicy": "on-request", "config": map[string]any{"sandbox_mode": "read-only", "model_reasoning_effort": "high"}}
			before, _ := json.Marshal(params)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			manager.configureHTTPObservation(ctx, client, session, workspace, params)
			after, _ := json.Marshal(params)
			current, err := manager.Session(session.ID)
			if err != nil || !bytes.Equal(before, after) || current.Observation == nil || current.Observation.State != sample.state {
				t.Fatalf("native parameters or coverage status wrong: %s %+v %v", after, current.Observation, err)
			}
		})
	}
}
