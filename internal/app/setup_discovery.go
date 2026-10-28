package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Discovery is read-only and bounded. Only loopback addresses are probed
// automatically; any non-local address must be supplied explicitly by an admin.
// Listing models is not evidence that inference or function calling works.
type SetupDiscoveryPlatform struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	CPUCount    int    `json:"cpuCount"`
	MemoryBytes uint64 `json:"memoryBytes"`
}
type SetupDiscoveryGPU struct {
	Vendor    string `json:"vendor"`
	Name      string `json:"name"`
	MemoryMiB uint64 `json:"memoryMiB"`
	Driver    string `json:"driver"`
	Detail    string `json:"detail,omitempty"`
}
type SetupDiscoveryTool struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}
type SetupDiscoveryModel struct {
	ID string `json:"id"`
}
type SetupDiscoveryService struct {
	Endpoint        string                `json:"endpoint"`
	Source          string                `json:"source"`
	Status          string                `json:"status"`
	Models          []SetupDiscoveryModel `json:"models"`
	Error           string                `json:"error,omitempty"`
	AuthRequired    bool                  `json:"authRequired"`
	InferenceTested bool                  `json:"inferenceTested"`
}
type SetupDiscovery struct {
	ScannedAt string                  `json:"scannedAt"`
	Platform  SetupDiscoveryPlatform  `json:"platform"`
	GPUs      []SetupDiscoveryGPU     `json:"gpus"`
	Tools     []SetupDiscoveryTool    `json:"tools"`
	Services  []SetupDiscoveryService `json:"services"`
	Warnings  []string                `json:"warnings"`
}
type SetupDiscoveryInput struct {
	Endpoints []string `json:"endpoints,omitempty"`
	APIKeyEnv string   `json:"apiKeyEnv,omitempty"`
}
type SetupModelTestInput struct {
	Endpoint  string `json:"endpoint"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"apiKeyEnv,omitempty"`
}
type SetupModelTestResult struct {
	Endpoint    string `json:"endpoint"`
	Model       string `json:"model"`
	InferenceOK bool   `json:"inferenceOK"`
	ToolsOK     bool   `json:"toolsOK"`
	RoundTripOK bool   `json:"roundTripOK"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	CheckedAt   string `json:"checkedAt"`
	LatencyMs   int64  `json:"latencyMs"`
}

// A test receipt belongs to one endpoint/model/credential configuration. It is
// never a permanent readiness assertion, and the secret itself is never stored.
type setupModelTestEvidence struct {
	SetupModelTestResult
	APIKeyEnv      string `json:"apiKeyEnv"`
	CredentialHash string `json:"credentialHash"`
}

func setupModelTestKey(endpoint, model, keyEnv string) string {
	return buildAssistantDigest(struct{ Endpoint, Model, APIKeyEnv string }{endpoint, model, keyEnv})
}
func (m *Manager) setupModelEvidence(endpoint, model, keyEnv string) (setupModelTestEvidence, error) {
	var evidence setupModelTestEvidence
	normalized, err := setupModelEndpoint(endpoint)
	if err != nil {
		return evidence, err
	}
	credential, err := setupModelCredential(keyEnv)
	if err != nil {
		return evidence, err
	}
	if err = m.Store.Get("setup-model-test", setupModelTestKey(normalized, model, keyEnv), &evidence); err != nil {
		return evidence, err
	}
	checked, err := time.Parse(time.RFC3339Nano, evidence.CheckedAt)
	if err != nil || time.Since(checked) > 30*time.Minute || checked.After(time.Now().Add(time.Minute)) || evidence.CredentialHash != keyHash(credential) || evidence.Endpoint != normalized || evidence.Model != model || evidence.APIKeyEnv != keyEnv {
		return evidence, fmt.Errorf("model test is stale or its configuration/credential has changed; test again")
	}
	return evidence, nil
}

var setupCredentialName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Accept an OpenAI base URL, server root, or a copied model/chat endpoint.
func setupModelEndpoint(raw string) (string, error) {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\x00") {
		return "", fmt.Errorf("invalid model endpoint")
	}
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("enter an HTTP(S) base URL without credentials, query or fragment")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid endpoint port")
		}
	}
	if strings.ContainsAny(u.Host, " \t\\") {
		return "", fmt.Errorf("invalid endpoint host")
	}
	path := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/chat/completions", "/models"} {
		path = strings.TrimSuffix(path, suffix)
	}
	if path == "" {
		path = "/v1"
	}
	u.Path = path
	u.RawPath = ""
	return u.String(), nil
}
func setupModelCredential(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if len(name) > 160 || !setupCredentialName.MatchString(name) {
		return "", fmt.Errorf("enter the credential environment variable name, not its value")
	}
	secret := os.Getenv(name)
	if secret == "" {
		return "", fmt.Errorf("credential environment variable %s is unset", name)
	}
	return secret, nil
}
func setupProbeProxy(fallback func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		host := strings.TrimSuffix(strings.ToLower(req.URL.Hostname()), ".")
		ipHost, _, _ := strings.Cut(host, "%")
		if ip := net.ParseIP(ipHost); host == "localhost" || ip != nil && ip.IsLoopback() {
			return nil, nil
		}
		return fallback(req)
	}
}
func setupProbeClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Loopback is always direct. Explicit remote services use the same proxy
	// environment as Kun's provider client, including its NO_PROXY rules.
	transport.Proxy = setupProbeProxy(http.ProxyFromEnvironment)
	transport.MaxResponseHeaderBytes = 16384
	transport.ResponseHeaderTimeout = timeout
	transport.DialContext = (&net.Dialer{Timeout: timeout}).DialContext
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func setupResponseJSON(ctx context.Context, client *http.Client, method, endpoint, credential string, body any, result any) (int, error) {
	var input io.Reader
	if body != nil {
		data, e := json.Marshal(body)
		if e != nil {
			return 0, e
		}
		input = bytes.NewReader(data)
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, input)
	if e != nil {
		return 0, e
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	response, e := client.Do(req)
	if e != nil {
		return 0, e
	}
	defer response.Body.Close()
	// Preserve bounded standard API diagnostics (e.g. a missing vLLM tool
	// parser) without returning arbitrary HTML or the credential used here.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		var remote struct {
			Error json.RawMessage `json:"error"`
		}
		message := ""
		if json.Unmarshal(data, &remote) == nil {
			var detail struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(remote.Error, &detail) == nil {
				message = detail.Message
			}
		}
		if credential != "" {
			message = strings.ReplaceAll(message, credential, "[redacted]")
		}
		message = diagnosticText(message)
		if len(message) > 1000 {
			message = message[:1000]
		}
		if message != "" {
			return response.StatusCode, fmt.Errorf("model service returned HTTP %d: %s", response.StatusCode, message)
		}
		return response.StatusCode, fmt.Errorf("model service returned HTTP %d", response.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if e != nil {
		return response.StatusCode, e
	}
	if len(data) > 256<<10 {
		return response.StatusCode, fmt.Errorf("model response exceeded 256 KiB")
	}
	if e = json.Unmarshal(data, result); e != nil {
		return response.StatusCode, fmt.Errorf("model service returned invalid JSON")
	}
	return response.StatusCode, nil
}
func setupDiscoverService(ctx context.Context, client *http.Client, endpoint, source, credential string) SetupDiscoveryService {
	r := SetupDiscoveryService{Endpoint: endpoint, Source: source, Status: "unreachable", Models: []SetupDiscoveryModel{}}
	var response struct {
		Data []SetupDiscoveryModel `json:"data"`
	}
	code, e := setupResponseJSON(ctx, client, http.MethodGet, endpoint+"/models", credential, nil, &response)
	if e != nil {
		r.Error = diagnosticText(e.Error())
		if code == 401 || code == 403 {
			r.Status = "auth_required"
			r.AuthRequired = true
		} else if code > 0 {
			r.Status = "incompatible"
		}
		return r
	}
	if response.Data == nil {
		r.Status = "incompatible"
		r.Error = "models response has no data array"
		return r
	}
	seen := map[string]bool{}
	for _, v := range response.Data {
		if v.ID != "" && len(v.ID) <= 160 && !strings.ContainsAny(v.ID, "\r\n\x00") && !seen[v.ID] {
			r.Models = append(r.Models, v)
			seen[v.ID] = true
			if len(r.Models) >= 200 {
				break
			}
		}
	}
	r.Status = "listed"
	if len(r.Models) == 0 {
		r.Status = "empty"
		r.Error = "model service is reachable but no model is loaded"
	}
	return r
}
func setupLocalEndpoints() []string {
	ports := []int{8000, 8080, 5000, 1234, 11434}
	seen := map[int]bool{}
	for _, p := range ports {
		seen[p] = true
	}
	// An all-address listener is reachable through loopback too. Reading these
	// kernel tables does not disclose command lines, process environment or keys.
	if runtime.GOOS == "linux" {
		var discovered []int
		for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
			f, e := os.Open(path)
			if e != nil {
				continue
			}
			scanner := bufio.NewScanner(io.LimitReader(f, 1<<20))
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) < 4 || fields[3] != "0A" {
					continue
				}
				parts := strings.Split(fields[1], ":")
				if len(parts) != 2 {
					continue
				}
				p, e := strconv.ParseInt(parts[1], 16, 32)
				if e == nil && p >= 1024 && p <= 65535 && !seen[int(p)] {
					seen[int(p)] = true
					discovered = append(discovered, int(p))
				}
			}
			f.Close()
		}
		sort.Ints(discovered)
		if len(discovered) > 20 {
			discovered = discovered[:20]
		}
		ports = append(ports, discovered...)
	}
	endpoints := make([]string, 0, len(ports)*2)
	for _, port := range ports {
		endpoints = append(endpoints, "http://127.0.0.1:"+strconv.Itoa(port)+"/v1", "http://[::1]:"+strconv.Itoa(port)+"/v1")
	}
	return endpoints
}
func setupMemoryBytes(parent context.Context) uint64 {
	if runtime.GOOS == "linux" {
		f, e := os.Open("/proc/meminfo")
		if e != nil {
			return 0
		}
		defer f.Close()
		scanner := bufio.NewScanner(io.LimitReader(f, 65536))
		for scanner.Scan() {
			v := strings.Fields(scanner.Text())
			if len(v) >= 2 && v[0] == "MemTotal:" {
				n, _ := strconv.ParseUint(v[1], 10, 64)
				return n * 1024
			}
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	var output string
	var err error
	switch runtime.GOOS {
	case "darwin":
		output, err = setupReadCommand(ctx, "sysctl", "-n", "hw.memsize")
	case "windows":
		output, err = setupReadCommand(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory")
	default:
		return 0
	}
	if err != nil {
		return 0
	}
	size, _ := strconv.ParseUint(strings.TrimSpace(output), 10, 64)
	return size
}

func setupReadCommand(ctx context.Context, path string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, path, args...)
	configureProbeProcess(command)
	command.WaitDelay = time.Second
	var out limitedOutput
	command.Stdout = &out
	command.Stderr = &out
	e := command.Run()
	return strings.TrimSpace(diagnosticText(out.String())), e
}
func setupNVIDIAGPUs(ctx context.Context) ([]SetupDiscoveryGPU, string) {
	out, e := setupReadCommand(ctx, "nvidia-smi", "--query-gpu=name,memory.total,driver_version", "--format=csv,noheader,nounits")
	if e != nil {
		return nil, "NVIDIA GPU query failed: " + diagnosticText(e.Error())
	}
	records, e := csv.NewReader(strings.NewReader(out)).ReadAll()
	if e != nil {
		return nil, "NVIDIA GPU response could not be parsed"
	}
	var gpus []SetupDiscoveryGPU
	for _, row := range records {
		if len(row) != 3 {
			continue
		}
		memory, _ := strconv.ParseUint(strings.TrimSpace(row[1]), 10, 64)
		gpus = append(gpus, SetupDiscoveryGPU{Vendor: "NVIDIA", Name: strings.TrimSpace(row[0]), MemoryMiB: memory, Driver: strings.TrimSpace(row[2])})
		if len(gpus) >= 32 {
			break
		}
	}
	return gpus, ""
}
func setupOtherGPUs(ctx context.Context, id, vendor string, args ...string) ([]SetupDiscoveryGPU, string) {
	out, e := setupReadCommand(ctx, id, args...)
	if e != nil {
		return nil, vendor + " GPU query failed: " + diagnosticText(e.Error())
	}
	// ROCm and MUSA CLI schemas vary across driver releases; retain bounded raw
	// inventory evidence instead of guessing memory/driver columns.
	if out == "" {
		return nil, ""
	}
	if len(out) > 12000 {
		out = out[:12000]
	}
	return []SetupDiscoveryGPU{{Vendor: vendor, Name: vendor + " GPU (see inventory)", Detail: out}}, ""
}
func (m *Manager) discoverSetup(parent context.Context, in SetupDiscoveryInput) (SetupDiscovery, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	result := SetupDiscovery{ScannedAt: time.Now().UTC().Format(time.RFC3339Nano), Platform: SetupDiscoveryPlatform{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUCount: runtime.NumCPU(), MemoryBytes: setupMemoryBytes(ctx)}, GPUs: []SetupDiscoveryGPU{}, Tools: []SetupDiscoveryTool{}, Services: []SetupDiscoveryService{}, Warnings: []string{}}
	if len(in.Endpoints) > 16 {
		return result, failure(400, "discovery_endpoint_limit", "Specify at most 16 model endpoints")
	}
	credential, e := setupModelCredential(in.APIKeyEnv)
	if e != nil {
		return result, failure(400, "discovery_credential", e.Error())
	}
	explicit := map[string]bool{}
	endpoints := setupLocalEndpoints()
	seen := map[string]bool{}
	for _, v := range endpoints {
		seen[v] = true
	}
	for _, raw := range in.Endpoints {
		endpoint, e := setupModelEndpoint(raw)
		if e != nil {
			return result, failure(400, "invalid_model_endpoint", e.Error())
		}
		explicit[endpoint] = true
		if !seen[endpoint] {
			endpoints = append(endpoints, endpoint)
			seen[endpoint] = true
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, spec := range []struct{ id, path string }{{"kun", m.kunExecutable()}, {"codex", m.codexExecutable()}, {"docker", "docker"}, {"node", "node"}, {"npm", "npm"}, {"vllm", "vllm"}, {"llama-server", "llama-server"}, {"ollama", "ollama"}} {
		spec := spec
		wg.Add(1)
		go func() {
			defer wg.Done()
			tool := SetupDiscoveryTool{ID: spec.id, Path: spec.path, Status: "unavailable"}
			path, e := exec.LookPath(spec.path)
			// The runtime prefers its configured/adjacent worker. Discover an
			// already installed PATH worker too so setup can save its exact path.
			if e != nil && spec.id == "kun" && m.Kun == "" {
				path, e = exec.LookPath("kun")
			}
			if e == nil {
				tool.Path = path
				probe, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				tool.Version, e = setupReadCommand(probe, path, "--version")
				tool.Status = "ready"
				if e != nil {
					tool.Status = "error"
					tool.Error = diagnosticText(e.Error())
				}
			}
			mu.Lock()
			result.Tools = append(result.Tools, tool)
			mu.Unlock()
		}()
	}
	for _, spec := range []struct {
		id, vendor string
		args       []string
	}{{"nvidia-smi", "NVIDIA", nil}, {"rocm-smi", "AMD", []string{"--json", "--showproductname", "--showmeminfo", "vram", "--showdriverversion"}}, {"mthreads-gmi", "MUSA", nil}} {
		spec := spec
		if _, e := exec.LookPath(spec.id); e != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			probe, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			var gpus []SetupDiscoveryGPU
			var warning string
			if spec.id == "nvidia-smi" {
				gpus, warning = setupNVIDIAGPUs(probe)
			} else {
				gpus, warning = setupOtherGPUs(probe, spec.id, spec.vendor, spec.args...)
			}
			mu.Lock()
			result.GPUs = append(result.GPUs, gpus...)
			if warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
			mu.Unlock()
		}()
	}
	client := setupProbeClient(1500 * time.Millisecond)
	defer client.CloseIdleConnections()
	semaphore := make(chan struct{}, 10)
	services := make([]SetupDiscoveryService, len(endpoints))
	for n, endpoint := range endpoints {
		n, endpoint := n, endpoint
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			source := "loopback"
			key := ""
			if explicit[endpoint] {
				source = "explicit"
				key = credential
			}
			services[n] = setupDiscoverService(ctx, client, endpoint, source, key)
		}()
	}
	wg.Wait()
	for _, service := range services {
		if service.Endpoint == "" {
			continue
		}
		if service.Status == "listed" || service.Status == "empty" || service.AuthRequired || explicit[service.Endpoint] {
			result.Services = append(result.Services, service)
		}
	}
	sort.Slice(result.Tools, func(i, j int) bool { return result.Tools[i].ID < result.Tools[j].ID })
	sort.Slice(result.GPUs, func(i, j int) bool {
		return result.GPUs[i].Vendor+result.GPUs[i].Name < result.GPUs[j].Vendor+result.GPUs[j].Name
	})
	sort.Strings(result.Warnings)
	if ctx.Err() != nil {
		result.Warnings = append(result.Warnings, "Discovery reached its time budget; rerun or enter a specific endpoint")
	}
	if result.Platform.MemoryBytes == 0 {
		result.Warnings = append(result.Warnings, "System memory inventory is unavailable on this platform")
	}
	return result, nil
}

func setupTestModel(ctx context.Context, client *http.Client, in SetupModelTestInput, credential string) SetupModelTestResult {
	start := time.Now()
	r := SetupModelTestResult{Endpoint: in.Endpoint, Model: in.Model, CheckedAt: start.UTC().Format(time.RFC3339Nano), Status: "failed"}
	type choice struct {
		Message struct {
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	}
	var response struct {
		Choices []choice `json:"choices"`
	}
	base := map[string]any{"model": in.Model, "messages": []map[string]string{{"role": "user", "content": "Reply with the single word OK."}}, "max_tokens": 256, "stream": false}
	code, e := setupResponseJSON(ctx, client, http.MethodPost, in.Endpoint+"/chat/completions", credential, base, &response)
	if e != nil {
		r.Error = e.Error()
		if code == 401 || code == 403 {
			r.Status = "auth_required"
		}
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	if len(response.Choices) == 0 || response.Choices[0].Message.Role != "assistant" || len(response.Choices[0].Message.Content) == 0 || string(response.Choices[0].Message.Content) == "null" {
		r.Error = "inference response has no assistant content"
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	var content string
	if json.Unmarshal(response.Choices[0].Message.Content, &content) != nil || strings.TrimSpace(content) == "" {
		r.Error = "inference response has empty or unsupported assistant content"
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	r.InferenceOK = true
	r.Status = "inference_only"
	base["messages"] = []map[string]string{{"role": "user", "content": "Call the rundesk_setup_ping function with value OK. Do not call any other function."}}
	base["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": "rundesk_setup_ping", "description": "Return a setup probe acknowledgement. This probe executes no tools.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string", "enum": []string{"OK"}}}, "required": []string{"value"}, "additionalProperties": false}}}}
	// Exercise automatic tool choice, as an agent does. Named/forced function
	// calling can succeed on vLLM while automatic tool calling is disabled.
	base["tool_choice"] = "auto"
	base["max_tokens"] = 512
	response.Choices = nil
	_, e = setupResponseJSON(ctx, client, http.MethodPost, in.Endpoint+"/chat/completions", credential, base, &response)
	if e != nil {
		r.Error = "Automatic function calling probe failed: " + e.Error() + "; check the server tool parser/chat template"
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	validCall := -1
	if len(response.Choices) > 0 && response.Choices[0].Message.Role == "assistant" {
		for n, call := range response.Choices[0].Message.ToolCalls {
			if call.Type != "function" || call.Function.Name != "rundesk_setup_ping" || call.ID == "" || len(call.ID) > 256 || strings.ContainsAny(call.ID, "\r\n\x00") {
				continue
			}
			var args map[string]any
			if json.Unmarshal([]byte(call.Function.Arguments), &args) == nil && len(args) == 1 && args["value"] == "OK" {
				validCall = n
				break
			}
		}
	}
	if validCall < 0 {
		r.Error = "Inference worked, but the service/model did not produce a valid function call; configure tool calling in the model server"
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	if len(response.Choices[0].Message.ToolCalls) != 1 {
		r.Error = "The tool probe returned unexpected additional calls; only one setup ping is accepted"
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	call := response.Choices[0].Message.ToolCalls[validCall]
	// Complete the same no-op exchange to check the chat template can consume
	// a tool-role message. A valid generated call alone is insufficient for an
	// agent that must read a result and continue. No real tool is executed.
	base["messages"] = []any{
		map[string]any{"role": "user", "content": "Call the rundesk_setup_ping function with value OK. Do not call any other function."},
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{call}},
		map[string]any{"role": "tool", "tool_call_id": call.ID, "content": `{"value":"OK"}`},
		map[string]any{"role": "user", "content": "The setup ping has completed. Confirm completion briefly in plain text. Do not request more tools."},
	}
	delete(base, "tools")
	delete(base, "tool_choice")
	base["max_tokens"] = 256
	response.Choices = nil
	_, e = setupResponseJSON(ctx, client, http.MethodPost, in.Endpoint+"/chat/completions", credential, base, &response)
	if e != nil {
		r.Error = "Tool-result round trip failed: " + e.Error() + "; check that the server chat template supports tool-role messages"
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	content = ""
	if len(response.Choices) == 0 || response.Choices[0].Message.Role != "assistant" || len(response.Choices[0].Message.ToolCalls) != 0 || json.Unmarshal(response.Choices[0].Message.Content, &content) != nil || strings.TrimSpace(content) == "" {
		r.Error = "The service did not return a valid final assistant message after receiving the tool result"
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	r.RoundTripOK = true
	r.ToolsOK = true
	r.Status = "ready"
	r.LatencyMs = time.Since(start).Milliseconds()
	return r
}
func (s *Server) setupDiscoveryRoutes(mux *http.ServeMux) {
	discover := func(w http.ResponseWriter, r *http.Request) {
		var in SetupDiscoveryInput
		if r.Method == http.MethodPost && !decode(w, r, &in) {
			return
		}
		result, e := s.Manager.discoverSetup(r.Context(), in)
		respond(w, result, e)
	}
	mux.HandleFunc("GET /api/setup/discovery", discover)
	mux.HandleFunc("POST /api/setup/discovery", discover)
	mux.HandleFunc("POST /api/setup/model-test", func(w http.ResponseWriter, r *http.Request) {
		var in SetupModelTestInput
		if !decode(w, r, &in) {
			return
		}
		if s.Manager.Demo {
			respond(w, nil, failure(400, "demo_no_model_calls", "Demo does not test a real model"))
			return
		}
		endpoint, e := setupModelEndpoint(in.Endpoint)
		if e != nil {
			respond(w, nil, failure(400, "invalid_model_endpoint", e.Error()))
			return
		}
		in.Endpoint = endpoint
		in.Model = strings.TrimSpace(in.Model)
		if in.Model == "" || len(in.Model) > 160 || strings.ContainsAny(in.Model, "\r\n\x00") {
			respond(w, nil, failure(400, "invalid_model", "Choose a model from discovery or enter its exact model ID"))
			return
		}
		credential, e := setupModelCredential(in.APIKeyEnv)
		if e != nil {
			respond(w, nil, failure(400, "model_credential", e.Error()))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		client := setupProbeClient(40 * time.Second)
		defer client.CloseIdleConnections()
		result := setupTestModel(ctx, client, in, credential)
		result.Error = diagnosticText(result.Error)
		evidence := setupModelTestEvidence{SetupModelTestResult: result, APIKeyEnv: in.APIKeyEnv, CredentialHash: keyHash(credential)}
		if e = s.Manager.Store.Put("setup-model-test", setupModelTestKey(in.Endpoint, in.Model, in.APIKeyEnv), evidence); e != nil {
			respond(w, nil, e)
			return
		}
		writeJSON(w, 200, result)
	})
}
