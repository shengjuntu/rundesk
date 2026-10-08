//go:build !linux && !darwin

package mcp

import "os/exec"

// Other platforms retain exec.CommandContext's direct-process cancellation.
func ConfigureProcess(cmd *exec.Cmd) func() error { return func() error { return cmd.Process.Kill() } }
