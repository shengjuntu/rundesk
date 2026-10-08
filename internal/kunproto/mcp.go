package kunproto

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
)

type MCPServer struct {
	Name           string            `json:"name"`
	Revision       string            `json:"revision"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	URL            string            `json:"url,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	StartupTimeout float64           `json:"startupTimeoutSeconds"`
	ToolTimeout    float64           `json:"toolTimeoutSeconds"`
	Allowlist      bool              `json:"allowlist"`
	EnabledTools   []string          `json:"enabledTools,omitempty"`
	DisabledTools  []string          `json:"disabledTools,omitempty"`
	ApprovalModes  map[string]string `json:"approvalModes,omitempty"`
}

var mcpName = regexp.MustCompile("^[A-Za-z0-9_-]{1,64}$")
var envName = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]{0,127}$")
var headerName = regexp.MustCompile("^[!#$%&'*+.^_" + "`" + "|~0-9A-Za-z-]+$")

func (s MCPServer) Validate() error {
	if !mcpName.MatchString(s.Name) {
		return fmt.Errorf("invalid MCP server name")
	}
	if (s.Command == "") == (s.URL == "") {
		return fmt.Errorf("%s: specify one command or URL", s.Name)
	}
	if strings.ContainsRune(s.Command, 0) || len(s.Command) > 4096 || len(s.Args) > 128 {
		return fmt.Errorf("%s: invalid command/arguments", s.Name)
	}
	for _, arg := range s.Args {
		if len(arg) > 8192 || strings.ContainsRune(arg, 0) {
			return fmt.Errorf("%s: invalid argument", s.Name)
		}
	}
	if s.URL != "" {
		u, e := url.Parse(s.URL)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("%s: MCP URL must be HTTP(S), without credentials, query or fragment", s.Name)
		}
	}
	for _, n := range []float64{s.StartupTimeout, s.ToolTimeout} {
		if math.IsNaN(n) || n < 0.1 || n > 600 {
			return fmt.Errorf("%s: MCP timeout must be 0.1–600 seconds", s.Name)
		}
	}
	for k, v := range s.Environment {
		if !envName.MatchString(k) || strings.ContainsRune(v, 0) || len(v) > 65536 {
			return fmt.Errorf("%s: invalid MCP environment entry", s.Name)
		}
	}
	for k, v := range s.Headers {
		if !headerName.MatchString(k) || strings.ContainsAny(v, "\r\n\x00") || len(v) > 65536 {
			return fmt.Errorf("%s: invalid MCP HTTP header", s.Name)
		}
		switch strings.ToLower(k) {
		case "host", "content-length", "content-type", "accept", "mcp-session-id", "mcp-protocol-version":
			return fmt.Errorf("%s: reserved MCP HTTP header %s", s.Name, k)
		}
	}
	if len(s.EnabledTools) > 256 || len(s.DisabledTools) > 256 || len(s.ApprovalModes) > 256 || len(JSON(s)) > 512<<10 {
		return fmt.Errorf("%s: MCP configuration too large", s.Name)
	}
	for name, mode := range s.ApprovalModes {
		if name == "" || len(name) > 256 {
			return fmt.Errorf("%s: invalid tool name", s.Name)
		}
		if mode != "approve" && mode != "prompt" {
			return fmt.Errorf("%s: Kun supports approval_mode approve or prompt; %s is unsupported", s.Name, mode)
		}
	}
	return nil
}

type MCPStatus struct {
	Name            string `json:"name"`
	Revision        string `json:"configRevision"`
	Transport       string `json:"transport"`
	Status          string `json:"status"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	ToolCount       int    `json:"toolCount"`
	Error           string `json:"error,omitempty"`
}
type MCPTool struct {
	Alias        string          `json:"alias"`
	Server       string          `json:"server"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	Annotations  json.RawMessage `json:"annotations,omitempty"`
	ApprovalMode string          `json:"approvalMode"`
}
type ToolApproval struct {
	CallID    string `json:"callId"`
	Server    string `json:"server"`
	Tool      string `json:"tool"`
	Arguments string `json:"arguments"`
	Decision  string `json:"decision,omitempty"`
}
