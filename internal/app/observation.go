package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/shengjuntu/rundesk/internal/observeproxy"
	"github.com/shengjuntu/rundesk/internal/rpc"
	"github.com/shengjuntu/rundesk/internal/store"
)

// ObservationOptions controls only transport observation, never model routing,
// provider selection, credentials, retry policy, prompts, or model parameters.
type ObservationOptions struct {
	DefaultEnabled  bool
	ListenAddr      string
	AdvertiseOrigin string
	MaxBodyBytes    int64
}

type ObservationStatus struct {
	CaptureID      string `json:"captureId,omitempty"`
	Pending        bool   `json:"pending"`
	AppliedEnabled bool   `json:"appliedEnabled"`
	State          string `json:"state"`
	Transport      string `json:"transport"`
	Provider       string `json:"provider,omitempty"`
	Upstream       string `json:"upstream,omitempty"`
	Message        string `json:"message"`
	MaxBodyBytes   int64  `json:"maxBodyBytes,omitempty"`
}

// ConfigureHTTPObservation stores defaults without binding a listener. A session
// must request capture before discovery or proxy startup is needed.
func (m *Manager) ConfigureHTTPObservation(options ObservationOptions) error {
	m.observationMu.Lock()
	defer m.observationMu.Unlock()
	if m.observation != nil {
		return errors.New("HTTP observation proxy options cannot change after startup")
	}
	if options.ListenAddr == "" {
		options.ListenAddr = "127.0.0.1:0"
	}
	if options.MaxBodyBytes == 0 {
		options.MaxBodyBytes = 16 << 20
	}
	if options.AdvertiseOrigin != "" {
		u, err := url.Parse(options.AdvertiseOrigin)
		if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return errors.New("observe-advertise must be an HTTP origin without credentials, path, query, or fragment")
		}
		options.AdvertiseOrigin = strings.TrimSuffix(options.AdvertiseOrigin, "/")
	}
	m.observationOptions = options
	return nil
}

// EnableHTTPObservation explicitly starts the listener for integration callers;
// it does not overwrite any session's saved capture preference.
func (m *Manager) EnableHTTPObservation(options ObservationOptions) error {
	if err := m.ConfigureHTTPObservation(options); err != nil {
		return err
	}
	_, err := m.ensureHTTPObservation()
	return err
}

func (m *Manager) ensureHTTPObservation() (*observeproxy.Proxy, error) {
	m.observationMu.Lock()
	defer m.observationMu.Unlock()
	if m.observation != nil {
		return m.observation, nil
	}
	if m.ctx != nil && m.ctx.Err() != nil {
		return nil, errors.New("HTTP observation host is shutting down")
	}
	options := m.observationOptions
	p, err := observeproxy.New(observeproxy.Config{
		ListenAddr:   options.ListenAddr,
		MaxBodyBytes: options.MaxBodyBytes,
		Record: func(record observeproxy.Record) error {
			// Do not use m.event: an observation failure must not cancel or replay
			// the business request. Header credentials were removed from the copy
			// by the proxy; body bytes remain exact (and may contain user data).
			m.mu.Lock()
			defer m.mu.Unlock()
			session := m.sessions[record.Labels.SessionID]
			if session == nil {
				// A session deliberately deleted while the queue drained must
				// not leave its captured prompts behind as orphan events.
				return nil
			}
			currentCapture := session.Observation != nil && session.Observation.AppliedEnabled && session.Observation.CaptureID != "" && session.Observation.CaptureID == record.Labels.CaptureID
			_, e := m.Store.Add(record.Labels.SessionID, "internal", "llm/exchange", record)
			if e != nil {
				log.Printf("HTTP observation persistence failed: %v", e)
				// A failed disk write cannot persist its own warning. Keep the
				// coverage warning visible in the current session immediately;
				// the recorder retries a loss marker when storage recovers.
				if currentCapture {
					status := *session.Observation
					status.State, status.Message = "degraded", "HTTP 观测写入失败，存在采集缺口；原始请求未重发。"
					session.Observation = &status
				}
			} else if currentCapture && session.Observation.State == "configured" && record.Association != "observation-loss" {
				status := *session.Observation
				status.State, status.Message = "observed", "已有请求观测入库；请逐条检查完整性，未结束的连接尚不在列表中。"
				session.Observation = &status
				if err := m.Store.Put("session", session.ID, session); err != nil {
					log.Printf("HTTP observation status persistence: %v", err)
				}
			}
			return e
		},
	})
	if err != nil {
		return nil, err
	}
	m.observation = p
	return p, nil
}

func waitingObservation(enabled bool, previous *ObservationStatus) *ObservationStatus {
	status := &ObservationStatus{State: "disabled", Transport: "http-pass-through", Message: "HTTP 观测已关闭；下次任务直接使用原生模型连接。"}
	if previous != nil {
		status.Provider, status.Upstream, status.MaxBodyBytes = previous.Provider, previous.Upstream, previous.MaxBodyBytes
	}
	if enabled {
		status.State, status.Pending = "pending", true
		status.Message = "已保存开启设置，下一轮任务连接时应用；当前尚未接入观测代理。"
	}
	return status
}

