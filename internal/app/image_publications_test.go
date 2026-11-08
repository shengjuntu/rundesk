package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shengjuntu/rundesk/internal/store"
)

type publicationDocker struct {
	*fakeDocker
	mode, target string
}

func (d *publicationDocker) Run(ctx context.Context, args ...string) ([]byte, error) {
	raw, err := d.fakeDocker.Run(ctx, args...)
	if err != nil {
		return raw, err
	}
	if len(args) >= 3 && args[0] == "image" && args[1] == "inspect" {
		d.mu.Lock()
		target := d.target
		d.mu.Unlock()
		if target != "" {
			var images []map[string]any
			_ = json.Unmarshal(raw, &images)
			repo := publicationRepository(target)
			if strings.HasPrefix(repo, "docker.io/") {
				repo = strings.TrimPrefix(repo, "docker.io/")
			}
			images[0]["RepoDigests"] = []string{repo + "@sha256:" + strings.Repeat("b", 64)}
			return json.Marshal(images)
		}
	}
	return raw, err
}
func (d *publicationDocker) Command(args ...string) *exec.Cmd {
	d.mu.Lock()
	d.calls = append(d.calls, append([]string{}, args...))
	mode := d.mode
	if len(args) == 3 && args[0] == "tag" {
		d.target = args[2]
	}
	d.mu.Unlock()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, append([]string{"-test.run=^TestImagePublicationProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "RUNDESK_PUBLICATION_FIXTURE="+mode, "RUNDESK_PUBLICATION_ARCH="+runtime.GOARCH)
	return cmd
}
func TestImagePublicationProcess(t *testing.T) {
	mode := os.Getenv("RUNDESK_PUBLICATION_FIXTURE")
	if mode == "" {
		return
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(3)
	}
	switch args[0] {
	case "tag":
		os.Exit(0)
	case "push":
		fmt.Print("Authorization: Bearer secret-part")
		fmt.Println("-tail")
		if mode == "push-fail" {
			os.Exit(2)
		}
		if mode == "slow" {
			time.Sleep(30 * time.Second)
		}
		digest := strings.Repeat("b", 64)
		if mode == "digest-mismatch" {
			digest = strings.Repeat("c", 64)
		}
		fmt.Println("v1: digest: sha256:" + digest + " size: 1024")
		os.Exit(0)
	case "--config":
		if len(args) != 6 || args[2] != "manifest" || args[3] != "inspect" || args[4] != "--verbose" {
			os.Exit(4)
		}
		data, e := os.ReadFile(filepath.Join(args[1], "config.json"))
		if e != nil || string(data) != "{\"auths\":{}}\n" {
			os.Exit(5)
		}
		if mode == "private" {
			fmt.Println("unauthorized")
			os.Exit(2)
		}
		if mode == "slow-anonymous" {
			time.Sleep(30 * time.Second)
		}
		digest := strings.Repeat("b", 64)
		if mode == "anonymous-wrong" {
			digest = strings.Repeat("e", 64)
		}
		if mode == "anonymous-old" {
			fmt.Printf(`{"Digest":"sha256:%s","Platform":{"os":"linux","architecture":%q}}`, digest, os.Getenv("RUNDESK_PUBLICATION_ARCH"))
		} else {
			fmt.Printf(`{"Descriptor":{"digest":"sha256:%s","platform":{"os":"linux","architecture":%q}}}`, digest, os.Getenv("RUNDESK_PUBLICATION_ARCH"))
		}
		os.Exit(0)
	}
	os.Exit(6)
}
func publicationFixture(t *testing.T) (*Manager, *publicationDocker, ImageBuild) {
	t.Helper()
	m := testManager(t)
	fake := newFakeDocker()
	fake.image = "sha256:" + strings.Repeat("a", 64)
	fake.imageLabels = map[string]string{"io.rundesk.profile": "news2douyin", "io.rundesk.codex.version": "0.157.1", "org.opencontainers.image.version": "1.1.2", "io.rundesk.agent-reach": "true"}
	d := &publicationDocker{fakeDocker: fake, mode: "ready"}
	m.docker = d
	b := ImageBuild{ID: store.ID(), InstanceID: DefaultInstance, Scope: "setup", Status: "succeeded", ImageID: fake.image, ProfileID: "news2douyin", CodexVersion: "0.157.1", PlatformOS: "linux", PlatformArch: runtime.GOARCH, ContextSHA256: strings.Repeat("d", 64), RuntimeIdentity: &RuntimeImageIdentity{ProfileVersion: "1.1.2", AgentReach: true}, RecipeSmoke: &RecipeSmokeSpec{Command: []string{"python3", "/opt/rundesk-profile/tools/profile_doctor.py", "--json"}}, Created: store.Now()}
	if e := m.Store.Put("image-build", b.ID, b); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"offline", "smoke"} {
		test := b
		test.ID = store.ID()
		test.Scope = "image-test"
		test.SourceBuildID = b.ID
		test.TestKind = kind
		test.Created = store.Now()
		if e := m.Store.Put("image-build", test.ID, test); e != nil {
			t.Fatal(e)
		}
	}
	return m, d, b
}
func waitPublication(t *testing.T, m *Manager, id string) ImagePublication {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		v, e := m.ImagePublication(id)
		if e != nil {
			t.Fatal(e)
		}
		if !buildActive(v.Status) {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	v, _ := m.ImagePublication(id)
	t.Fatal("publication timeout", v)
	return v
}
func publicationInput(b ImageBuild, target string) ImagePublicationInput {
	return ImagePublicationInput{SourceBuildID: b.ID, Target: target, Confirm: true, RequestID: store.ID()}
}
func TestImagePublicationReadyPushAndPublicAccess(t *testing.T) {
	for _, target := range []string{"ghcr.io/example/news:1.1.2", "docker.io/example/news:1.1.2"} {
		t.Run(target, func(t *testing.T) {
			m, d, b := publicationFixture(t)
			ready, e := m.ImagePublicationReadiness(BuildCenterImageKey(b.ImageID))
			if e != nil || !ready.Eligible {
				t.Fatal(ready, e)
			}
			in := publicationInput(b, target)
			v, e := m.StartImagePublication(in)
			if e != nil {
				t.Fatal(e)
			}
			again, e := m.StartImagePublication(in)
			if e != nil || again.ID != v.ID {
				t.Fatal(again, e)
			}
			changed := in
			changed.Target = "ghcr.io/example/other:1.1.2"
			if _, e = m.StartImagePublication(changed); e == nil {
				t.Fatal("request ID rebound")
			}
			v = waitPublication(t, m, v.ID)
			if v.Status != "succeeded" || v.Digest != "sha256:"+strings.Repeat("b", 64) || v.PublicAccessVerified || v.Reference != publicationRepository(target)+"@"+v.Digest {
				t.Fatal(v)
			}
			fragment, _ := json.Marshal(v.PublicImage)
			if !strings.Contains(string(fragment), v.Reference) {
				t.Fatal(string(fragment))
			}
			log, e := m.ImagePublicationLog(v.ID, 0)
			if e != nil || strings.Contains(fmt.Sprint(log), "secret-part") {
				t.Fatal(log, e)
			}
			for _, mode := range []string{"private", "anonymous-wrong", "ready", "anonymous-old"} {
				d.mu.Lock()
				d.mode = mode
				d.mu.Unlock()
				checked, e := m.CheckImagePublicationPublic(context.Background(), v.ID)
				want := mode == "ready" || mode == "anonymous-old"
				if e != nil || checked.PublicAccessVerified != want || checked.PublicCheckedAt == "" || checked.PublicChecking || checked.Status != "succeeded" {
					t.Fatal(mode, checked, e)
				}
			}
			d.mu.Lock()
			calls := append([][]string{}, d.calls...)
			d.mu.Unlock()
			pushes := 0
			checks := 0
			for _, args := range calls {
				if args[0] == "push" {
					pushes++
					if args[1] != target {
						t.Fatal(args)
					}
				}
				if args[0] == "tag" && args[1] != b.ImageID {
					t.Fatal("tagged moving source", args)
				}
				if args[0] == "--config" {
					checks++
					if _, e = os.Stat(args[1]); !os.IsNotExist(e) {
						t.Fatal("anonymous config retained", args, e)
					}
					if args[len(args)-1] != v.Reference {
						t.Fatal("anonymous check used moving tag", args)
					}
				}
			}
			if pushes != 1 || checks != 4 {
				t.Fatal(pushes, checks)
			}
		})
	}
}
func TestImagePublicationRejectsEvidenceAndTargets(t *testing.T) {
	for _, target := range []string{"docker.io/example/news:1.1.2", "index.docker.io/example/news:1.1.2", "registry-1.docker.io/example/news:1.1.2"} {
		v, e := normalizePublicationTarget(target)
		if e != nil || v != "docker.io/example/news:1.1.2" {
			t.Fatal(v, e)
		}
	}

	for _, target := range []string{"news:1.0", "owner/news:1.0", "https://ghcr.io/owner/news:1.0", "ghcr.io/owner/news:latest", "ghcr.io/owner/news@sha256:" + strings.Repeat("b", 64), "ghcr.io/owner/news:1.0;touch /tmp/x", "ghcr.io/u:pass@host/news:1.0"} {
		if _, e := normalizePublicationTarget(target); e == nil {
			t.Fatal("accepted target", target)
		}
	}
	for _, bad := range []string{"missing-smoke", "wrong-source", "wrong-context", "new-failed", "wrong-image-labels"} {
		t.Run(bad, func(t *testing.T) {
			m, d, b := publicationFixture(t)
			rows, _ := m.buildsLocked("")
			for _, test := range rows {
				if test.Scope != "image-test" || test.TestKind != "smoke" {
					continue
				}
				switch bad {
				case "missing-smoke":
					_ = m.Store.Delete("image-build", test.ID)
				case "wrong-source":
					test.SourceBuildID = store.ID()
					_ = m.Store.Put("image-build", test.ID, test)
				case "wrong-context":
					test.ContextSHA256 = strings.Repeat("e", 64)
					_ = m.Store.Put("image-build", test.ID, test)
				case "new-failed":
					test.ID = store.ID()
					test.Created = time.Now().Add(time.Second).Format(time.RFC3339Nano)
					test.Status = "failed"
					_ = m.Store.Put("image-build", test.ID, test)
				}
			}
			if bad == "wrong-image-labels" {
				d.mu.Lock()
				d.imageLabels["io.rundesk.codex.version"] = "0.1.0"
				d.mu.Unlock()
			}
			ready, e := m.ImagePublicationReadiness(BuildCenterImageKey(b.ImageID))
			if e != nil || ready.Eligible || ready.Reason == "" {
				t.Fatal(ready, e)
			}
			if _, e = m.StartImagePublication(publicationInput(b, "ghcr.io/example/news:1.1.2")); e == nil {
				t.Fatal("unverified publication admitted")
			}
		})
	}
}
func TestImagePublicationFailureCancellationAndRestart(t *testing.T) {
	for _, mode := range []string{"push-fail", "digest-mismatch", "slow"} {
		t.Run(mode, func(t *testing.T) {
			m, d, b := publicationFixture(t)
			d.mu.Lock()
			d.mode = mode
			d.mu.Unlock()
			v, e := m.StartImagePublication(publicationInput(b, "ghcr.io/example/news:1.1.2"))
			if e != nil {
				t.Fatal(e)
			}
			if mode == "slow" {
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					cur, _ := m.ImagePublication(v.ID)
					if cur.Status == "running" {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
				if _, e = m.CancelImagePublication(v.ID); e != nil {
					t.Fatal(e)
				}
			}
			v = waitPublication(t, m, v.ID)
			if mode == "slow" && v.Status != "canceled" || mode != "slow" && v.Status != "failed" || v.PublicAccessVerified || v.Reference != "" {
				t.Fatal(v)
			}
		})
	}
	m, _, b := publicationFixture(t)
	in := publicationInput(b, "ghcr.io/example/news:1.1.2")
	v, e := m.StartImagePublication(in)
	if e != nil {
		t.Fatal(e)
	}
	m.publicationMu.Lock()
	var record imagePublicationRecord
	_ = m.Store.Get("image-publication", v.ID, &record)
	record.Status = "running"
	_ = m.Store.Put("image-publication", v.ID, record)
	e = m.initImagePublications()
	m.publicationMu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	v, e = m.StartImagePublication(in)
	if e != nil || v.Status != "interrupted" {
		t.Fatal("restart replayed upload", v, e)
	}
}
func TestImagePublicationAnonymousCancellation(t *testing.T) {
	m, d, b := publicationFixture(t)
	v, e := m.StartImagePublication(publicationInput(b, "ghcr.io/example/news:1.1.2"))
	if e != nil {
		t.Fatal(e)
	}
	v = waitPublication(t, m, v.ID)
	d.mu.Lock()
	d.mode = "slow-anonymous"
	d.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	v, e = m.CheckImagePublicationPublic(ctx, v.ID)
	if e != nil || v.PublicAccessVerified || v.PublicChecking || v.PublicAccessError == "" || time.Since(started) > 5*time.Second {
		t.Fatal(v, e, time.Since(started))
	}
}
func TestImagePublicationAdministratorBoundary(t *testing.T) {
	m, _, member, _, _, shared, _ := userFixture(t)
	_, key, e := m.CreateApplicationKey("news-members", KeyInput{Name: "no-publication", WorkspaceIDs: []string{shared.WorkspaceID}, Scopes: []string{"read", "run", "files"}})
	if e != nil {
		t.Fatal(e)
	}
	h := NewHandler(m, userTestAdmin, true)
	for _, token := range []string{member, key} {
		for _, prefix := range []string{"/api", "/api/v1"} {
			for _, entry := range []struct{ method, path string }{{"GET", "/build-center/publications"}, {"POST", "/build-center/publications"}, {"GET", "/build-center/publications/aaaaaaaaaaaaaaaaaaaaaaaa"}, {"GET", "/build-center/publications/aaaaaaaaaaaaaaaaaaaaaaaa/log"}, {"POST", "/build-center/publications/aaaaaaaaaaaaaaaaaaaaaaaa/cancel"}, {"POST", "/build-center/publications/aaaaaaaaaaaaaaaaaaaaaaaa/check-public"}, {"GET", "/build-center/images/aaaaaaaaaaaaaaaaaaaaaaaa/publication-readiness"}} {
				r := appRequest(h, entry.method, prefix+entry.path, `{}`, token, "")
				if r.Code != 403 {
					t.Fatal(entry, r.Code, r.Body.String())
				}
			}
		}
	}
}

func TestImagePublicationHTTPIdempotencyAndCheck(t *testing.T) {
	m, _, source := publicationFixture(t)
	h := NewHandler(m, "", true)
	readiness := v1Request(h, "GET", "/build-center/images/"+BuildCenterImageKey(source.ImageID)+"/publication-readiness", "", "")
	if readiness.Code != 200 || object(t, readiness)["eligible"] != true {
		t.Fatal(readiness.Code, readiness.Body.String())
	}
	input := publicationInput(source, "ghcr.io/example/news:1.1.2")
	raw, _ := json.Marshal(input)
	missing := v1Request(h, "POST", "/build-center/publications", string(raw), "")
	if missing.Code != 400 {
		t.Fatal("mutation missing Idempotency-Key accepted", missing.Code, missing.Body.String())
	}
	first := v1Request(h, "POST", "/build-center/publications", string(raw), "publication-http-01")
	if first.Code != 202 {
		t.Fatal(first.Code, first.Body.String())
	}
	again := v1Request(h, "POST", "/build-center/publications", string(raw), "publication-http-01")
	if again.Code != 202 || strings.TrimSpace(first.Body.String()) != strings.TrimSpace(again.Body.String()) || again.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("http retry differs", first.Body.String(), again.Body.String())
	}
	id := object(t, first)["id"].(string)
	job := waitPublication(t, m, id)
	if job.Status != "succeeded" {
		t.Fatal(job)
	}
	checked := v1Request(h, "POST", "/build-center/publications/"+id+"/check-public", `{}`, "publication-check-01")
	if checked.Code != 200 || object(t, checked)["publicAccessVerified"] != true {
		t.Fatal(checked.Code, checked.Body.String())
	}
	list := v1Request(h, "GET", "/build-center/publications", "", "")
	if list.Code != 200 || len(object(t, list)["items"].([]any)) != 1 {
		t.Fatal(list.Code, list.Body.String())
	}
	log := v1Request(h, "GET", "/build-center/publications/"+id+"/log?after=0", "", "")
	if log.Code != 200 {
		t.Fatal(log.Code, log.Body.String())
	}
	invalid := v1Request(h, "GET", "/build-center/publications/"+id+"/log?after=-1", "", "")
	if invalid.Code != 400 {
		t.Fatal(invalid.Code)
	}
}

func TestImagePublicationPublicCheckManagerShutdown(t *testing.T) {
	m, e := New(t.TempDir(), "", true)
	if e != nil {
		t.Fatal(e)
	}
	closed := false
	defer func() {
		if !closed {
			m.Close()
		}
	}()
	d := &publicationDocker{fakeDocker: newFakeDocker(), mode: "slow-anonymous"}
	m.docker = d
	id := store.ID()
	digest := "sha256:" + strings.Repeat("b", 64)
	record := imagePublicationRecord{ImagePublication: ImagePublication{ID: id, Status: "succeeded", Target: "ghcr.io/example/news:1.1.2", Digest: digest, Reference: "ghcr.io/example/news@" + digest, Platform: "linux/" + runtime.GOARCH}}
	if e = m.Store.Put("image-publication", id, record); e != nil {
		t.Fatal(e)
	}
	checked := make(chan error, 1)
	go func() {
		v, err := m.CheckImagePublicationPublic(context.Background(), id)
		if err == nil && (v.PublicAccessVerified || v.PublicChecking || v.PublicAccessError == "") {
			err = fmt.Errorf("invalid shutdown result: %+v", v)
		}
		checked <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		d.mu.Lock()
		for _, args := range d.calls {
			if args[0] == "--config" {
				started = true
			}
		}
		d.mu.Unlock()
		if started {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !started {
		t.Fatal("public check never started")
	}
	m.Close()
	closed = true
	select {
	case e := <-checked:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("manager closed before public check completed")
	}
	d.mu.Lock()
	calls := append([][]string{}, d.calls...)
	d.mu.Unlock()
	for _, args := range calls {
		if args[0] == "--config" {
			if _, e = os.Stat(args[1]); !os.IsNotExist(e) {
				t.Fatal("anonymous config remained after shutdown", e)
			}
		}
	}
}
