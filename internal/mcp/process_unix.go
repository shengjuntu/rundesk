//go:build linux || darwin

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

// MCP launchers may spawn children. Stop the entire owned process group.
func ConfigureProcess(cmd *exec.Cmd) func() error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != syscall.ESRCH {
			return err
		}
		return os.ErrProcessDone
	}
	if cmd.Cancel != nil {
		cmd.Cancel = kill
	}
	return kill
}
