package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func publicImageTestEntry() PublicImageEntry {
	return PublicImageEntry{ID: "news-release", ProfileID: "news2douyin", ProfileVersion: "1.1.2", CodexVersion: "0.144.6", Platform: "linux/" + runtime.GOARCH, Sources: []PublicImageSource{{ID: "primary", Name: "Test publisher", Reference: "registry.example.test/team/news@sha256:" + strings.Repeat("b", 64)}}}
}

func publicImageCatalogFixture(t *testing.T, m *Manager) PublicImageCatalog {
	t.Helper()
	catalog, err := m.PublicImages()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Items = []PublicImageEntry{publicImageTestEntry()}
	catalog, err = m.SavePublicImages(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestPublicImagesExternalCatalogCASAndExplicitPath(t *testing.T) {
	m := testManager(t)
	catalog, err := m.PublicImages()
	if err != nil || catalog.SchemaVersion != 1 || len(catalog.Items) != 0 || len(catalog.Revision) != 64 {
		t.Fatal(catalog, err)
	}
	path := filepath.Join(m.Data, "config", "public-images.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	stale := catalog
	catalog = publicImageCatalogFixture(t, m)
	if _, err := m.SavePublicImages(stale); err == nil {
		t.Fatal("stale update accepted")
	}
	data, _ := json.Marshal(catalog)
	configured := filepath.Join(t.TempDir(), "published.json")
	if err := os.WriteFile(configured, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNDESK_PUBLIC_IMAGES", configured)
	loaded, err := m.PublicImages()
	if err != nil || len(loaded.Items) != 1 {
		t.Fatal(loaded, err)
	}
	loaded.Items[0].Sources[0].Name = "Edited registry"
	updated, err := m.SavePublicImages(loaded)
	if err != nil || updated.Revision == loaded.Revision {
		t.Fatal(updated, err)
	}
	saved, _ := os.ReadFile(configured)
	if !strings.Contains(string(saved), "Edited registry") {
		t.Fatal("explicit catalog was not updated")
	}
	if err := os.WriteFile(configured, []byte(`{"schemaVersion":1,"items":[],"unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PublicImages(); err == nil {
		t.Fatal("invalid explicit file silently fell back")
	}
	t.Setenv("RUNDESK_PUBLIC_IMAGES", configured+"-missing")
	if _, err := m.PublicImages(); err == nil {
		t.Fatal("missing explicit file silently fell back")
	}
}

func TestPublicImagesRejectMutableCredentialsAndMismatchedMirrors(t *testing.T) {
	for name, change := range map[string]func(*PublicImageCatalog){
		"mutable-tag": func(c *PublicImageCatalog) { c.Items[0].Sources[0].Reference = "ghcr.io/team/news:latest" },
		"implicit-registry": func(c *PublicImageCatalog) {
			c.Items[0].Sources[0].Reference = "team/news@sha256:" + strings.Repeat("b", 64)
		},
		"credential": func(c *PublicImageCatalog) {
			c.Items[0].Sources[0].Reference = "https://user:secret@ghcr.io/team/news@sha256:" + strings.Repeat("b", 64)
		},
		"newline":          func(c *PublicImageCatalog) { c.Items[0].Sources[0].Name = "registry\nsecret" },
		"unknown-profile":  func(c *PublicImageCatalog) { c.Items[0].ProfileID = "other" },
		"mutable-codex":    func(c *PublicImageCatalog) { c.Items[0].CodexVersion = "latest" },
		"wrong-platform":   func(c *PublicImageCatalog) { c.Items[0].Platform = "windows/amd64" },
		"duplicate-entry":  func(c *PublicImageCatalog) { c.Items = append(c.Items, c.Items[0]) },
		"duplicate-source": func(c *PublicImageCatalog) { c.Items[0].Sources = append(c.Items[0].Sources, c.Items[0].Sources[0]) },
		"different-digest": func(c *PublicImageCatalog) {
			c.Items[0].Sources = append(c.Items[0].Sources, PublicImageSource{ID: "mirror", Name: "Mirror", Reference: "mirror.example.test/team/news@sha256:" + strings.Repeat("c", 64)})
		},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := PublicImageCatalog{SchemaVersion: 1, Items: []PublicImageEntry{publicImageTestEntry()}}
			change(&catalog)
			if _, err := normalizePublicImages(catalog); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
	catalog := PublicImageCatalog{SchemaVersion: 1, Items: []PublicImageEntry{publicImageTestEntry()}}
	catalog.Items[0].Sources = append(catalog.Items[0].Sources, PublicImageSource{ID: "mirror", Name: "Mirror", Reference: "mirror.example.test/team/news@sha256:" + strings.Repeat("b", 64)})
	if _, err := normalizePublicImages(catalog); err != nil {
		t.Fatal(err)
	}
}

type publicImageDocker struct {
	*runtimeImageDocker
	reference string
	wrong     bool
}

func (d *publicImageDocker) Run(ctx context.Context, args ...string) ([]byte, error) {
	data, err := d.runtimeImageDocker.Run(ctx, args...)
	if err != nil || len(args) < 2 || args[0] != "image" || args[1] != "inspect" {
		return data, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	if !d.wrong {
		rows[0]["RepoDigests"] = []string{d.reference}
	}
	return json.Marshal(rows)
}

func TestPublicImagePullFrozenIdentityAndDurableRequestReplay(t *testing.T) {
	m, docker := runtimeImageFixture(t)
	catalog := publicImageCatalogFixture(t, m)
	ref := catalog.Items[0].Sources[0].Reference
	docker.missingImage = ref
	d := &publicImageDocker{runtimeImageDocker: docker, reference: ref}
	m.docker = d
	in := RuntimeImageInput{Source: "pull", PublicImageID: "news-release", PublicSourceID: "primary", CatalogRevision: catalog.Revision, RequestID: "public-pull-request-1"}
	job, err := m.PrepareRuntimeImage(in)
	if err != nil || job.PublicImage == nil || job.Reference != ref || job.PublicImage.CatalogRevision != catalog.Revision || job.RequestID != in.RequestID {
		t.Fatal(job, err)
	}
	job = waitBuild(t, m, DefaultInstance, job.ID, "succeeded", "failed")
	if job.Status != "succeeded" || !job.OfflineReady {
		t.Fatal(job)
	}
	listed, err := m.BuildCenterBuilds()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listed {
		if item.ID == job.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("public download missing from build center")
	}
	// Replaying a lost request remains valid even after an administrator removes
	// the catalog entry. The exact frozen job is returned; there is no new pull.
	catalog.Items = nil
	if _, err = m.SavePublicImages(catalog); err != nil {
		t.Fatal(err)
	}
	again, err := m.PrepareRuntimeImage(in)
	if err != nil || again.ID != job.ID {
		t.Fatal(again, err)
	}
	changed := in
	changed.CodexVersion = "0.157.1"
	if _, err = m.PrepareRuntimeImage(changed); err == nil {
		t.Fatal("same request accepted a changed payload")
	}
	d.mu.Lock()
	pulls := 0
	for _, call := range d.calls {
		if call[0] == "pull" {
			pulls++
			if strings.Join(call, " ") != "pull --platform linux/"+runtime.GOARCH+" "+ref {
				t.Fatal(call)
			}
		}
	}
	d.mu.Unlock()
	if pulls != 1 {
		t.Fatal("request replay repeated pull", pulls)
	}
	if err = m.DeleteBuild(DefaultInstance, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.PrepareRuntimeImage(in); err == nil {
		t.Fatal("removed request was silently requeued")
	}
}

func TestPublicImageSelectionAndDigestMismatch(t *testing.T) {
	for _, kind := range []string{"revision", "profile", "version", "reference", "platform", "recipe-version", "digest"} {
		t.Run(kind, func(t *testing.T) {
			m, docker := runtimeImageFixture(t)
			catalog := publicImageCatalogFixture(t, m)
			if kind == "platform" {
				catalog.Items[0].Platform = "linux/arm64"
				if runtime.GOARCH == "arm64" {
					catalog.Items[0].Platform = "linux/amd64"
				}
			}
			if kind == "recipe-version" {
				catalog.Items[0].ProfileVersion = "99.0.0"
			}
			var err error
			catalog, err = m.SavePublicImages(catalog)
			if err != nil {
				t.Fatal(err)
			}
			in := RuntimeImageInput{Source: "pull", PublicImageID: "news-release", PublicSourceID: "primary", CatalogRevision: catalog.Revision, RequestID: "public-mismatch-request"}
			switch kind {
			case "revision":
				in.CatalogRevision = "stale"
			case "profile":
				in.ProfileID = "people-search"
			case "version":
				in.CodexVersion = "0.157.1"
			case "reference":
				in.Reference = "example:other"
			case "digest":
				m.docker = &publicImageDocker{runtimeImageDocker: docker, reference: catalog.Items[0].Sources[0].Reference, wrong: true}
			}
			job, err := m.PrepareRuntimeImage(in)
			if kind != "digest" {
				if err == nil {
					t.Fatal("mismatched selection accepted", job)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			job = waitBuild(t, m, DefaultInstance, job.ID, "succeeded", "failed")
			if job.Status != "failed" || job.OfflineReady || !strings.Contains(job.Error, "Digest") {
				t.Fatal(job)
			}
		})
	}
}

func TestRuntimeImageConcurrentRequestIsSingleJob(t *testing.T) {
	m, _ := runtimeImageFixture(t)
	in := RuntimeImageInput{Source: "local", ProfileID: "news2douyin", CodexVersion: "0.144.6", Reference: "example:ready", RequestID: "concurrent-runtime-request"}
	results := make(chan ImageBuild, 8)
	problems := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); result, err := m.PrepareRuntimeImage(in); results <- result; problems <- err }()
	}
	wg.Wait()
	close(results)
	close(problems)
	for err := range problems {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for job := range results {
		if id != "" && job.ID != id {
			t.Fatal("duplicate queued jobs")
		}
		id = job.ID
	}
	job := waitBuild(t, m, DefaultInstance, id, "succeeded", "failed")
	if job.Status != "succeeded" {
		t.Fatal(job)
	}
	// Also cover recovering a missing receipt from the persisted job.
	if err := m.Store.Delete("runtime-image-request", runtimeImageRequestKey(in.RequestID)); err != nil {
		t.Fatal(err)
	}
	recovered, err := m.PrepareRuntimeImage(in)
	if err != nil || recovered.ID != id {
		t.Fatal(recovered, err)
	}
}

func TestPublicImageCatalogHTTPAdministratorOnly(t *testing.T) {
	m := testManager(t)
	h := NewHandler(m, "", true)
	response := v1Request(h, "GET", "/setup/public-images", "", "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var catalog PublicImageCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	catalog.Items = []PublicImageEntry{publicImageTestEntry()}
	body, _ := json.Marshal(catalog)
	response = v1Request(h, "PUT", "/setup/public-images", string(body), "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := v1Request(h, "PUT", "/setup/public-images", string(body), ""); response.Code != 409 {
		t.Fatal("CAS conflict missing", response.Code)
	}
	connected, err := m.ConnectApplication("public-catalog-reader", ApplicationConnect{Name: "Reader", InstallationID: "public-catalog-reader"})
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := m.CreateApplicationKey(connected.Application.AppID, KeyInput{Name: "reader", WorkspaceIDs: []string{connected.Application.WorkspaceID}, Scopes: []string{"read", "run"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		response := appRequest(NewHandler(m, "admin-secret", true), method, "/api/v1/setup/public-images", string(body), key, "")
		if response.Code != 403 {
			t.Fatal("application may access global image catalog", method, response.Code, response.Body.String())
		}
	}
}

func TestPublicImageDockerHubDigestSpelling(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	for reference, canonical := range map[string]string{
		"team/news" + digest:                       "docker.io/team/news" + digest,
		"docker.io/team/news" + digest:             "docker.io/team/news" + digest,
		"index.docker.io/team/news" + digest:       "docker.io/team/news" + digest,
		"registry-1.docker.io/team/news" + digest:  "docker.io/team/news" + digest,
		"busybox" + digest:                         "docker.io/library/busybox" + digest,
		"docker.io/busybox" + digest:               "docker.io/library/busybox" + digest,
		"registry-1.docker.io/busybox" + digest:    "docker.io/library/busybox" + digest,
		"ghcr.io/team/news" + digest:               "ghcr.io/team/news" + digest,
		"localhost:5000/team/news" + digest:        "localhost:5000/team/news" + digest,
		"registry.example.test/team/news" + digest: "registry.example.test/team/news" + digest,
	} {
		if got := canonicalImageDigestReference(reference); got != canonical {
			t.Fatal(reference, got, canonical)
		}
	}
	m, docker := runtimeImageFixture(t)
	catalog := publicImageCatalogFixture(t, m)
	catalog.Items[0].Sources[0].Reference = "docker.io/team/news" + digest
	catalog, err := m.SavePublicImages(catalog)
	if err != nil {
		t.Fatal(err)
	}
	m.docker = &publicImageDocker{runtimeImageDocker: docker, reference: "team/news" + digest}
	job, err := m.PrepareRuntimeImage(RuntimeImageInput{Source: "pull", PublicImageID: "news-release", PublicSourceID: "primary", CatalogRevision: catalog.Revision})
	if err != nil {
		t.Fatal(err)
	}
	job = waitBuild(t, m, DefaultInstance, job.ID, "succeeded", "failed")
	if job.Status != "succeeded" {
		t.Fatal(job)
	}
}

func TestRuntimeImageFrozenRetryPreservesAcquisitionSource(t *testing.T) {
	for _, source := range []string{"pull", "local"} {
		t.Run(source, func(t *testing.T) {
			m, docker := runtimeImageFixture(t)
			catalog := publicImageCatalogFixture(t, m)
			input := RuntimeImageInput{Source: "local", ProfileID: "news2douyin", CodexVersion: "0.144.6", Reference: "example:ready"}
			if source == "pull" {
				input = RuntimeImageInput{Source: "pull", PublicImageID: "news-release", PublicSourceID: "primary", CatalogRevision: catalog.Revision}
				m.docker = &publicImageDocker{runtimeImageDocker: docker, reference: catalog.Items[0].Sources[0].Reference}
			}
			original, err := m.PrepareRuntimeImage(input)
			if err != nil {
				t.Fatal(err)
			}
			original = waitBuild(t, m, DefaultInstance, original.ID, "succeeded", "failed")
			if original.Status != "succeeded" {
				t.Fatal(original)
			}
			catalog.Items = nil
			if _, err = m.SavePublicImages(catalog); err != nil {
				t.Fatal(err)
			}
			retried, err := m.RebuildCenterBuild(original.ID, RebuildCenterBuildInput{RequestID: strings.Repeat("e", 24)})
			if err != nil || retried.Source != source || retried.Reference != original.Reference || retried.ContextSHA256 != original.ContextSHA256 || retried.RebuildOf != original.ID {
				t.Fatal(retried, err)
			}
			if source == "pull" && (retried.PublicImage == nil || *retried.PublicImage != *original.PublicImage) {
				t.Fatal("retry discarded public digest verification", retried)
			}
			retried = waitBuild(t, m, DefaultInstance, retried.ID, "succeeded", "failed")
			if retried.Status != "succeeded" || !retried.OfflineReady {
				t.Fatal(retried)
			}
			docker.mu.Lock()
			defer docker.mu.Unlock()
			for _, call := range docker.calls {
				if call[0] == "build" {
					t.Fatal("acquisition retry changed into a build", call)
				}
			}
		})
	}
}
