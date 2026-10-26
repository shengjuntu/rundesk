package kun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Bash is host execution, not an os.Root file operation. It requires a separate
// administrator grant, then the same durable approval/dispatch ledger as MCP.
const bashOutputLimit = 64 << 10

type bashOutput struct {
	mu    sync.Mutex
	tail  []byte
	bytes int64
}

func (b *bashOutput) Write(v []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(v)
	b.bytes += int64(n)
	if n >= bashOutputLimit {
		b.tail = append(b.tail[:0], v[n-bashOutputLimit:]...)
	} else {
		if len(b.tail)+n > bashOutputLimit {
			b.tail = append(b.tail[:0], b.tail[len(b.tail)+n-bashOutputLimit:]...)
		}
		b.tail = append(b.tail, v...)
	}
	return n, nil
}
func (b *bashOutput) snapshot() (string, int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(string(b.tail), "�"), b.bytes, b.bytes > int64(len(b.tail))
}

type bashResult struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       *int   `json:"exitCode"`
	TimedOut       bool   `json:"timedOut"`
	Cancelled      bool   `json:"cancelled"`
	Truncated      bool   `json:"truncated"`
	StdoutBytes    int64  `json:"stdoutBytes"`
	StderrBytes    int64  `json:"stderrBytes"`
	DurationMillis int64  `json:"durationMillis"`
	Error          string `json:"error,omitempty"`
}

func resolveBash(custom string) (string, error) {
	if custom != "" {
		return exec.LookPath(custom)
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
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Bash executable not found; install Bash (Git for Windows on Windows) or configure an absolute bashPath")
}
func executeBash(ctx context.Context, workspace string, cfg p.Config, call p.ToolCall) (toolResult, error) {
	var args struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return toolResult{Output: err.Error(), IsError: true}, nil
	}
	if !cfg.AllowBash || strings.TrimSpace(args.Command) == "" || len(args.Command) > 32<<10 || strings.ContainsRune(args.Command, 0) || args.Timeout < 0 || args.Timeout > 600 {
		return toolResult{Output: "Bash disabled or invalid command/timeout", IsError: true}, nil
	}
	shell, err := resolveBash(cfg.BashPath)
	if err != nil {
		return toolResult{Output: err.Error(), IsError: true}, nil
	}
	limit := cfg.BashTimeoutSeconds
	if limit == 0 {
		limit = 120
	}
	if args.Timeout > 0 && args.Timeout < limit {
		limit = args.Timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(limit)*time.Second)
	defer cancel()
	// Non-login, non-interactive: shell profiles are not execution prerequisites.
	cmd := exec.CommandContext(runCtx, shell, "--noprofile", "--norc", "-c", args.Command)
	cmd.Dir = workspace
	// BASH_ENV/ENV would inject an unrecorded startup script into every command.
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !strings.EqualFold(name, "BASH_ENV") && !strings.EqualFold(name, "ENV") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	stdout, stderr := &bashOutput{}, &bashOutput{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cleanup := configureBashProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	started := time.Now()
	err = cmd.Run()
	_ = cleanup()
	a, ab, at := stdout.snapshot()
	b, bb, bt := stderr.snapshot()
	result := bashResult{Stdout: a, Stderr: b, StdoutBytes: ab, StderrBytes: bb, Truncated: at || bt, DurationMillis: time.Since(started).Milliseconds()}
	result.TimedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
	result.Cancelled = errors.Is(runCtx.Err(), context.Canceled)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() && !result.TimedOut && !result.Cancelled {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	if err != nil {
		result.Error = err.Error()
	}
	raw := p.JSON(result)
	return toolResult{Output: string(raw), Raw: raw, IsError: err != nil || result.TimedOut || result.Cancelled}, nil
}
func builtinSideEffect(name string) bool {
	return name == "write_file" || name == "edit_file" || name == "bash"
}
