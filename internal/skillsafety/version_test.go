package skillsafety

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeVersionUsesOnlyBoundedTrustedVersionArguments(t *testing.T) {
	arguments := filepath.Join(t.TempDir(), "args")
	exe := writeScanner(t, "printf '%s\\n' \"$@\" > "+shellQuote(arguments)+"\nprintf 'SkillSpector v2.13.4'")
	banner, err := ProbeVersion(context.Background(), exe)
	if err != nil || banner != "SkillSpector v2.13.4" {
		t.Fatal(banner, err)
	}
	data, err := os.ReadFile(arguments)
	if err != nil || string(data) != "--version\n" {
		t.Fatal(string(data), err)
	}
	_, err = ProbeVersion(context.Background(), filepath.Join(t.TempDir(), "missing-executable"))
	if !errors.Is(err, ErrVersionUnavailable) {
		t.Fatal(err)
	}
	for _, script := range []string{"printf secret-value >&2; exit 3", "true", "head -c 5000 /dev/zero"} {
		_, err = ProbeVersion(context.Background(), writeScanner(t, script))
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatal("unbounded/empty/failed version command passed", err)
		}
	}
}

func TestProbeVersionCancellationCleansDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	child := "sleep 0.3; printf survived > " + shellQuote(marker)
	exe := writeScanner(t, "sh -c "+shellQuote(child)+" &\nwait")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := ProbeVersion(ctx, exe)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("version helper child survived cancellation", err)
	}
}
