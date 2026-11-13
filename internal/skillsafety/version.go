package skillsafety

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrVersionUnavailable identifies a missing configured trusted executable.
var ErrVersionUnavailable = errors.New("trusted scanner executable is unavailable")

// ProbeVersion runs only --version on an administrator-selected trusted tool.
// The caller parses the returned version banner; it is never a safety verdict.
// Errors omit executable paths and raw logs. Process-tree cleanup is shared
// with the real scanner on all supported platforms.
func ProbeVersion(ctx context.Context, executable string) (string, error) {
	if strings.TrimSpace(executable) == "" {
		executable = "skillspector"
	}
	binary, err := exec.LookPath(executable)
	if err != nil {
		return "", ErrVersionUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	cmd.WaitDelay = time.Second
	stdout, stderr := &cappedOutput{limit: 4096}, &cappedOutput{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = runSpectorCommand(cmd)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	out, truncated := stdout.snapshot()
	_, logsTruncated := stderr.snapshot()
	if err != nil || truncated || logsTruncated || len(out) == 0 || !utf8.Valid(out) {
		return "", errors.New("trusted tool did not return a bounded successful version banner")
	}
	return strings.TrimSpace(Redact(string(out))), nil
}
