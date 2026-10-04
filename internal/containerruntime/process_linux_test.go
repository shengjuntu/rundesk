package containerruntime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "supervise":
			if e := Supervise(os.Args[2], os.Args[3:]); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
			os.Exit(0)
		case "detached":
			c := exec.Command(os.Args[0], "heartbeat", os.Args[2])
			c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if e := c.Start(); e != nil {
				os.Exit(3)
			}
			for {
				time.Sleep(time.Second)
			}
		case "heartbeat":
			for n := 0; ; n++ {
				_ = os.WriteFile(os.Args[2], []byte(strconv.Itoa(n)), 0600)
				time.Sleep(20 * time.Millisecond)
			}
		}
	}
	os.Exit(m.Run())
}
func TestSupervisorKillsDetachedDescendantsOnly(t *testing.T) {
	dir := t.TempDir()
	lease := filepath.Join(dir, "lease")
	os.WriteFile(lease, []byte("ok"), 0600)
	heart := filepath.Join(dir, "child")
	otherHeart := filepath.Join(dir, "other")
	exe, _ := os.Executable()
	other := exec.Command(exe, "heartbeat", otherHeart)
	if e := other.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { other.Process.Kill(); other.Wait() }()
	cmd := exec.Command(exe, "supervise", lease, exe, "detached", heart)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	defer stdin.Close()
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, e = os.Stat(heart); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	os.Remove(lease)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not revoke")
	}
	time.Sleep(100 * time.Millisecond)
	one, _ := os.ReadFile(heart)
	a, _ := os.ReadFile(otherHeart)
	time.Sleep(150 * time.Millisecond)
	two, _ := os.ReadFile(heart)
	b, _ := os.ReadFile(otherHeart)
	if string(one) != string(two) {
		t.Fatal("detached tool survived revoked lease")
	}
	if string(a) == string(b) {
		t.Fatal("unrelated session was killed")
	}
}
func TestExpiredLeaseDoesNotStartChild(t *testing.T) {
	dir := t.TempDir()
	lease := filepath.Join(dir, "lease")
	os.WriteFile(lease, nil, 0600)
	old := time.Now().Add(-time.Minute)
	os.Chtimes(lease, old, old)
	if e := Supervise(lease, []string{"/bin/false"}); e == nil {
		t.Fatal("expired lease accepted")
	}
}
