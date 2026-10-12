package app

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var codexRelease = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

// Resolve npm's JavaScript entry point, avoiding cmd.exe / shell interpolation
// and .cmd launching differences on Windows. No global installation is changed.
func npmCommand() (string, string, error) {
	node, err := exec.LookPath("node")
	if err != nil {
		return "", "", fmt.Errorf("install Node.js and npm for the RunDesk service account first")
	}
	npm, err := exec.LookPath("npm")
	if err != nil {
		return "", "", fmt.Errorf("npm is not on the service PATH")
	}
	real, err := filepath.EvalSymlinks(npm)
	if err != nil {
		return "", "", err
	}
	candidates := []string{real, filepath.Join(filepath.Dir(real), "node_modules", "npm", "bin", "npm-cli.js"), filepath.Join(filepath.Dir(node), "node_modules", "npm", "bin", "npm-cli.js")}
	for _, candidate := range candidates {
		if filepath.Base(candidate) != "npm-cli.js" {
			continue
		}
		if stat, e := os.Stat(candidate); e == nil && stat.Mode().IsRegular() {
			return node, candidate, nil
		}
	}
	return "", "", fmt.Errorf("npm-cli.js could not be located; use the manual installation guide")
}

func installedCodex(root string) (string, error) {
	platform := map[string]string{"linux/amd64": "x86_64-unknown-linux-musl", "linux/arm64": "aarch64-unknown-linux-musl", "darwin/amd64": "x86_64-apple-darwin", "darwin/arm64": "aarch64-apple-darwin", "windows/amd64": "x86_64-pc-windows-msvc", "windows/arm64": "aarch64-pc-windows-msvc"}[runtime.GOOS+"/"+runtime.GOARCH]
	if platform == "" {
		return "", fmt.Errorf("managed installation is unavailable on this platform")
	}
	name := "codex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	suffix := filepath.Join("vendor", platform, "codex", name)
	var found string
	err := filepath.WalkDir(filepath.Join(root, "node_modules"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Type().IsRegular() && strings.HasSuffix(path, string(filepath.Separator)+suffix) {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("npm completed but no supported native Codex binary was found; check the selected release/platform")
	}
	return found, nil
}

type CodexInstallResult struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Codex   string `json:"codex"`
	Log     string `json:"log"`
	Error   string `json:"error"`
}

func (m *Manager) installCodex(parent context.Context, version string) CodexInstallResult {
	result := CodexInstallResult{Version: version}
	node, npm, err := npmCommand()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	base := filepath.Join(m.Data, "tools", "codex")
	if err = os.MkdirAll(base, 0700); err != nil {
		result.Error = err.Error()
		return result
	}
	// Each explicit attempt has a new prefix. Existing and selected binaries are
	// never overwritten. Failed directories remain for an administrator to inspect.
	dir, err := os.MkdirTemp(base, version+"-")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	cmd := exec.CommandContext(ctx, node, npm, "install", "--prefix", dir, "--registry=https://registry.npmjs.org", "--ignore-scripts", "--no-audit", "--no-fund", "--save-exact", "@openai/codex@"+version)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	configureProbeProcess(cmd)
	var output limitedOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err = cmd.Run()
	result.Log = diagnosticText(output.String())
	if err != nil {
		result.Error = err.Error()
		return result
	}
	binary, err := installedCodex(dir)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	observed, err := probeVersion(ctx, binary)
	if err != nil {
		result.Error = "Codex version check failed: " + err.Error()
		return result
	}
	fields := strings.Fields(observed)
	if len(fields) == 0 || fields[len(fields)-1] != version {
		result.Error = "Installed version differs from requested version: " + observed
		return result
	}
	result.OK, result.Codex = true, binary
	return result
}

func (s *Server) codexInstallRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/setup/codex-install", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version string `json:"version"`
			Confirm bool   `json:"confirm"`
		}
		if !decode(w, r, &in) {
			return
		}
		if !in.Confirm || len(in.Version) > 64 || !codexRelease.MatchString(in.Version) {
			respond(w, nil, failure(400, "invalid_install_request", "Choose an exact release and explicitly confirm installation"))
			return
		}
		if s.Manager.Demo {
			respond(w, nil, failure(400, "demo_install_disabled", "Demo mode does not install software"))
			return
		}
		if !s.Manager.installMu.TryLock() {
			respond(w, nil, failure(409, "install_busy", "A Codex installation is already running"))
			return
		}
		defer s.Manager.installMu.Unlock()
		writeJSON(w, 200, s.Manager.installCodex(r.Context(), in.Version))
	})
}