func (m *Manager) observationDisconnected(sid string) {
	if err := m.update(sid, func(s *Session) {
		s.Observation = waitingObservation(s.ObserveHTTP, s.Observation)
	}); err != nil {
		log.Printf("HTTP observation connection status: %v", err)
	}
}

// Called only after thread/start or thread/resume acknowledged the override.
func (m *Manager) observationApplied(sid string, requested bool) {
	if err := m.update(sid, func(s *Session) {
		if s.Observation == nil {
			return
		}
		status := *s.Observation
		status.Pending = s.ObserveHTTP != requested
		status.AppliedEnabled = true
		status.State = "configured"
		status.Message = "此线程已接入原协议直通观测；HTTP 完成或 WebSocket 关闭后入库，请逐条检查完整性。"
		s.Observation = &status
	}); err != nil {
		log.Printf("HTTP observation applied status: %v", err)
	}
}

func (m *Manager) closeHTTPObservation() {
	m.observationMu.RLock()
	p := m.observation
	m.observationMu.RUnlock()
	if p != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.Close(ctx); err != nil {
			log.Printf("HTTP observation shutdown: %v", err)
		}
	}
}

func (m *Manager) observationState(sid string, status ObservationStatus, requested bool) {
	status.Transport = "http-pass-through"
	if err := m.update(sid, func(s *Session) {
		status.Pending = status.Pending || s.ObserveHTTP != requested
		copy := status
		s.Observation = &copy
	}); err != nil {
		log.Printf("HTTP observation status: %v", err)
	}
	if _, err := m.Store.Add(sid, "internal", "llm/capture-status", status); err != nil {
		log.Printf("HTTP observation status persistence: %v", err)
	}
}

// configureHTTPObservation runs before thread/start or thread/resume. Overrides
// apply to this thread only; Codex's own config/read resolves the native config.
// Failure to discover an endpoint leaves execution untouched and visible as a
// coverage gap. A registered endpoint failure is returned by the proxy normally;
// it never triggers a hidden direct fallback.
func (m *Manager) configureHTTPObservation(ctx context.Context, c *rpc.Client, s Session, w Workspace, params map[string]any) bool {
	m.observationMu.RLock()
	options := m.observationOptions
	m.observationMu.RUnlock()
	status := ObservationStatus{State: "unavailable", MaxBodyBytes: options.MaxBodyBytes}
	if !s.ObserveHTTP {
		status.State, status.Message = "disabled", "HTTP 观测已关闭；此线程直接使用原生模型连接，工具协议事件继续记录。"
		m.observationState(s.ID, status, s.ObserveHTTP)
		return false
	}
	if m.Demo {
		status.State, status.Message = "simulated", "演示模式没有真实模型 HTTP 请求。"
		m.observationState(s.ID, status, s.ObserveHTTP)
		return false
	}
	if s.ExecutionMode == "docker" && options.AdvertiseOrigin == "" {
		status.Message = "Docker 需要配置可达的 --observe-listen 和 --observe-advertise；当前仅采集 App Server 协议事件。"
		m.observationState(s.ID, status, s.ObserveHTTP)
		return false
	}
	if s.ExecutionMode == "docker" {
		host, _, _ := net.SplitHostPort(options.ListenAddr)
		if host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
			status.Message = "观测代理只监听宿主回环地址，Docker 无法访问；请设置可达的私有监听地址。"
			m.observationState(s.ID, status, s.ObserveHTTP)
			return false
		}
	}
	discover, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := c.Call(discover, "config/read", map[string]any{"cwd": w.Path, "includeLayers": false})
	if err != nil {
		status.Message = "无法读取 Codex 有效配置；没有改变模型连接，HTTP 正文未采集。"
		m.observationState(s.ID, status, s.ObserveHTTP)
		return false
	}
	var cfg nativeConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&cfg); err != nil {
		status.Message = "Codex 配置响应无法解析，HTTP 正文未采集。"
		m.observationState(s.ID, status, s.ObserveHTTP)
		return false
	}
	provider, upstream, key, err := observationTarget(cfg.Config)
	if errors.Is(err, errObservationNeedsAuth) {
		upstream, err = m.builtinObservationUpstream(discover, c, s)
		key = "openai_base_url"
	}
	status.Provider = provider
	if err != nil {
		status.Message = err.Error()
		m.observationState(s.ID, status, s.ObserveHTTP)
		return false
	}
	p, err := m.ensureHTTPObservation()
	if err != nil {
		status.Message = "无法启动观测代理；保持原模型连接，HTTP 正文未采集。"
		m.observationState(s.ID, status, s.ObserveHTTP)
		return false
	}
	captureID := store.ID()
	route, revoke, err := p.Register(upstream, func() observeproxy.Labels {
		current, e := m.Session(s.ID)
		if e != nil {
			return observeproxy.Labels{CaptureID: captureID, SessionID: s.ID}
		}
		return observeproxy.Labels{CaptureID: captureID, SessionID: s.ID, RunID: current.RunID, ThreadID: current.ThreadID, TurnID: current.TurnID}
	})
	if err != nil {
		status.Message = "无法注册直通观测路由；保持原模型连接。"
		m.observationState(s.ID, status, s.ObserveHTTP)
		return false
	}
	go func() { <-c.Done(); revoke() }()
	if options.AdvertiseOrigin != "" {
		u, _ := url.Parse(route)
		route = options.AdvertiseOrigin + u.EscapedPath()
	}
	config, _ := params["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
	}
	config[key] = route
	params["config"] = config
	status.State, status.Upstream, status.Pending = "pending", upstream, true
	status.CaptureID = captureID
	status.Message = "观测路由已准备，等待原生线程接受设置；尚未确认接入。"
	m.observationState(s.ID, status, s.ObserveHTTP)
	return true
}

