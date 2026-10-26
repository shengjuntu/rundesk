//go:build !linux && !darwin && !windows

package kun

import "os/exec"

func configureBashProcess(cmd *exec.Cmd) func() error {
	return func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
}
