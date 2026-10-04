package containerruntime

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func prepareProcess(c *exec.Cmd) error {
	fd, e := unix.PidfdOpen(os.Getpid(), 0)
	if e != nil {
		return fmt.Errorf("container supervision requires pidfd support (Linux 5.3+): %w", e)
	}
	e = unix.PidfdSendSignal(fd, 0, nil, 0)
	_ = unix.Close(fd)
	if e != nil {
		return fmt.Errorf("container supervision cannot signal process handles: %w", e)
	}
	if e := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); e != nil {
		return e
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}
func namespacePIDs(path string) []int {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "NSpid:") {
			out := []int{}
			for _, v := range strings.Fields(strings.TrimPrefix(line, "NSpid:")) {
				pid, e := strconv.Atoi(v)
				if e != nil {
					return nil
				}
				out = append(out, pid)
			}
			return out
		}
	}
	return nil
}

type processIdentity struct {
	parent int
	start  string
}

func procIdentity(pid int) (processIdentity, bool) {
	b, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if e != nil {
		return processIdentity{}, false
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return processIdentity{}, false
	}
	fields := strings.Fields(string(b)[end+1:])
	if len(fields) < 20 {
		return processIdentity{}, false
	}
	parent, e := strconv.Atoi(fields[1])
	return processIdentity{parent: parent, start: fields[19]}, e == nil
}
func killChildren() {
	// procfs may belong to an outer PID namespace. Never use its numeric PIDs
	// directly with kill(2); map the relevant NSpid column to this helper's namespace.
	self, e := os.Readlink("/proc/self")
	if e != nil {
		return
	}
	root, e := strconv.Atoi(self)
	if e != nil {
		return
	}
	depth := len(namespacePIDs("/proc/self/status")) - 1
	if depth < 0 {
		return
	}
	for pass := 0; pass < 4; pass++ {
		entries, e := os.ReadDir("/proc")
		if e != nil {
			return
		}
		identities := map[int]processIdentity{}
		for _, entry := range entries {
			pid, e := strconv.Atoi(entry.Name())
			if e != nil {
				continue
			}
			if v, ok := procIdentity(pid); ok {
				identities[pid] = v
			}
		}
		owned := map[int]bool{root: true}
		for changed := true; changed; {
			changed = false
			for pid, v := range identities {
				if owned[v.parent] && !owned[pid] {
					owned[pid] = true
					changed = true
				}
			}
		}
		for pid := range owned {
			if pid == root {
				continue
			}
			pids := namespacePIDs("/proc/" + strconv.Itoa(pid) + "/status")
			if len(pids) <= depth {
				continue
			}
			target := pids[depth]
			if target <= 1 || target == os.Getpid() {
				continue
			}
			fd, e := unix.PidfdOpen(target, 0)
			if e != nil {
				continue
			}
			// Recheck start time after opening the handle, so a reused PID is not killed.
			if now, ok := procIdentity(pid); ok && now.start == identities[pid].start {
				_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
			}
			_ = unix.Close(fd)
		}
	}
}
