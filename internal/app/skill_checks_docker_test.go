package app

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func skillCheckTarFixture(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, h := range headers {
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			if _, err := io.CopyN(w, strings.NewReader(strings.Repeat("x", int(h.Size))), h.Size); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSkillCheckReportTarRequiresOneRegularBoundedFile(t *testing.T) {
	valid := &tar.Header{Name: "result.json", Typeflag: tar.TypeReg, Mode: 0600, Size: 2}
	data, err := readSkillCheckTar(skillCheckTarFixture(t, valid))
	if err != nil || string(data) != "xx" {
		t.Fatal(string(data), err)
	}
	for _, headers := range [][]*tar.Header{
		{{Name: "result.json", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		{{Name: "result.json", Typeflag: tar.TypeLink, Linkname: "elsewhere"}},
		{{Name: "../result.json", Typeflag: tar.TypeReg, Size: 1}},
		{{Name: "/result.json", Typeflag: tar.TypeReg, Size: 1}},
		{{Name: "result.json", Typeflag: tar.TypeDir}},
		{{Name: "result.json", Typeflag: tar.TypeReg, Size: 0}},
		{valid, valid},
		{{Name: "result.json", Typeflag: tar.TypeReg, Size: skillCheckReportLimit + 1}},
	} {
		if _, err := readSkillCheckTar(skillCheckTarFixture(t, headers...)); err == nil {
			t.Fatalf("accepted unsafe report headers: %+v", headers)
		}
	}
	if _, err := readSkillCheckTar([]byte("broken")); err == nil {
		t.Fatal("accepted truncated archive")
	}
	if _, err := readSkillCheckTar(append(skillCheckTarFixture(t, valid), []byte("trailing data")...)); err == nil {
		t.Fatal("accepted nonzero data after the tar terminator")
	}
}

func skillCheckDockerJob() SkillCheckJob {
	id := strings.Repeat("a", 24)
	return SkillCheckJob{ID: id, PackID: strings.Repeat("b", 24), Digest: "sha256:" + strings.Repeat("c", 64), Action: "run", InstanceID: "fixture", ImageID: "sha256:" + strings.Repeat("d", 64), ContainerName: "rundesk-skillcheck-" + id}
}

func TestSkillCheckContainerArgsKeepExecutionIsolated(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only Docker sandbox")
	}
	t.Setenv("RUNDESK_SKILLUP_CONTAINER_BIN", "")
	source := t.TempDir()
	job := skillCheckDockerJob()
	args, err := skillCheckContainerArgs(job, source)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\x00")
	for _, required := range []string{
		"--pull\x00never", "--read-only", "--log-driver\x00none", "--cap-drop\x00ALL", "--security-opt\x00no-new-privileges:true",
		"--user\x0065532:65532", "--network\x00none", "--memory\x002048m", "--memory-swap\x002048m", "--pids-limit\x00256",
		"--cpus\x002", "--tmpfs\x00/work:", "--tmpfs\x00/tmp:", "--tmpfs\x00/output:",
		"type=bind,src=" + source + ",dst=/source,readonly", "--entrypoint\x00/usr/local/bin/skill-up\x00" + job.ImageID,
		"run\x00/source/evals/eval.yaml\x00--output-dir\x00/output\x00--iteration\x001\x00--parallelism\x001",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing %q in args %v", required, args)
		}
	}
	if strings.Count(joined, "--mount") != 1 || strings.Contains(joined, "--privileged") || strings.Contains(joined, "docker.sock") || strings.Contains(joined, "CODEX_HOME") || strings.Contains(joined, "--env-file") || strings.Contains(joined, "--rm") {
		t.Fatal("sandbox inherited host configuration", args)
	}
	job.AllowNetwork = true
	args, err = skillCheckContainerArgs(job, source)
	if err != nil || !strings.Contains(strings.Join(args, "\x00"), "--network\x00bridge") {
		t.Fatal("explicit network request not reflected", args, err)
	}
	job.Action = "validate"
	args, err = skillCheckContainerArgs(job, source)
	if err != nil || strings.Contains(strings.Join(args, "\x00"), "--output-dir") {
		t.Fatal("validate changed into a run", args, err)
	}
	job.ImageID = "moving-tag:latest"
	if _, err = skillCheckContainerArgs(job, source); err == nil {
		t.Fatal("accepted mutable image identity")
	}
	job = skillCheckDockerJob()
	for _, path := range []string{source + ",rw", "relative/path", source + ":extra"} {
		if _, err = skillCheckContainerArgs(job, path); err == nil {
			t.Fatal("accepted unsafe mount path", path)
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if _, err = skillCheckContainerArgs(job, link); err == nil {
		t.Fatal("accepted symlink source")
	}
}

type skillCheckDockerFixture struct {
	mu          sync.Mutex
	calls       [][]string
	archive     string
	containerID string
	owner       string
	createErr   error
	startCode   int
	block       chan struct{}
	started     chan struct{}
	removed     bool
	cleanupLive bool
	cleanupErr  error
}

func (f *skillCheckDockerFixture) Run(ctx context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), args...))
	f.mu.Unlock()
	switch args[0] {
	case "create":
		if f.createErr != nil {
			return nil, f.createErr
		}
		return []byte(f.containerID), nil
	case "start":
		if f.started != nil {
			close(f.started)
		}
		if f.block != nil {
			select {
			case <-f.block:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if f.startCode != 0 {
			return []byte("ordinary test failure"), errors.New("container returned nonzero")
		}
		return []byte("framework output"), nil
	case "inspect":
		if args[1] == "--type" {
			return json.Marshal(map[string]any{"Id": f.containerID, "Config": map[string]any{"Labels": map[string]string{"io.rundesk.skillcheck": f.owner}}})
		}
		return json.Marshal(map[string]any{"Status": "exited", "Running": false, "ExitCode": f.startCode, "Error": "", "OOMKilled": false})
	case "rm":
		f.mu.Lock()
		f.removed, f.cleanupLive = f.cleanupErr == nil, ctx.Err() == nil
		f.mu.Unlock()
		return nil, f.cleanupErr
	}
	return nil, errors.New("unexpected Docker command")
}

func (f *skillCheckDockerFixture) Command(args ...string) *exec.Cmd {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), args...))
	f.mu.Unlock()
	executable, _ := os.Executable()
	command := exec.Command(executable, "-test.run=^TestSkillCheckReportCopyHelper$", "--")
	command.Env = []string{"RUNDESK_SKILLCHECK_COPY_FIXTURE=" + f.archive}
	return command
}

