package skillcheck

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const validationOutputLimit = 64 << 10

type cappedOutput struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (output *cappedOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	remaining := validationOutputLimit - len(output.data)
	if len(data) > remaining {
		output.overflow = true
	}
	if remaining > 0 {
		if remaining > len(data) {
			remaining = len(data)
		}
		output.data = append(output.data, data[:remaining]...)
	}
	return len(data), nil
}

func (output *cappedOutput) snapshot() ([]byte, bool) {
	output.mu.Lock()
	defer output.mu.Unlock()
	return append([]byte(nil), output.data...), output.overflow
}

// This success text is emitted by internal/cli/validate.go at v0.15.0. The
// command has no JSON mode. An exit code alone is not evidence of validation.
var validationSuccess = regexp.MustCompile(`(?m)^✓ eval\.yaml is valid \(loaded ([0-9]+) case\(s\)\)\s*$`)

// ParseValidationOutput is shared by isolated container callers and the trusted
// local adapter. The pinned CLI emits text, not JSON; a successful exit without
// its explicit nonempty case count is not a completed validation.
func ParseValidationOutput(output []byte, exitCode int) (Validation, error) {
	result := Validation{Status: "failed", Warnings: []string{}}
	if len(output) > 2*validationOutputLimit {
		result.Warnings = append(result.Warnings, "Validation output exceeded its 128 KiB combined budget.")
		return result, failure("output_limit", "Skill-Up validation output was truncated")
	}
	result.Output = clean(strings.TrimSpace(string(output)))
	if exitCode != 0 {
		return result, failure("validation_failed", "Skill-Up validation failed; inspect the redacted output")
	}
	match := validationSuccess.FindAllSubmatch(output, -1)
	if len(match) != 1 || len(match[0]) != 2 {
		return result, failure("invalid_validation_output", "Skill-Up returned no unique recognizable completed validation")
	}
	count, err := strconv.Atoi(string(match[0][1]))
	if err != nil || count <= 0 || count > MaxCases {
		return result, failure("invalid_validation_output", "Skill-Up did not validate 1–1000 cases")
	}
	result.Cases, result.Status = count, "success"
	return result, nil
}

// Validate is a low-level protocol adapter for a TRUSTED or already isolated
// suite directory. Skill-Up's loader resolves YAML file references, so a temp
// directory alone is NOT a sandbox. Production callers must perform untrusted
// validation inside their managed container; this function does not provide
// filesystem/network isolation and never invokes the run command.
func Validate(ctx context.Context, dir, executable string) (Validation, error) {
	result := Validation{Status: "failed", Warnings: []string{}}
	if strings.TrimSpace(executable) == "" {
		executable = "skill-up"
	}
	command, err := exec.LookPath(executable)
	if err != nil {
		result.Status = "unavailable"
		return result, failure("unavailable", "Skill-Up is not installed or its configured command is unavailable")
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return result, failure("invalid_directory", "Cannot resolve the trusted suite directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, failure("invalid_directory", "Suite must be a prepared ordinary directory")
	}
	for _, name := range []string{"evals", "evals/eval.yaml"} {
		info, err = os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || name == "evals/eval.yaml" && (!info.Mode().IsRegular() || info.Size() > MaxReportBytes) || name == "evals" && !info.IsDir() {
			return result, failure("invalid_config", "A regular evals/eval.yaml is required in the prepared suite")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "validate", filepath.Join(dir, "evals", "eval.yaml"))
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	stdout, stderr := &cappedOutput{}, &cappedOutput{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := runValidationCommand(cmd)
	out, overOut := stdout.snapshot()
	logs, overErr := stderr.snapshot()
	// Redact before display. A validation error can include content copied from
	// an input case; no raw output bytes are returned by this adapter.
	result.Output = clean(strings.TrimSpace(string(out) + "\n" + string(logs)))
	if ctx.Err() != nil {
		result.Warnings = append(result.Warnings, "Validation was canceled or exceeded 30 seconds.")
		return result, ctx.Err()
	}
	if overOut || overErr {
		result.Warnings = append(result.Warnings, "Validation output exceeded its 64 KiB stream budget.")
		return result, failure("output_limit", "Skill-Up validation output was truncated")
	}
	exitCode := 0
	if runErr != nil {
		var exited *exec.ExitError
		if errors.As(runErr, &exited) {
			exitCode = exited.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return ParseValidationOutput(append(append(out, '\n'), logs...), exitCode)
}

var _ io.Writer = (*cappedOutput)(nil)
