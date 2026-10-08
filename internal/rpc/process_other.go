//go:build !linux && !darwin

package rpc

import (
	"os"
	"os/exec"
)

const CleanupMode = "direct-process"

func prepareProcess(cmd *exec.Cmd) error { return nil }
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}
