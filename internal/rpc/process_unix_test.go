//go:build linux || darwin

package rpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTreeFixture(t *testing.T) {
	mode := os.Getenv("RUNDESK_TREE_FIXTURE")
	if mode == "" {
		return
	}
	path := os.Getenv("RUNDESK_TREE_HEART")
	if mode == "heartbeat" {
		for {
			// Readers must see a complete heartbeat, never WriteFile's truncate window.
			if err := os.WriteFile(path+".tmp", []byte(fmt.Sprint(time.Now().UnixNano())), 0600); err != nil {
				os.Exit(8)
			}
			if err := os.Rename(path+".tmp", path); err != nil {
				os.Exit(9)
			}
			time.Sleep(15 * time.Millisecond)
		}
	}
	child := exec.Command(os.Args[0], "-test.run=^TestTreeFixture$")
	child.Env = append(os.Environ(), "RUNDESK_TREE_FIXTURE=heartbeat")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if e := child.Start(); e != nil {
		os.Exit(7)
	}
	for n := 0; n < 100; n++ {
		if _, e := os.Stat(path); e == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Fprintf(os.Stdout, "{\"method\":\"tree/ready\",\"params\":{\"pid\":%d}}\n", child.Process.Pid)
	if mode == "exit" {
		for n := 0; n < 100; n++ {
			fmt.Fprintf(os.Stdout, "{\"method\":\"tree/final\",\"params\":{\"n\":%d}}\n", n)
		}
		os.Exit(0)
	}
	if mode == "invalid" {
		fmt.Fprintln(os.Stdout, "invalid-json")
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
	}
	time.Sleep(20 * time.Second)
	os.Exit(0)
}
func TestOwnedGroupCleanupAndFinalFrames(t *testing.T) {
	for _, mode := range []string{"close", "exit", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			heart := filepath.Join(dir, "child")
			otherHeart := filepath.Join(dir, "other")
			other := exec.Command(os.Args[0], "-test.run=^TestTreeFixture$")
			other.Env = append(os.Environ(), "RUNDESK_TREE_FIXTURE=heartbeat", "RUNDESK_TREE_HEART="+otherHeart)
			if e := other.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() { other.Process.Kill(); other.Wait() }()
			cmd := exec.Command(os.Args[0], "-test.run=^TestTreeFixture$")
			cmd.Env = append(os.Environ(), "RUNDESK_TREE_FIXTURE="+mode, "RUNDESK_TREE_HEART="+heart)
			ready := make(chan struct{}, 1)
			finals := 0
			c, e := Start(cmd, func(msg Message) {
				if msg.Method == "tree/ready" {
					ready <- struct{}{}
				}
				if msg.Method == "tree/final" {
					var p map[string]int
					json.Unmarshal(msg.Params, &p)
					finals++
				}
			}, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			select {
			case <-ready:
			case <-time.After(3 * time.Second):
				t.Fatal("fixture did not start")
			}
			if mode == "close" {
				c.Close()
			}
			select {
			case <-c.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("descendant-held pipes stranded connection")
			}
			if mode == "exit" && finals != 100 {
				t.Fatalf("lost buffered final frames: %d", finals)
			}
			before, err := os.ReadFile(heart)
			if err != nil || len(before) == 0 {
				t.Fatalf("owned heartbeat missing after cleanup: %q, %v", before, err)
			}
			// Establish the foreign baseline only after cleanup, so an unrelated
			// process killed by cleanup cannot pass by having ticked beforehand.
			foreignBefore := awaitTreeHeartbeat(t, otherHeart, nil)
			time.Sleep(100 * time.Millisecond)
			after, err := os.ReadFile(heart)
			if err != nil || string(before) != string(after) {
				t.Fatalf("owned descendant kept running: before=%q after=%q error=%v", before, after, err)
			}
			// A scheduling delay is not evidence that the process was killed. Require
			// actual progress within a bounded interval, retaining the isolation check.
			awaitTreeHeartbeat(t, otherHeart, foreignBefore)
			if c.ProcessInfo().CleanupMode != "process-group" || !c.ProcessInfo().Exited {
				t.Fatal(c.ProcessInfo())
			}
		})
	}
}

// Await a complete tick that differs from a post-cleanup baseline. If the
// foreign process was killed, no new value can appear and this still fails.
func awaitTreeHeartbeat(t *testing.T, path string, before []byte) []byte {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last []byte
	var err error
	for time.Now().Before(deadline) {
		last, err = os.ReadFile(path)
		if err == nil && len(last) > 0 && string(last) != string(before) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unrelated process stopped advancing: before=%q after=%q error=%v", before, last, err)
	return nil
}