var errObservationNeedsAuth = errors.New("built-in OpenAI endpoint requires native authentication routing")

// Select the original endpoint. The built-in provider has its own supported
// override; writing model_providers.openai is rejected by current Codex.
func observationTarget(config map[string]any) (provider, upstream, key string, err error) {
	provider, _ = config["model_provider"].(string)
	if profile, _ := config["profile"].(string); profile != "" {
		profiles, _ := config["profiles"].(map[string]any)
		selected, _ := profiles[profile].(map[string]any)
		if v, _ := selected["model_provider"].(string); v != "" {
			provider = v
		}
	}
	if provider == "" {
		provider = "openai"
	}
	providers, _ := config["model_providers"].(map[string]any)
	p, _ := providers[provider].(map[string]any)
	upstream, _ = p["base_url"].(string)
	if provider == "openai" {
		upstream, _ = config["openai_base_url"].(string)
		if upstream == "" {
			return provider, "", "openai_base_url", errObservationNeedsAuth
		}
		key = "openai_base_url"
	} else {
		// App Server splits override keys on dots, and does not interpret TOML
		// quoting. Never accidentally create a second provider or rewrite its
		// full defaults merely to support an unusual provider identifier.
		if !slug.MatchString(provider) {
			return provider, "", "", errors.New("provider ID 不能安全表示为原生配置覆盖路径；保留原连接，HTTP 正文未采集。")
		}
		key = "model_providers." + provider + ".base_url"
	}
	if upstream == "" {
		return provider, "", "", fmt.Errorf("provider %s 未返回显式 base_url，无法确认原生认证对应的上游；当前只采集 App Server 协议事件。", provider)
	}
	if e := validateObservationUpstream(upstream); e != nil {
		return provider, "", "", e
	}
	return provider, upstream, key, nil
}

func validateObservationUpstream(upstream string) error {
	u, e := url.Parse(upstream)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return errors.New("模型 base_url 含不支持的认证、查询参数或 URL 形式；保持原连接并标记 HTTP 采集缺口。")
	}
	return nil
}

func (m *Manager) builtinObservationUpstream(ctx context.Context, c *rpc.Client, s Session) (string, error) {
	// Never substitute the host environment for an unknown Docker environment.
	if s.ExecutionMode == "docker" {
		return "", errors.New("Docker 内置 provider 需要显式 openai_base_url，以免覆盖容器自身的原生路由；当前仅采集协议事件。")
	}
	if endpoint := os.Getenv("OPENAI_BASE_URL"); endpoint != "" {
		// Codex releases differ on whether this legacy variable is honored.
		// config/read's explicit openai_base_url is authoritative; guessing the
		// environment precedence could silently change the original upstream.
		return "", errors.New("检测到 OPENAI_BASE_URL 环境覆盖，但当前原生配置未确认其生效；请显式配置 openai_base_url 后采集，当前保留原连接。")
	}
	raw, err := c.Call(ctx, "account/read", map[string]any{"refreshToken": false})
	if err != nil {
		return "", errors.New("无法确认 Codex 原生认证路由；保留原连接，HTTP 正文未采集。")
	}
	return observationAccountUpstream(raw)
}

func observationAccountUpstream(raw json.RawMessage) (string, error) {
	var response struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
		WorkspaceRouting *struct {
			BackendOrigin string `json:"backendOrigin"`
			Override      string `json:"accountRoutingOverride"`
		} `json:"workspaceRouting"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Account == nil {
		return "", errors.New("未获取原生账户类型，无法确认模型上游；HTTP 正文未采集。")
	}
	switch response.Account.Type {
	case "apiKey":
		return "https://api.openai.com/v1", nil
	case "chatgpt":
		// Managed residency/routing may be resolved by Codex at request time.
		// Do not pin an inferred origin and override that routing decision.
		if r := response.WorkspaceRouting; r != nil && (r.Override != "" && r.Override != "NO_CONSTRAINT" || r.BackendOrigin != "" && strings.TrimSuffix(r.BackendOrigin, "/") != "https://chatgpt.com") {
			return "", errors.New("账户使用原生工作区路由策略，无法安全固定模型上游；保留原连接并标记 HTTP 采集缺口。")
		}
		return "https://chatgpt.com/backend-api/codex", nil
	default:
		return "", errors.New("当前原生账户类型尚未验证直通接入；保留原连接，HTTP 正文未采集。")
	}
}
