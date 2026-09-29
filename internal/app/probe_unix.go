//go:build linux || darwin

package app

import (
	"os"
	"os/exec"
	"syscall"
)

// Kill the wrapper and its descendants on timeout, including npm's Node launcher.
func configureProbeProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != syscall.ESRCH {
			return err
		}
		return os.ErrProcessDone
	}
}
