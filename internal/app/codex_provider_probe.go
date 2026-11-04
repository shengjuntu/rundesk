package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

type CodexProviderTest struct {
	Status      string `json:"status"`
	InferenceOK bool   `json:"inferenceOK"`
	ToolsOK     bool   `json:"toolsOK"`
	Error       string `json:"error,omitempty"`
	Scope       string `json:"scope"`
	CheckedAt   string `json:"checkedAt"`
}

func codexProviderFailure(r CodexProviderTest, err error, key string) CodexProviderTest {
	if err != nil {
		message := err.Error()
		if key != "" {
			message = strings.ReplaceAll(message, key, "[redacted]")
		}
		r.Error = diagnosticText(message)
	}
	return r
}
func probeCodexResponses(ctx context.Context, client *http.Client, p CodexProviderConfig, key, scope string) CodexProviderTest {
	r := CodexProviderTest{Status: "failed", Scope: scope, CheckedAt: store.Now()}
	// A synthetic function is handled here, never delegated to a filesystem,
	// shell or business MCP. Probe output never includes provider reply text.
	function := map[string]any{"type": "function", "name": "rundesk_provider_ping", "description": "Return the value unchanged for the setup check.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false}, "strict": true}
	body := map[string]any{"model": p.Model, "input": []any{map[string]any{"role": "user", "content": "Call rundesk_provider_ping with value OK to test tool calling. Do not answer directly."}}, "tools": []any{function}, "tool_choice": "auto", "max_output_tokens": 512, "store": false}
	var first struct {
		Output []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			CallID    string `json:"call_id"`
			Arguments string `json:"arguments"`
		} `json:"output"`
	}
	if _, err := setupResponseJSON(ctx, client, http.MethodPost, p.BaseURL+"/responses", key, body, &first); err != nil {
		return codexProviderFailure(r, err, key)
	}
	if len(first.Output) == 0 {
		return codexProviderFailure(r, errors.New("Responses API 没有返回 output，不能确认推理"), key)
	}
	r.InferenceOK = true
	r.Status = "inference_only"
	callID := ""
	for _, out := range first.Output {
		if out.Type != "function_call" || out.Name != "rundesk_provider_ping" || out.CallID == "" {
			continue
		}
		var args struct {
			Value string `json:"value"`
		}
		if json.Unmarshal([]byte(out.Arguments), &args) == nil && args.Value == "OK" {
			callID = out.CallID
			break
		}
	}
	if callID == "" {
		return codexProviderFailure(r, errors.New("Responses 推理已返回，但没有有效的自动工具调用"), key)
	}
	// Repeat input explicitly rather than relying on server-side response storage.
	body["input"] = []any{map[string]any{"role": "user", "content": "Call rundesk_provider_ping with value OK to test tool calling. Do not answer directly."}, map[string]any{"type": "function_call", "call_id": callID, "name": "rundesk_provider_ping", "arguments": "{\"value\":\"OK\"}"}, map[string]any{"type": "function_call_output", "call_id": callID, "output": "OK"}, map[string]any{"role": "user", "content": "The tool returned OK. Reply OK without another tool call."}}
	body["tool_choice"] = "none"
	var final struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if _, err := setupResponseJSON(ctx, client, http.MethodPost, p.BaseURL+"/responses", key, body, &final); err != nil {
		return codexProviderFailure(r, err, key)
	}
	for _, out := range final.Output {
		if out.Type == "message" {
			for _, part := range out.Content {
				if part.Type == "output_text" && strings.TrimSpace(part.Text) != "" {
					r.ToolsOK = true
					r.Status = "ready"
					return r
				}
			}
		}
	}
	return codexProviderFailure(r, errors.New("工具结果回传后没有最终文本，不能确认完整工具闭环"), key)
}
func codexProviderAuthMatches(provider map[string]any, keyEnv string) bool {
	key, _ := provider["env_key"].(string)
	if key != keyEnv || provider["requires_openai_auth"] != false {
		return false
	}
	for _, name := range []string{"http_headers", "env_http_headers", "experimental_bearer_token", "bearer_token", "query_params"} {
		switch v := provider[name].(type) {
		case nil:
		case string:
			if v != "" {
				return false
			}
		case map[string]any:
			if len(v) > 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func (m *Manager) TestCodexProvider(parent context.Context, iid, wid string) (CodexProviderTest, error) {
	if m.Demo {
		return CodexProviderTest{}, failure(400, "demo_no_model_calls", "Demo 不调用真实模型")
	}
	view, err := m.CodexProvider(iid, wid)
	if err != nil {
		return CodexProviderTest{}, err
	}
	if !view.Configured || view.State != "configured" {
		return CodexProviderTest{}, failure(409, "codex_provider_not_applied", "请先保存模型服务；待应用或密钥变化的容器需要明确启动或重建")
	}
	i, err := m.Instance(iid)
	if err != nil {
		return CodexProviderTest{}, err
	}
	scope := "host"
	if i.Execution.normalized().Mode == "docker" {
		v, err := m.EnvironmentAction(view.EnvironmentID, "inspect", 0)
		if err != nil {
			return CodexProviderTest{}, err
		}
		if v.State != "running" {
			return CodexProviderTest{}, failure(409, "codex_provider_container_stopped", "测试不会自动启动容器，请先启动所选环境")
		}
		scope = "docker-container"
	}
	// Read effective native config from this exact environment. Project overrides
	// must not be mistaken for the managed provider stored in another HOME.
	native, err := m.readConfig(wid, iid)
	if err != nil {
		return CodexProviderTest{}, err
	}
	providers, _ := native.Config["model_providers"].(map[string]any)
	provider, _ := providers["rundesk"].(map[string]any)
	if native.Config["model_provider"] != "rundesk" || native.Config["model"] != view.Model || provider["base_url"] != view.BaseURL || provider["wire_api"] != "responses" || !codexProviderAuthMatches(provider, view.APIKeyEnv) {
		return CodexProviderTest{}, failure(409, "codex_provider_overridden", "当前 Codex 有效模型配置与保存值不同，请检查项目层覆盖")
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	if scope == "docker-container" {
		m.environmentMu.Lock()
		v := *m.environments[view.EnvironmentID]
		m.environmentMu.Unlock()
		out, err := m.docker.Run(ctx, "exec", v.ContainerName, "/opt/rundesk/bin/rundesk", "__codex_provider_probe", view.BaseURL, view.Model, view.APIKeyEnv)
		if err != nil {
			secret := os.Getenv(view.APIKeyEnv)
			return codexProviderFailure(CodexProviderTest{Status: "failed", Scope: scope, CheckedAt: store.Now()}, errors.New("容器模型测试失败；检查容器连接、模型服务与凭据"), secret), nil
		}
		var result CodexProviderTest
		if json.Unmarshal(out, &result) != nil || result.Scope != "docker-container" {
			return CodexProviderTest{}, failure(502, "codex_provider_probe_invalid", "容器测试未返回有效结果")
		}
		if result.Status == "ready" && (!result.InferenceOK || !result.ToolsOK) {
			return CodexProviderTest{}, failure(502, "codex_provider_probe_invalid", "容器测试结果不完整")
		}
		result = codexProviderFailure(result, errors.New(result.Error), os.Getenv(view.APIKeyEnv))
		if result.Error == "" {
			result.Error = ""
		}
		return result, nil
	}
	key, err := setupModelCredential(view.APIKeyEnv)
	if err != nil {
		return CodexProviderTest{}, failure(400, "codex_provider_credential", err.Error())
	}
	client := setupProbeClient(40 * time.Second)
	defer client.CloseIdleConnections()
	return probeCodexResponses(ctx, client, view.CodexProviderConfig, key, scope), nil
}

// RunCodexProviderProbe is used by the already-running managed container. Only
// public configuration is passed in argv; its selected key comes from env.
func RunCodexProviderProbe(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("provider probe requires base URL, model and credential variable name")
	}
	p, err := normalizeCodexProvider(CodexProviderInput{BaseURL: args[0], Model: args[1], APIKeyEnv: args[2]}, false)
	result := CodexProviderTest{Status: "failed", Scope: "docker-container", CheckedAt: store.Now()}
	key := ""
	if args[2] != "" {
		key = os.Getenv(args[2])
	}
	if err != nil {
		result = codexProviderFailure(result, err, key)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		client := setupProbeClient(35 * time.Second)
		defer client.CloseIdleConnections()
		result = probeCodexResponses(ctx, client, p, key, "docker-container")
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
