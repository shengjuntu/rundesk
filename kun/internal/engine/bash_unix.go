//go:build linux || darwin

package kun

import (
	"os"
	"os/exec"
	"syscall"
)

// Every call owns one process group. Cancel, timeout and normal shell exit
// terminate its remaining children; this tool does not provide detached jobs.
func configureBashProcess(cmd *exec.Cmd) func() error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = kill
	return kill
}
