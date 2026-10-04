//go:build linux

package app

import (
	"os/exec"
	"syscall"
	"time"
)

func prepareBuildCommand(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.WaitDelay = 5 * time.Second
}
func interruptBuildCommand(c *exec.Cmd) { _ = syscall.Kill(-c.Process.Pid, syscall.SIGINT) }
func killBuildCommand(c *exec.Cmd)      { _ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
