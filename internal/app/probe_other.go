//go:build !linux && !darwin

package app

import "os/exec"

func configureProbeProcess(cmd *exec.Cmd) {}
