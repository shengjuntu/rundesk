//go:build linux || darwin

package skillsafety

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// A scanner can launch model servers or other helpers. Give it a private process
// group so cancelling a scan also terminates those descendants.
func runSpectorCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = kill
	if err := cmd.Start(); err != nil {
		return err
	}
	// Also remove helpers left behind after a normal or unsuccessful scanner exit.
	defer kill()
	return cmd.Wait()
}