func TestSkillCheckReportCopyHelper(t *testing.T) {
	name := os.Getenv("RUNDESK_SKILLCHECK_COPY_FIXTURE")
	if name == "" {
		return
	}
	data, err := os.ReadFile(name)
	if err != nil {
		os.Exit(4)
	}
	_, _ = os.Stdout.Write(data)
	os.Exit(0)
}

func TestSkillCheckDockerExecutionKeepsCLIExitAndReportSeparate(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only Docker sandbox")
	}
	t.Setenv("RUNDESK_DOCKER_HOST", "")
	for _, exit := range []int{0, 1} {
		t.Run(string(rune('0'+exit)), func(t *testing.T) {
			m := testManager(t)
			archive := filepath.Join(t.TempDir(), "report.tar")
			if err := os.WriteFile(archive, skillCheckTarFixture(t, &tar.Header{Name: "result.json", Typeflag: tar.TypeReg, Size: 2}), 0600); err != nil {
				t.Fatal(err)
			}
			job := skillCheckDockerJob()
			fixture := &skillCheckDockerFixture{containerID: strings.Repeat("e", 64), archive: archive, startCode: exit, owner: job.ID}
			m.docker = fixture
			result, err := m.executeSkillCheck(context.Background(), job, t.TempDir())
			if err != nil || result.ExitCode != exit || string(result.Report) != "xx" || !fixture.removed {
				t.Fatalf("result=%+v err=%v removed=%v", result, err, fixture.removed)
			}
			for _, args := range fixture.calls {
				if args[0] == "cp" && (args[1] != fixture.containerID+":/output/iteration-1/result.json" || args[2] != "-") {
					t.Fatal("report copy did not remain an in-memory stream", args)
				}
			}
		})
	}
}

func TestSkillCheckDockerCancellationRemovesOnlyOwnedContainer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only Docker sandbox")
	}
	t.Setenv("RUNDESK_DOCKER_HOST", "")
	m := testManager(t)
	job := skillCheckDockerJob()
	fixture := &skillCheckDockerFixture{containerID: strings.Repeat("e", 64), started: make(chan struct{}), block: make(chan struct{}), owner: job.ID}
	m.docker = fixture
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	source := t.TempDir()
	go func() { _, err := m.executeSkillCheck(ctx, job, source); done <- err }()
	select {
	case <-fixture.started:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !fixture.removed || !fixture.cleanupLive {
			t.Fatalf("cancellation=%v removed=%v cleanupLive=%v", err, fixture.removed, fixture.cleanupLive)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not finish cleanup")
	}
	for _, owner := range []string{job.ID, "another-job"} {
		fixture = &skillCheckDockerFixture{containerID: strings.Repeat("f", 64), createErr: errors.New("uncertain create"), owner: owner}
		m.docker = fixture
		_, err := m.executeSkillCheck(context.Background(), job, source)
		if err == nil || fixture.removed != (owner == job.ID) {
			t.Fatalf("create cleanup touched unrelated container: owner=%s removed=%v err=%v", owner, fixture.removed, err)
		}
	}
}

func TestSkillCheckDockerCleanupFailureCannotBecomeSuccessfulExecution(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only Docker sandbox")
	}
	t.Setenv("RUNDESK_DOCKER_HOST", "")
	m := testManager(t)
	job := skillCheckDockerJob()
	job.Action = "validate"
	fixture := &skillCheckDockerFixture{containerID: strings.Repeat("e", 64), owner: job.ID, cleanupErr: errors.New("daemon stopped before cleanup confirmation")}
	m.docker = fixture
	result, err := m.executeSkillCheck(context.Background(), job, t.TempDir())
	if err == nil || result.CleanupError == "" || result.ExitCode != 0 || fixture.removed {
		t.Fatalf("cleanup error was hidden: %+v error=%v removed=%v", result, err, fixture.removed)
	}
}
