//go:build linux || darwin

package skillsafety

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These fixtures are explicitly configured fake scanner CLIs. No skill source is
// executed. A delayed write detects a descendant surviving scanner cleanup.
func TestSpectorCancellationStopsDescendants(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("Read this fixture."), 0600); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(directory, "child-ready")
	marker := filepath.Join(directory, "child-survived")
	child := "printf ready > " + shellQuote(ready) + "; sleep 0.6; printf survived > " + shellQuote(marker)
	command := writeScanner(t, "sh -c "+shellQuote(child)+" &\nwait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		report Report
		err    error
	}
	done := make(chan result, 1)
	go func() {
		report, err := RunSkillSpector(ctx, directory, command, false)
		done <- result{report, err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake scanner child did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || result.report.Status != "incomplete" {
			t.Fatalf("report=%+v error=%v", result.report, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("scanner cancellation left inherited output pipes open")
	}
	time.Sleep(750 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scanner child survived cancellation: stat=%v", err)
	}
}

func TestSpectorNormalExitCleansUpDescendants(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("Read this fixture."), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(directory, "child-survived")
	child := "sleep 0.6; printf survived > " + shellQuote(marker)
	command := writeScanner(t, "sh -c "+shellQuote(child)+" >/dev/null 2>&1 &\nprintf '%s' "+shellQuote(string(spectorJSON("SAFE", true, true))))
	report, err := RunSkillSpector(context.Background(), directory, command, false)
	if err != nil || report.Status != "completed" {
		t.Fatalf("report=%+v error=%v", report, err)
	}
	time.Sleep(750 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scanner child survived normal exit: stat=%v", err)
	}
}
