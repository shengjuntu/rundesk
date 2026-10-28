package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func codexPlatform(goos, arch string) (target, npmPlatform, binary string, err error) {
	target = map[string]string{"linux/amd64": "x86_64-unknown-linux-musl", "linux/arm64": "aarch64-unknown-linux-musl", "darwin/amd64": "x86_64-apple-darwin", "darwin/arm64": "aarch64-apple-darwin", "windows/amd64": "x86_64-pc-windows-msvc", "windows/arm64": "aarch64-pc-windows-msvc"}[goos+"/"+arch]
	if target == "" {
		return "", "", "", fmt.Errorf("managed installation is unavailable on %s/%s", goos, arch)
	}
	npmOS, npmArch := goos, arch
	if goos == "windows" {
		npmOS = "win32"
	}
	if arch == "amd64" {
		npmArch = "x64"
	}
	binary = "codex"
	if goos == "windows" {
		binary += ".exe"
	}
	return target, npmOS + "-" + npmArch, binary, nil
}

func (m *Manager) installCodexNative(parent context.Context, in CodexInstallInput, progress func(string)) CodexInstallResult {
	result := CodexInstallResult{Version: in.Version, Method: "native", Registry: in.Registry}
	var output limitedOutput
	log := func(message string) {
		_, _ = output.Write([]byte(diagnosticText(message) + "\n"))
		result.Log = output.String()
		if progress != nil {
			progress(result.Log)
		}
	}
	base := filepath.Join(m.Data, "tools", "codex")
	if err := os.MkdirAll(base, 0700); err != nil {
		result.Error = err.Error()
		return result
	}
	dir, err := os.MkdirTemp(base, in.Version+"-")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	log("Installing Codex " + in.Version + " natively from " + in.Registry + "; Node/npm are not required")
	result.Integrity, result.PackageVersion, err = downloadCodexNative(ctx, dir, in.Registry, in.Version, log)
	if err != nil {
		result.Error = diagnosticText(err.Error())
		log("Installation failed: " + result.Error)
		return result
	}
	binary, err := installedCodex(dir)
	if err != nil {
		result.Error = err.Error()
		log(result.Error)
		return result
	}
	observed, err := probeVersion(ctx, binary)
	if err != nil {
		result.Error = "Codex version check failed: " + err.Error()
		log(result.Error)
		return result
	}
	fields := strings.Fields(observed)
	if len(fields) == 0 || fields[len(fields)-1] != in.Version {
		result.Error = "Installed version differs from requested version: " + observed
		log(result.Error)
		return result
	}
	result.OK, result.Codex = true, binary
	log("Verified " + observed + ". Save this executable path in setup to select it; existing installations were not changed.")
	return result
}

type codexPackageMetadata struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	Dist                 struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

