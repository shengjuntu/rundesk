//go:build !linux

package containerruntime

import (
	"errors"
	"os/exec"
)

func prepareProcess(*exec.Cmd) error { return errors.New("container supervision requires Linux") }
func killChildren()                  {}
