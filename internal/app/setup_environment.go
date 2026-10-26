package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	p "github.com/your-org/kun/sdk/protocol"
)

type EnvironmentCheck struct {
	ID       string `json:"id"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
}
type SetupEnvironment struct {
	InstanceID      string             `json:"instanceId"`
	Backend         string             `json:"backend"`
	Ready           bool               `json:"ready"`
	InferenceTested bool               `json:"inferenceTested"`
	Demo            bool               `json:"demo"`
	Checks          []EnvironmentCheck `json:"checks"`
}

func probeVersion(parent context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	var out limitedOutput
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout, cmd.Stderr = &out, &out
	configureProbeProcess(cmd)
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// This is a local prerequisite check. It does not start a worker, connect to a
// provider, infer with a model, or install packages. Secrets are never returned.
func (m *Manager) setupEnvironment(iid, wid string) (SetupEnvironment, error) {
	i, err := m.Instance(iid)
	if err != nil {
		return SetupEnvironment{}, err
	}
	kind := runtimeKind(i.AgentRuntime.Kind)
	applicationsOnly := false
	if i.ID == DefaultInstance {
		if setup, e := m.setupState(); e == nil {
			applicationsOnly = setup.ApplicationsOnly
		}
	}
	result := SetupEnvironment{InstanceID: i.ID, Backend: kind, Ready: true, Demo: m.Demo, Checks: []EnvironmentCheck{}}
	add := func(id string, required bool, status, detail string) {
		result.Checks = append(result.Checks, EnvironmentCheck{id, required, status, detail})
		if required && status != "ready" {
			result.Ready = false
		}
	}
	if _, err := agentBackend(kind); err != nil {
		add("backend", true, "unsupported", err.Error())
		return result, nil
	}
	if wid != "" {
		w, e := m.Workspace(wid)
		if e == nil {
			e = writableDirectory(w.Path)
		}
		if e != nil {
			add("workspace", true, "unavailable", e.Error())
		} else {
			add("workspace", true, "ready", w.Path)
		}
	} else {
		add("workspace", true, "unconfigured", "Select a workspace")
	}
	for _, id := range []string{"kun", "codex", "bash", "node", "npm", "docker"} {
		path, required := id, id == kind
		switch id {
		case "kun":
			path = m.kunExecutable()
		case "codex":
			path = m.codexExecutable()
			required = kind == "codex" && i.Execution.normalized().Mode == "local"
		case "bash":
			required = kind == "kun" && i.AgentRuntime.AllowBash
			path = setupBashPath(i.AgentRuntime.BashPath)
		case "docker":
			required = i.Execution.normalized().Mode == "docker"
		}
		if applicationsOnly {
			required = id == "docker"
		}
		resolved, e := exec.LookPath(path)
		if e != nil {
			add(id, required, "unavailable", path)
		} else {
			add(id, required, "ready", resolved)
		}
	}
	if applicationsOnly {
		result.Backend = "docker-applications"
		add("runtime-images", false, "unchecked", "Prepare an image in setup; each application still needs authentication and live checks")
	} else if kind == "kun" {
		cfg := i.AgentRuntime.Normalized()
		if err := cfg.Validate(); err != nil {
			add("configuration", true, "unconfigured", err.Error())
		} else {
			add("configuration", true, "ready", "Chat Completions configuration is valid; network and inference untested")
		}
		if cfg.APIKeyEnv == "" {
			add("credential", false, "skipped", "No credential environment variable configured")
		} else if os.Getenv(cfg.APIKeyEnv) == "" {
			add("credential", true, "unavailable", cfg.APIKeyEnv+" is unset")
		} else {
			add("credential", true, "ready", cfg.APIKeyEnv+" is set")
		}
		add("workerProtocol", false, "unchecked", "Requires Kun JSONL protocol "+fmt.Sprint(p.Version)+"; the worker handshake records its actual version and optional capabilities")
	} else {
		add("authentication", false, "unchecked", "Use the account check; local executable discovery does not prove authentication")
	}
	if m.Demo {
		result.Ready = false
		add("demo", false, "skipped", "Demo does not verify a real runtime")
	}
	return result, nil
}

func (s *Server) setupEnvironmentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/environment", func(w http.ResponseWriter, r *http.Request) {
		iid := r.URL.Query().Get("instanceId")
		if iid == "" {
			iid = DefaultInstance
		}
		wid := r.URL.Query().Get("workspaceId")
		if wid == "" {
			if saved, err := s.Manager.setupState(); err == nil {
				wid = saved.WorkspaceID
			}
		}
		result, err := s.Manager.setupEnvironment(iid, wid)
		respond(w, result, err)
	})
}

func setupBashPath(custom string) string {
	if custom != "" {
		return custom
	}
	candidates := []string{"/bin/bash", "bash"}
	if runtime.GOOS == "windows" {
		candidates = nil
		for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if dir := os.Getenv(key); dir != "" {
				candidates = append(candidates, filepath.Join(dir, "Git", "bin", "bash.exe"))
			}
		}
		candidates = append(candidates, "bash.exe")
	}
	for _, candidate := range candidates {
		if found, err := exec.LookPath(candidate); err == nil {
			return found
		}
	}
	return "bash"
}
