//go:build !linux

package app

import "os/exec"

func prepareBuildCommand(c *exec.Cmd)   {}
func interruptBuildCommand(c *exec.Cmd) { _ = c.Process.Kill() }
func killBuildCommand(c *exec.Cmd)      { _ = c.Process.Kill() }
