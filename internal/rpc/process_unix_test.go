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
			_ = os.WriteFile(path, []byte(fmt.Sprint(time.Now().UnixNano())), 0600)
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
			before, _ := os.ReadFile(heart)
			foreignBefore, _ := os.ReadFile(otherHeart)
			time.Sleep(100 * time.Millisecond)
			after, _ := os.ReadFile(heart)
			foreignAfter, _ := os.ReadFile(otherHeart)
			if len(before) == 0 || string(before) != string(after) {
				t.Fatal("owned descendant kept running")
			}
			if len(foreignAfter) == 0 || string(foreignBefore) == string(foreignAfter) {
				t.Fatal("unrelated process affected")
			}
			if c.ProcessInfo().CleanupMode != "process-group" || !c.ProcessInfo().Exited {
				t.Fatal(c.ProcessInfo())
			}
		})
	}
}
