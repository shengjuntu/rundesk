package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type runtimeImageDocker struct {
	*fakeDocker
	failProbe  bool
	baseImages map[string]map[string]string
	probeGate  <-chan struct{}
}

func (d *runtimeImageDocker) Run(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) >= 3 && args[0] == "image" && args[1] == "inspect" {
		d.mu.Lock()
		labels, ok := d.baseImages[args[2]]
		if ok {
			d.calls = append(d.calls, append([]string{}, args...))
			body, err := json.Marshal([]map[string]any{{"Id": args[2], "Os": "linux", "Architecture": runtime.GOARCH, "Config": map[string]any{"Labels": labels}}})
			d.mu.Unlock()
			return body, err
		}
		d.mu.Unlock()
	}
	return d.fakeDocker.Run(ctx, args...)
}

func (d *runtimeImageDocker) Command(args ...string) *exec.Cmd {
	if args[0] == "run" && d.probeGate != nil {
		<-d.probeGate
	}
	d.mu.Lock()
	d.calls = append(d.calls, append([]string{}, args...))
	if args[0] == "pull" {
		d.missingImage = ""
	}
	imageID := "sha256:" + strings.Repeat("a", 64)
	if args[0] == "build" {
		for i, arg := range args {
			if arg == "--label" && i+1 < len(args) && strings.HasPrefix(args[i+1], "io.rundesk.base.key=") {
				key := strings.TrimPrefix(args[i+1], "io.rundesk.base.key=")
				sum := sha256.Sum256([]byte(key))
				imageID = fmt.Sprintf("sha256:%x", sum)
				if d.baseImages == nil {
					d.baseImages = map[string]map[string]string{}
				}
				d.baseImages[imageID] = map[string]string{"io.rundesk.base.key": key}
			}
		}
	}
	d.mu.Unlock()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, append([]string{"-test.run=^TestRuntimeImageProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("RUNDESK_RUNTIME_IMAGE_HELPER=%t", d.failProbe), "RUNDESK_RUNTIME_IMAGE_ID="+imageID)
	return cmd
}

func TestRuntimeImageProcess(t *testing.T) {
	mode := os.Getenv("RUNDESK_RUNTIME_IMAGE_HELPER")
	if mode == "" {
		return
	}
	args := os.Args
	for n, arg := range args {
		if arg == "--iidfile" {
			if err := os.WriteFile(args[n+1], []byte(os.Getenv("RUNDESK_RUNTIME_IMAGE_ID")), 0600); err != nil {
				os.Exit(3)
			}
		}
		if arg == "run" && mode == "true" {
			fmt.Println("fixture failed executable check")
			os.Exit(2)
		}
	}
	fmt.Println("fixture process completed; this is not a real Docker acceptance test")
	os.Exit(0)
}

func runtimeImageFixture(t *testing.T) (*Manager, *runtimeImageDocker) {
	m := testManager(t)
	fake := newFakeDocker()
	fake.image = "sha256:" + strings.Repeat("a", 64)
	fake.imageLabels = map[string]string{"io.rundesk.profile": "news2douyin", "io.rundesk.codex.version": "0.144.6", "org.opencontainers.image.version": "1.1.2", "io.rundesk.agent-reach": "true"}
	d := &runtimeImageDocker{fakeDocker: fake}
	m.docker = d
	return m, d
}

func TestRuntimeImageEmbeddedContext(t *testing.T) {
	for _, profile := range []string{"news2douyin", "people-search", "video-build"} {
		data, err := runtimeImageContext(profile, "0.144.6")
		if err != nil {
			t.Fatal(err)
		}
		again, _ := runtimeImageContext(profile, "0.144.6")
		if !bytes.Equal(data, again) {
			t.Fatal("nondeterministic context")
		}
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		videoFiles := map[string]bool{}
		for _, f := range z.File {
			videoFiles[f.Name] = true
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			if profile == "video-build" && f.Name == "profile.lock.json" {
				var lock struct {
					Profile struct {
						Version string `json:"version"`
					} `json:"profile"`
					Selection struct {
						VideoUse bool `json:"videoUse"`
					} `json:"selection"`
					Dependencies struct {
						Hyperframes struct {
							NPMLockSHA256 string `json:"npmLockSHA256"`
						} `json:"hyperframes"`
					} `json:"dependencies"`
				}
				if json.Unmarshal(b, &lock) != nil || lock.Profile.Version != "1.4.0" || lock.Selection.VideoUse || lock.Dependencies.Hyperframes.NPMLockSHA256 != runtimeHyperframesLockSHA256 {
					t.Fatal("embedded default and image-label contract differ", string(b))
				}
			}
			if f.Name == "Dockerfile" {
				if bytes.Contains(b, []byte("0.0.0")) || bytes.Contains(b, []byte("wigolo")) || (profile != "video-build" && bytes.Contains(b, []byte("chromium"))) || !bytes.Contains(b, []byte("ARG CODEX_VERSION=0.144.6")) {
					t.Fatal(string(b))
				}
			}
		}
		if profile == "video-build" {
			for _, name := range []string{"hyperframes/package-lock.json", "hyperframes/LICENSES/Apache-2.0.txt", "skills/build-video-artifacts/references/hyperframes-authoring.md", "skills/build-video-artifacts/references/backend-compatibility.md"} {
				if !videoFiles[name] {
					t.Fatal("missing authoring runtime resource", name)
				}
			}
			if videoFiles["vendor/video-use.tar.gz"] {
				t.Fatal("default unexpectedly installs legacy editor")
			}
		}
	}
	for _, version := range []string{"latest", "0.0.0", "1.0.0\nRUN bad"} {
		if _, err := runtimeImageContext("news2douyin", version); err == nil {
			t.Fatal(version)
		}
	}
	if _, err := runtimeImageContext("../news2douyin", "1.2.3"); err == nil {
		t.Fatal("path accepted")
	}
}

func TestRuntimeImageQueueBeforeApplicationRegistration(t *testing.T) {
	for _, source := range []string{"build", "pull", "local"} {
		t.Run(source, func(t *testing.T) {
			m, d := runtimeImageFixture(t)
			// Keep the first preparation active until its duplicate is submitted.
			// Once the first job finishes, a second preparation is a valid new job.
			probeGate := make(chan struct{})
			d.probeGate = probeGate
			releaseProbe := sync.OnceFunc(func() { close(probeGate) })
			defer releaseProbe()
			if source == "pull" {
				d.missingImage = "example:ready"
			}
			before, _ := m.Instance(DefaultInstance)
			input := RuntimeImageInput{ProfileID: "news2douyin", CodexVersion: "0.144.6", Source: source, Reference: "example:ready"}
			v, err := m.PrepareRuntimeImage(input)
			if err != nil || v.Scope != "setup" || v.Status != "queued" {
				t.Fatal(v, err)
			}
			// Concurrent duplicate cannot enqueue another installation.
			again, err := m.PrepareRuntimeImage(input)
			if err != nil || again.ID != v.ID {
				t.Fatal(again, err)
			}
			releaseProbe()
			v = waitBuild(t, m, DefaultInstance, v.ID, "succeeded", "failed")
			if v.Status != "succeeded" || !v.OfflineReady || v.ImageVersionID != "" {
				t.Fatal(v)
			}
			after, _ := m.Instance(DefaultInstance)
			if before.Revision != after.Revision || after.Execution.normalized().Mode != "local" {
				t.Fatal("modified assistant")
			}
			d.mu.Lock()
			calls := append([][]string{}, d.calls...)
			d.mu.Unlock()
			probe, pulls := false, 0
			for _, args := range calls {
				if args[0] == "pull" {
					pulls++
				}
				if args[0] == "run" {
					probe = true
					s := strings.Join(args, " ")
					if !strings.Contains(s, "--network none --read-only") || !strings.Contains(s, "--user 1000:1000") || strings.Contains(s, "--privileged") {
						t.Fatal(args)
					}
				}
			}
			if !probe || (source == "pull" && pulls != 1) {
				t.Fatal(calls)
			}
			if source == "build" {
				next, err := m.PrepareRuntimeImage(input)
				if err != nil {
					t.Fatal(err)
				}
				waitBuild(t, m, DefaultInstance, next.ID, "succeeded")
				d.mu.Lock()
				builds, bases := 0, 0
				for _, args := range d.calls {
					if args[0] == "build" {
						if strings.Contains(strings.Join(args, " "), "rundesk-base/") {
							bases++
						} else {
							builds++
						}
					}
				}
				d.mu.Unlock()
				if builds != 2 || bases != 2 {
					t.Fatal("expected two preset submissions to reuse system/media bases", builds, bases)
				}
			}
		})
	}
}

func TestRuntimeImageWrongProfileAndProbeFailure(t *testing.T) {
	for _, kind := range []string{"profile", "old-recipe", "probe", "missing"} {
		t.Run(kind, func(t *testing.T) {
			m, d := runtimeImageFixture(t)
			if kind == "profile" {
				d.imageLabels["io.rundesk.profile"] = "video-build"
			}
			if kind == "old-recipe" {
				d.imageLabels["org.opencontainers.image.version"] = "1.1.0"
			}
			if kind == "probe" {
				d.failProbe = true
			}
			if kind == "missing" {
				d.missingImage = "example:ready"
			}
			v, err := m.PrepareRuntimeImage(RuntimeImageInput{ProfileID: "news2douyin", CodexVersion: "0.144.6", Source: "local", Reference: "example:ready"})
			if err != nil {
				t.Fatal(err)
			}
			v = waitBuild(t, m, DefaultInstance, v.ID, "failed")
			canceled, _ := m.CancelBuild(DefaultInstance, v.ID)
			if canceled.Status != "failed" || canceled.OfflineReady {
				t.Fatal("cancel changed terminal result", canceled)
			}
			if v.OfflineReady || v.Error == "" {
				t.Fatal(v)
			}
		})
	}
}

func TestRuntimeImageOfflineProbeTemporaryStorage(t *testing.T) {
	for _, profile := range []string{"video-build", "news2douyin", "people-search", "hyperframes", "video-use", "openmontage"} {
		t.Run(profile, func(t *testing.T) {
			m, d := runtimeImageFixture(t)
			d.imageLabels = map[string]string{
				"io.rundesk.profile": profile, "io.rundesk.codex.version": "0.144.6",
				"org.opencontainers.image.version": runtimeProfileVersion(profile), "io.rundesk.agent-reach": "true",
			}
			wantMemory, wantPids := "512m", "128"
			wantTmpfs := "/tmp:rw,nosuid,nodev,size=128m,mode=1777"
			if profile == "video-build" {
				d.imageLabels["io.rundesk.agent-reach"] = "false"
				d.imageLabels["io.rundesk.hyperframes.version"] = "0.8.87"
				d.imageLabels["io.rundesk.gsap.version"] = "3.15.0"
				d.imageLabels["io.rundesk.hyperframes.lock"] = runtimeHyperframesLockSHA256
				d.imageLabels["io.rundesk.video-use.enabled"] = "false"
				wantMemory, wantPids = "2048m", "256"
				wantTmpfs = "/tmp:rw,nosuid,nodev,size=2g,mode=1777"
			} else if independentVideoProfile(profile) {
				identity, ok := independentRuntimeIdentity(profile)
				if !ok {
					t.Fatal("missing reviewed upstream identity", profile)
				}
				d.imageLabels["io.rundesk.agent-reach"] = "false"
				d.imageLabels["io.rundesk.upstream.repository"] = identity.Repository
				d.imageLabels["io.rundesk.upstream.commit"] = identity.Commit
				d.imageLabels["io.rundesk.upstream.source.sha256"] = identity.SourceSHA256
				d.imageLabels["io.rundesk.video-app-media.contract"] = "1"
				wantMemory, wantPids = "2048m", "256"
				wantTmpfs = "/tmp:rw,nosuid,nodev,size=768m,mode=1777"
			}
			build := ImageBuild{ID: "render-probe", ProfileID: profile, CodexVersion: "0.144.6"}
			var log bytes.Buffer
			if err := m.verifyRuntimeImage(context.Background(), build, d.image, &log); err != nil {
				t.Fatal(err, log.String())
			}
			d.mu.Lock()
			calls := append([][]string{}, d.calls...)
			d.mu.Unlock()
			probes := 0
			for _, args := range calls {
				if len(args) == 0 || args[0] != "run" {
					continue
				}
				probes++
				flag := func(name string) string {
					for index, value := range args {
						if value == name && index+1 < len(args) {
							return args[index+1]
						}
					}
					return ""
				}
				if flag("--tmpfs") != wantTmpfs || flag("--memory") != wantMemory || flag("--pids-limit") != wantPids {
					t.Fatal("incorrect profile-specific probe resources", args)
				}
				joined := strings.Join(args, " ")
				for _, restriction := range []string{"--rm", "--pull=never", "--network none", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges:true", "--user 1000:1000", "--env HOME=/tmp", "--env CODEX_HOME=/tmp/codex", "/opt/rundesk-profile/tools/profile_doctor.py --json"} {
					if !strings.Contains(joined, restriction) {
						t.Fatal("probe lost isolation or skipped the doctor", restriction, args)
					}
				}
				if strings.Contains(joined, "--mount") || strings.Contains(joined, "--privileged") || strings.Contains(joined, "docker.sock") {
					t.Fatal("probe acquired host access", args)
				}
			}
			if probes != 1 {
				t.Fatal("expected one offline doctor invocation", probes, calls)
			}
		})
	}
}

func TestRuntimeImageVideoBuildDoctorFailureDoesNotBecomeReady(t *testing.T) {
	m, d := runtimeImageFixture(t)
	d.imageLabels = map[string]string{
		"io.rundesk.profile": "video-build", "io.rundesk.codex.version": "0.144.6",
		"org.opencontainers.image.version": "1.4.0", "io.rundesk.agent-reach": "false",
		"io.rundesk.hyperframes.version": "0.8.87", "io.rundesk.gsap.version": "3.15.0",
		"io.rundesk.hyperframes.lock": runtimeHyperframesLockSHA256, "io.rundesk.video-use.enabled": "false",
	}
	d.failProbe = true
	v, err := m.PrepareRuntimeImage(RuntimeImageInput{ProfileID: "video-build", CodexVersion: "0.144.6", Source: "local", Reference: "example:ready"})
	if err != nil {
		t.Fatal(err)
	}
	v = waitBuild(t, m, DefaultInstance, v.ID, "failed")
	if v.OfflineReady || v.Error == "" || v.ImageID != d.image {
		t.Fatalf("a failed doctor was bypassed or lost the existing image: %+v", v)
	}
}

func TestRuntimeImageHTTPIdempotency(t *testing.T) {
	m, _ := runtimeImageFixture(t)
	h := NewHandler(m, "", true)
	body := `{"profileId":"news2douyin","codexVersion":"0.144.6","source":"local","reference":"example:ready"}`
	if r := v1Request(h, "POST", "/setup/runtime-images", body, ""); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	one := v1Request(h, "POST", "/setup/runtime-images", body, "runtime-image-test-key")
	two := v1Request(h, "POST", "/setup/runtime-images", body, "runtime-image-test-key")
	if one.Code != 200 || two.Code != 200 {
		t.Fatal(one.Body.String(), two.Body.String())
	}
	var a, b ImageBuild
	json.Unmarshal(one.Body.Bytes(), &a)
	json.Unmarshal(two.Body.Bytes(), &b)
	if a.ID == "" || a.ID != b.ID {
		t.Fatal(a, b)
	}
	conn, err := m.ConnectApplication("runtime-auth-test", ApplicationConnect{Name: "News", InstallationID: "runtime-auth-test"})
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := m.CreateApplicationKey(conn.Application.AppID, KeyInput{Name: "worker", WorkspaceIDs: []string{conn.Application.WorkspaceID}, Scopes: []string{"read", "run"}})
	if err != nil {
		t.Fatal(err)
	}
	r := appRequest(NewHandler(m, "admin-secret", true), "POST", "/api/v1/setup/runtime-images", body, key, "runtime-unauthorized-key")
	if r.Code != 403 {
		t.Fatal("application could prepare global image", r.Code, r.Body.String())
	}
}

func TestSetupApplicationsOnlyWithoutHostCodex(t *testing.T) {
	m := testManager(t)
	m.Demo = false
	m.Codex = "missing-host-codex"
	i, _ := m.Instance(DefaultInstance)
	v, err := m.saveSetup(SetupInput{WorkspaceID: m.Workspaces()[0].ID, ApplicationsOnly: true, Completed: true, InstanceRevision: &i.Revision})
	if err != nil || !v.Completed || !v.ApplicationsOnly {
		t.Fatal(v, err)
	}
	after, _ := m.Instance(DefaultInstance)
	if after.AgentRuntime.Kind != i.AgentRuntime.Kind {
		t.Fatal("changed assistant backend")
	}
	e, err := m.setupEnvironment(DefaultInstance, m.Workspaces()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range e.Checks {
		if check.ID == "codex" && check.Required {
			t.Fatal("host Codex required", check)
		}
	}
}