func codexDownloadClient() *http.Client {
	return &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many download redirects")
		}
		return buildSourceValue("url", req.URL.String())
	}}
}
func codexMetadata(ctx context.Context, client *http.Client, registry, version string) (codexPackageMetadata, error) {
	var metadata codexPackageMetadata
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registry+"/@openai%2fcodex/"+url.PathEscape(version), nil)
	if err != nil {
		return metadata, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "RunDesk/"+Version)
	response, err := client.Do(req)
	if err != nil {
		return metadata, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return metadata, fmt.Errorf("npm metadata for %s returned HTTP %d", version, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil {
		return metadata, err
	}
	if len(data) > 4<<20 {
		return metadata, fmt.Errorf("npm metadata exceeds 4 MiB")
	}
	if err = json.Unmarshal(data, &metadata); err != nil {
		return metadata, fmt.Errorf("invalid npm metadata: %w", err)
	}
	if metadata.Name != "@openai/codex" || metadata.Version != version {
		return metadata, fmt.Errorf("npm metadata does not match the exact Codex package version %s", version)
	}
	return metadata, nil
}
func codexIntegrity(value string) ([]byte, error) {
	for _, token := range strings.Fields(value) {
		if strings.HasPrefix(token, "sha512-") {
			sum, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(token, "sha512-"))
			if err == nil && len(sum) == sha512.Size {
				return sum, nil
			}
		}
	}
	return nil, fmt.Errorf("Codex package metadata must contain SHA-512 integrity")
}
func downloadCodexNative(ctx context.Context, dir, registry, version string, log func(string)) (integrity, packageVersion string, err error) {
	_, platform, _, err := codexPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", "", err
	}
	client := codexDownloadClient()
	log("Reading exact package metadata and platform dependency for " + platform)
	metadata, err := codexMetadata(ctx, client, registry, version)
	if err != nil {
		return "", "", err
	}
	packageVersion = version
	if alias, ok := metadata.OptionalDependencies["@openai/codex-"+platform]; ok {
		expected := "npm:@openai/codex@" + version + "-" + platform
		if alias != expected {
			return "", "", fmt.Errorf("platform dependency is not pinned to the requested Codex version")
		}
		packageVersion = version + "-" + platform
		metadata, err = codexMetadata(ctx, client, registry, packageVersion)
		if err != nil {
			return "", "", err
		}
	} else if len(metadata.OptionalDependencies) > 0 {
		return "", "", fmt.Errorf("Codex %s does not publish a package for %s", version, platform)
	}
	expected, err := codexIntegrity(metadata.Dist.Integrity)
	if err != nil {
		return "", "", err
	}
	if err = buildSourceValue("url", metadata.Dist.Tarball); err != nil {
		return "", "", fmt.Errorf("invalid Codex archive URL: %w", err)
	}
	archiveURL := metadata.Dist.Tarball
	// Mirrors sometimes retain the official registry in dist.tarball. Preserve
	// the immutable path but use the registry explicitly selected by the user.
	artifactURL, _ := url.Parse(archiveURL)
	registryURL, _ := url.Parse(registry)
	if artifactURL.Hostname() == "registry.npmjs.org" && registryURL.Host != artifactURL.Host {
		artifactURL.Scheme = registryURL.Scheme
		artifactURL.Host = registryURL.Host
		artifactURL.Path = strings.TrimRight(registryURL.Path, "/") + artifactURL.Path
		artifactURL.RawPath = ""
		archiveURL = artifactURL.String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "RunDesk/"+Version)
	response, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Codex platform archive returned HTTP %d", response.StatusCode)
	}
	const limit int64 = 256 << 20
	if response.ContentLength > limit {
		return "", "", fmt.Errorf("Codex archive exceeds 256 MiB")
	}
	archive := filepath.Join(dir, "codex-package.tgz")
	file, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", err
	}
	hash := sha512.New()
	log("Downloading native platform archive")
	progress := &codexDownloadProgress{log: log}
	n, copyErr := io.Copy(io.MultiWriter(file, hash, progress), io.LimitReader(response.Body, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", "", copyErr
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if n > limit {
		return "", "", fmt.Errorf("Codex archive exceeds 256 MiB")
	}
	if subtle.ConstantTimeCompare(hash.Sum(nil), expected) != 1 {
		return "", "", fmt.Errorf("Codex archive SHA-512 integrity mismatch; archive was not extracted or executed")
	}
	integrity = metadata.Dist.Integrity
	log(fmt.Sprintf("Downloaded %d bytes; SHA-512 integrity verified", n))
	destination := filepath.Join(dir, "node_modules", "@openai", "codex-"+platform)
	if err = extractCodexArchive(archive, destination); err != nil {
		return "", "", err
	}
	log("Extracted native package and companion tools; checking executable version")
	return integrity, packageVersion, nil
}
func extractCodexArchive(archive, destination string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if count >= 4096 {
			return fmt.Errorf("Codex archive contains too many files")
		}
		raw := header.Name
		if strings.ContainsAny(raw, "\\:\x00") || strings.HasPrefix(raw, "/") || path.Clean(raw) != strings.TrimSuffix(raw, "/") || strings.HasPrefix(raw, "../") {
			return fmt.Errorf("unsafe path in Codex archive")
		}
		if raw == "package" || raw == "package/" {
			if header.Typeflag == tar.TypeDir {
				continue
			}
			return fmt.Errorf("invalid package root in Codex archive")
		}
		if !strings.HasPrefix(raw, "package/") {
			return fmt.Errorf("Codex archive entry is outside package/")
		}
		relative := strings.TrimPrefix(strings.TrimSuffix(raw, "/"), "package/")
		if relative == "" || relative == "." || strings.HasPrefix(relative, "../") {
			return fmt.Errorf("unsafe path in Codex archive")
		}
		key := relative
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			return fmt.Errorf("duplicate path in Codex archive")
		}
		seen[key] = true
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if header.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("Codex archive contains a link or special file")
		}
		if header.Size < 0 || header.Size > 512<<20 || total+header.Size > 1<<30 {
			return fmt.Errorf("Codex archive expansion exceeds allowed size")
		}
		total += header.Size
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if header.Mode&0111 != 0 {
			mode = 0700
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(out, reader, header.Size)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// Report a bounded number of progress milestones without exposing request
// headers or requiring an npm process merely to obtain the native binary.
type codexDownloadProgress struct {
	bytes    int64
	reported int64
	log      func(string)
}

func (p *codexDownloadProgress) Write(data []byte) (int, error) {
	p.bytes += int64(len(data))
	milestone := p.bytes / (16 << 20)
	if milestone > p.reported {
		p.reported = milestone
		p.log(fmt.Sprintf("Downloaded %d MiB", p.bytes>>20))
	}
	return len(data), nil
}
