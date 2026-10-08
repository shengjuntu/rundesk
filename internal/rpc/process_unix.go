//go:build linux || darwin

package rpc

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const CleanupMode = "process-group"

func prepareProcess(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cmd.SysProcAttr.Setsid || cmd.SysProcAttr.Pgid != 0 || cmd.SysProcAttr.Foreground {
		return fmt.Errorf("RPC process must own a private process group")
	}
	cmd.SysProcAttr.Setpgid = true
	return nil
}
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	e := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if e == syscall.ESRCH {
		return nil
	}
	return e
}
