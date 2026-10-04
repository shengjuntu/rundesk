package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for n, s := range files {
		w, e := z.Create(n)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = w.Write([]byte(s))
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestBuildArchiveValidation(t *testing.T) {
	m, _, i, _ := dockerTestApp(t)
	for _, files := range []map[string]string{{"../outside": "bad", "Dockerfile": "FROM scratch"}, {"/absolute": "bad", "Dockerfile": "FROM scratch"}, {"C:\\bad": "bad", "Dockerfile": "FROM scratch"}, {"Dockerfile": "x", "dockerfile": "y"}, {"readme": "missing Dockerfile"}} {
		if _, e := m.CreateBuild(i.ID, "test", "Dockerfile", 1, false, bytes.NewReader(buildZip(t, files))); e == nil {
			t.Fatal("unsafe archive accepted", files)
		}
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "Dockerfile"}
	h.SetMode(os.ModeSymlink | 0777)
	w, _ := z.CreateHeader(h)
	w.Write([]byte("/etc/passwd"))
	z.Close()
	if _, e := m.CreateBuild(i.ID, "test", "Dockerfile", 1, false, &b); e == nil {
		t.Fatal("symlink accepted")
	}
	v, e := m.CreateBuild(i.ID, "valid", "docker/Dockerfile", 1, false, bytes.NewReader(buildZip(t, map[string]string{"docker/Dockerfile": "FROM scratch", "skills/news/SKILL.md": "test", ".dockerignore": ".env"})))
	if e != nil || v.Files != 3 || v.Status != "draft" || len(v.ContextSHA256) != 64 {
		t.Fatal(v, e)
	}
	if _, e = os.Stat(filepath.Join(m.buildDir(v.ID), "context", "skills/news/SKILL.md")); e != nil {
		t.Fatal(e)
	}
	if e = m.DeleteBuild(i.ID, v.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(m.buildDir(v.ID)); !os.IsNotExist(e) {
		t.Fatal("context not cleaned")
	}
}

type buildTestDocker struct {
	*fakeDocker
	lock   sync.Mutex
	starts int
	mode   string
}

func (d *buildTestDocker) Command(args ...string) *exec.Cmd {
	d.lock.Lock()
	d.starts++
	d.lock.Unlock()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, append([]string{"-test.run=^TestBuildProcessHelper$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "RUNDESK_BUILD_HELPER="+d.mode)
	return cmd
}
func TestBuildProcessHelper(t *testing.T) {
	mode := os.Getenv("RUNDESK_BUILD_HELPER")
	if mode == "" {
		return
	}
	args := os.Args
	var iid string
	for j, a := range args {
		if a == "--iidfile" {
			iid = args[j+1]
		}
	}
	fmt.Println("fixture build started")
	if mode == "wait" {
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "fixture dependency failed")
		os.Exit(2)
	}
	if mode == "logs" {
		fmt.Print(strings.Repeat("x", buildLogLimit+1000))
	}
	if iid == "" {
		os.Exit(3)
	}
	if e := os.WriteFile(iid, []byte("sha256:"+strings.Repeat("a", 64)), 0600); e != nil {
		os.Exit(4)
	}
	fmt.Println("fixture build complete")
	os.Exit(0)
}
func waitBuild(t *testing.T, m *Manager, iid, id string, want ...string) ImageBuild {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		v, e := m.Build(iid, id)
		if e != nil {
			t.Fatal(e)
		}
		for _, s := range want {
			if v.Status == s {
				return v
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	v, _ := m.Build(iid, id)
	t.Fatalf("build timed out: %+v", v)
	return v
}
func TestBuildExecutionCancellationAndLogs(t *testing.T) {
	for _, mode := range []string{"success", "fail", "wait", "logs"} {
		t.Run(mode, func(t *testing.T) {
			m, f, i, _ := dockerTestApp(t)
			d := &buildTestDocker{fakeDocker: f, mode: mode}
			m.docker = d
			v, e := m.CreateBuild(i.ID, mode, "Dockerfile", 1, false, bytes.NewReader(buildZip(t, map[string]string{"Dockerfile": "FROM scratch"})))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = m.StartBuild(i.ID, v.ID); e != nil {
				t.Fatal(e)
			}
			if _, e = m.StartBuild(i.ID, v.ID); e != nil {
				t.Fatal(e)
			}
			if mode == "wait" {
				waitBuild(t, m, i.ID, v.ID, "running")
				if e = m.DeleteBuild(i.ID, v.ID); e == nil {
					t.Fatal("deleted active build")
				}
				_, e = m.CancelBuild(i.ID, v.ID)
				if e != nil {
					t.Fatal(e)
				}
				v = waitBuild(t, m, i.ID, v.ID, "canceled")
			} else {
				v = waitBuild(t, m, i.ID, v.ID, "succeeded", "failed")
				if mode == "fail" && v.Status != "failed" || mode != "fail" && v.Status != "succeeded" {
					t.Fatal(v)
				}
			}
			d.lock.Lock()
			starts := d.starts
			d.lock.Unlock()
			if starts > 1 {
				t.Fatal("duplicate start", starts)
			}
			current, _ := m.Instance(i.ID)
			if current.Execution.Image != i.Execution.Image || current.Revision != i.Revision {
				t.Fatal("build changed application")
			}
			if v.Status == "succeeded" {
				catalog, e := m.Images(i.ID)
				if e != nil || len(catalog.Versions) != 1 || catalog.Versions[0].ImageID != v.ImageID || v.ImageVersionID == "" {
					t.Fatal(catalog, e)
				}
			}
			if mode == "logs" {
				info, e := os.Stat(filepath.Join(m.buildDir(v.ID), "build.log"))
				if e != nil || info.Size() != buildLogLimit || !v.LogTruncated {
					t.Fatal("unbounded log", info, e, v)
				}
			}
			if _, e = os.Stat(filepath.Join(m.buildDir(v.ID), "context")); !os.IsNotExist(e) {
				t.Fatal("completed context retained")
			}
			if _, e = m.BuildLog(i.ID, v.ID, -1); e == nil {
				t.Fatal("negative log offset accepted")
			}
		})
	}
}
func TestBuildRestartAndDefaultDeny(t *testing.T) {
	m, u, code, _, _, shared, _ := userFixture(t)
	iid := shared.InstanceID
	v, e := m.CreateBuild(iid, "restart", "Dockerfile", 1, false, bytes.NewReader(buildZip(t, map[string]string{"Dockerfile": "FROM scratch"})))
	if e != nil {
		t.Fatal(e)
	}
	h := NewHandler(m, userTestAdmin, true)
	key, token, e := m.CreateApplicationKey("news-members", KeyInput{Name: "build-deny", WorkspaceIDs: []string{shared.WorkspaceID}, Scopes: []string{"read", "run", "files"}})
	_ = key
	if e != nil {
		t.Fatal(e)
	}
	for _, bearer := range []string{code, token} {
		for _, p := range []string{"/api/v1/instances/" + iid + "/builds", "/api/v1/instances/" + iid + "/builds/" + v.ID + "/log", "/api/v1/member/" + u.Grants[0].ID + "/instances/" + iid + "/builds"} {
			r := appRequest(h, "GET", p, "", bearer, "")
			if r.Code != 403 {
				t.Fatal("build access leaked", p, r.Code)
			}
		}
		r := appRequest(h, "POST", "/api/v1/instances/"+iid+"/builds/"+v.ID+"/start", "", bearer, "")
		if r.Code != 403 {
			t.Fatal("build execution permitted", r.Code)
		}
	}
	m.buildMu.Lock()
	v.Status = "running"
	e = m.Store.Put("image-build", v.ID, v)
	m.buildMu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	// Exercise the startup recovery routine with no queued jobs and no live process.
	m.buildMu.Lock()
	e = m.initBuilds()
	m.buildMu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	v, _ = m.Build(iid, v.ID)
	if v.Status != "interrupted" {
		t.Fatal(v)
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), "不会自动重跑") {
		t.Fatal("uncertainty not explained")
	}
}

func TestBuildQueueSerialAndQueuedCancellation(t *testing.T) {
	m, f, i, _ := dockerTestApp(t)
	d := &buildTestDocker{fakeDocker: f, mode: "wait"}
	m.docker = d
	create := func() ImageBuild {
		v, e := m.CreateBuild(i.ID, "queued", "Dockerfile", 1, false, bytes.NewReader(buildZip(t, map[string]string{"Dockerfile": "FROM scratch"})))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = m.StartBuild(i.ID, v.ID); e != nil {
			t.Fatal(e)
		}
		return v
	}
	first := create()
	waitBuild(t, m, i.ID, first.ID, "running")
	second := create()
	third := create()
	if _, e := m.CancelBuild(i.ID, third.ID); e != nil {
		t.Fatal(e)
	}
	waitBuild(t, m, i.ID, third.ID, "canceled")
	second, _ = m.Build(i.ID, second.ID)
	if second.Status != "queued" {
		t.Fatal("not serial", second)
	}
	if _, e := m.CancelBuild(i.ID, first.ID); e != nil {
		t.Fatal(e)
	}
	waitBuild(t, m, i.ID, first.ID, "canceled")
	waitBuild(t, m, i.ID, second.ID, "running")
	if _, e := m.CancelBuild(i.ID, second.ID); e != nil {
		t.Fatal(e)
	}
	waitBuild(t, m, i.ID, second.ID, "canceled")
}
