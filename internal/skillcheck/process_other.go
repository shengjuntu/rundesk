//go:build !linux && !darwin && !windows

package skillcheck

import (
	"errors"
	"os/exec"
)

func runValidationCommand(cmd *exec.Cmd) error {
	return errors.New("validation process-tree cleanup is unavailable on this platform")
}
