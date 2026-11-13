//go:build !linux && !darwin && !windows

package skillsafety

import (
	"errors"
	"os/exec"
)

func runSpectorCommand(cmd *exec.Cmd) error {
	return errors.New("scanner process-tree cleanup is unavailable on this platform")
}
