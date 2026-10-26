package servicelock

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLockFixture(t *testing.T) {
	path := os.Getenv("RUNDESK_LOCK_FIXTURE")
	if path == "" {
		return
	}
	l, e := Acquire(path)
	if e != nil {
		os.Exit(4)
	}
	defer l.Close()
	os.WriteFile(path+".ready", []byte("ready"), 0600)
	for {
		time.Sleep(time.Second)
	}
}
func TestExclusiveAndCrashRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockFixture$")
	cmd.Env = append(os.Environ(), "RUNDESK_LOCK_FIXTURE="+path)
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Process.Kill()
	ready := false
	for n := 0; n < 200; n++ {
		if _, e := os.Stat(path + ".ready"); e == nil {
			ready = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ready {
		t.Fatal("fixture did not acquire lock")
	}
	if l, e := Acquire(path); e == nil {
		l.Close()
		t.Fatal("second server acquired live lock")
	}
	if e := cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = cmd.Wait()
	l, e := Acquire(path)
	if e != nil {
		t.Fatal("crash left stale lock", e)
	}
	if e = l.Close(); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path)
	if e != nil || !strings.HasPrefix(string(b), marker) {
		t.Fatal("lock inode should persist")
	}
	again, e := Acquire(path)
	if e != nil {
		t.Fatal(e)
	}
	again.Close()
}
func TestLegacyLockNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.lock")
	old := []byte("pid=42\n")
	os.WriteFile(path, old, 0600)
	if l, e := Acquire(path); e == nil {
		l.Close()
		t.Fatal("overwrote legacy lock")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(old) {
		t.Fatal("legacy lock changed")
	}
}
