package kun

import (
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestKunArtifactFIFOIsBounded(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan *p.ArtifactEvidence, 1)
	go func() { done <- captureArtifacts(dir, []p.ArtifactRule{{Path: "pipe", Kind: "file_exists"}}) }()
	select {
	case result := <-done:
		if result.Results[0].Reason != "not_regular" {
			t.Fatal(result)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked capture")
	}
}
