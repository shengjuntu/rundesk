package skillcheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func validationFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "evals"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "evals/eval.yaml"), []byte("# controlled validation fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func validationCommand(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX trusted validator protocol fixture")
	}
	file := filepath.Join(t.TempDir(), "skill-up")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestValidateUsesExactTrustedCLIProtocol(t *testing.T) {
	dir := validationFixture(t)
	cmd := validationCommand(t, "test \"$#\" = 2 && test \"$1\" = validate && test \"$2\" = \"$PWD/evals/eval.yaml\" || exit 7\nprintf '✓ eval.yaml is valid (loaded 2 case(s))\\n'")
	result, err := Validate(context.Background(), dir, cmd)
	if err != nil || result.Status != "success" || result.Cases != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestValidateNeverAcceptsExitCodeAlone(t *testing.T) {
	for name, body := range map[string]string{"empty": "exit 0", "unknown": "printf 'all good\\n'", "zero cases": "printf '✓ eval.yaml is valid (loaded 0 case(s))\\n'", "failure": "printf 'token = github_pat_1234567890ABCDEFGHIJKLMN\\n' >&2; exit 1", "overflow": "head -c 70000 /dev/zero"} {
		t.Run(name, func(t *testing.T) {
			result, err := Validate(context.Background(), validationFixture(t), validationCommand(t, body))
			if err == nil || result.Status == "success" || strings.Contains(result.Output, "github_pat_") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestValidateUnavailableInvalidDirectoryAndCancellation(t *testing.T) {
	result, err := Validate(context.Background(), validationFixture(t), filepath.Join(t.TempDir(), "missing"))
	if err == nil || result.Status != "unavailable" {
		t.Fatalf("%+v %v", result, err)
	}
	command := validationCommand(t, "exec sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	result, err = Validate(ctx, validationFixture(t), command)
	if !errors.Is(err, context.DeadlineExceeded) || result.Status != "failed" {
		t.Fatalf("%+v %v", result, err)
	}
	result, err = Validate(context.Background(), t.TempDir(), command)
	if err == nil || result.Status != "failed" {
		t.Fatalf("%+v %v", result, err)
	}
	if runtime.GOOS != "windows" {
		dir := t.TempDir()
		if err := os.Symlink(validationFixture(t), filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := Validate(context.Background(), filepath.Join(dir, "link"), command); err == nil {
			t.Fatal("accepted symlink suite root")
		}
	}
}

func TestParseValidationOutputRequiresOneBoundedNonemptyValidation(t *testing.T) {
	for _, data := range []struct {
		output string
		exit   int
	}{
		{"", 0}, {"✓ eval.yaml is valid (loaded 0 case(s))", 0}, {"✓ eval.yaml is valid (loaded 1001 case(s))", 0}, {"✓ eval.yaml is valid (loaded 2 case(s))", 1}, {"✓ eval.yaml is valid (loaded 1 case(s))\n✓ eval.yaml is valid (loaded 2 case(s))", 0}, {strings.Repeat("x", (128<<10)+1), 0},
	} {
		if result, err := ParseValidationOutput([]byte(data.output), data.exit); err == nil || result.Status != "failed" {
			t.Fatalf("%+v %v", result, err)
		}
	}
	if result, err := ParseValidationOutput([]byte("✓ eval.yaml is valid (loaded 2 case(s))\n"), 0); err != nil || result.Status != "success" || result.Cases != 2 {
		t.Fatalf("%+v %v", result, err)
	}
}
