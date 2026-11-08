// Package recipefiles reads editable build recipes from ordinary files.
// Recipes are shipped beside RunDesk, not embedded in its executable.
package recipefiles

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Files retains the old ReadFile("profile.zip") interface for callers. Each
// read packs the current directory, so editing recipe files needs no restart.
var Files diskRecipes

type diskRecipes struct{}

// Directory resolves the active recipe directory. An explicit path never
// silently falls back to another source when missing or invalid.
func Directory() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("RUNDESK_RECIPES_DIR")); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", err
		}
		if err := recipeDirectory(absolute); err != nil {
			return "", fmt.Errorf("RUNDESK_RECIPES_DIR %q: %w", absolute, err)
		}
		return absolute, nil
	}
	var candidates []string
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "recipes"))
	}
	// Source checkouts can run from their root or a package directory in go test.
	if cwd, err := os.Getwd(); err == nil {
		for current := cwd; ; current = filepath.Dir(current) {
			candidates = append(candidates, filepath.Join(current, "recipes"))
			if filepath.Dir(current) == current {
				break
			}
		}
	}
	for _, candidate := range candidates {
		if err := recipeDirectory(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("external recipes directory not found; keep recipes/ beside RunDesk or set RUNDESK_RECIPES_DIR")
}

func recipeDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("expected an ordinary directory")
	}
	return nil
}

func validProfile(name string) bool {
	if name == "" || len(name) > 64 || strings.HasPrefix(name, "-") {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// ProfileDirectory returns the current external files for one profile.
func ProfileDirectory(profile string) (string, error) {
	if !validProfile(profile) {
		return "", fmt.Errorf("invalid recipe name")
	}
	root, err := Directory()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(root, profile)
	if err := recipeDirectory(directory); err != nil {
		return "", fmt.Errorf("recipe %q: %w", profile, err)
	}
	return directory, nil
}

// Metadata reads the single editable recipe descriptor. Older externally
// supplied recipe directories may still use profile.lock.json as their source.
// A directory containing both forms is ambiguous and must be reconciled rather
// than silently taking whichever file a particular caller happens to read.
func Metadata(profile string) ([]byte, error) {
	directory, err := ProfileDirectory(profile)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, name := range []string{"recipe.json", "profile.lock.json"} {
		info, err := os.Lstat(filepath.Join(directory, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("recipe metadata must be an ordinary file: %s", name)
		}
		present[name] = true
	}
	name, err := metadataName(present["recipe.json"], present["profile.lock.json"])
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(directory, name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, (256<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(content) == 0 || len(content) > 256<<10 {
		return nil, fmt.Errorf("recipe metadata must contain 1–262144 bytes: %s", name)
	}
	return content, nil
}

func metadataName(canonical, legacy bool) (string, error) {
	if canonical && legacy {
		return "", fmt.Errorf("recipe contains both recipe.json and profile.lock.json; keep one authoritative descriptor")
	}
	if canonical {
		return "recipe.json", nil
	}
	if legacy {
		return "profile.lock.json", nil
	}
	return "", fmt.Errorf("recipe requires recipe.json (or legacy profile.lock.json)")
}

// sharedPath uses portable, slash-separated names on every host. In particular,
// a Windows drive or backslash must not become a different path on Linux.
func sharedPath(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\:") || strings.IndexFunc(name, unicode.IsControl) >= 0 || path.IsAbs(name) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func generatedPath(name string) bool {
	for _, reserved := range []string{"recipe.json", "profile.lock.json", "SHA256SUMS", "upstream/upstream.json"} {
		if name == reserved || strings.HasPrefix(name, reserved+"/") {
			return true
		}
	}
	return false
}

func uniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate or invalid recipe JSON key: %v", key)
			}
			seen[name] = true
			if err := uniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	case json.Delim('['):
		for decoder.More() {
			if err := uniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	}
	return err
}

// Remove authoring-only references without reordering or reformatting the
// existing lock declaration. Moving a dependency into common changes where it
// is maintained, not the identity of an otherwise identical build context.
func frozenDescriptor(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var start, end int64
	first, found := true, false
	for decoder.More() {
		before := decoder.InputOffset()
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if key == "sharedFiles" {
			if found {
				return nil, fmt.Errorf("duplicate sharedFiles declaration")
			}
			start, end, found = before, decoder.InputOffset(), true
			if first {
				// The first field has no preceding comma; remove its following
				// comma instead when another field follows it.
				for end < int64(len(data)) && (data[end] == ' ' || data[end] == '\t' || data[end] == '\r' || data[end] == '\n') {
					end++
				}
				if end < int64(len(data)) && data[end] == ',' {
					end++
				}
			}
		}
		first = false
	}
	if !found {
		return data, nil
	}
	result := append([]byte{}, data[:start]...)
	return append(result, data[end:]...), nil
}

// sharedSource checks every component before walking a shared tree. A missing
// common source remains an error even if a recipe has a local override.
func sharedSource(root, relative string) (string, os.FileInfo, error) {
	current := root
	var info os.FileInfo
	for _, part := range append([]string{"common"}, strings.Split(relative, "/")...) {
		if info != nil && !info.IsDir() {
			return "", nil, fmt.Errorf("shared source parent is not a directory")
		}
		current = filepath.Join(current, part)
		var err error
		info, err = os.Lstat(current)
		if err != nil {
			return "", nil, fmt.Errorf("shared source %q: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return "", nil, fmt.Errorf("shared source is a link or special file: %s", relative)
		}
	}
	return current, info, nil
}

func addShared(directory string, mappings map[string]string, files map[string][]byte, localDirectories map[string]bool, modes map[string]os.FileMode, total *int64, count *int) error {
	targets := make([]string, 0, len(mappings))
	for target, source := range mappings {
		if !sharedPath(target) || !sharedPath(source) || generatedPath(target) {
			return fmt.Errorf("invalid sharedFiles path: %q -> %q", target, source)
		}
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for i, target := range targets {
		for _, previous := range targets[:i] {
			if strings.HasPrefix(target, previous+"/") {
				return fmt.Errorf("overlapping sharedFiles targets: %s and %s", previous, target)
			}
		}
	}
	for _, target := range targets {
		source, info, err := sharedSource(filepath.Dir(directory), mappings[target])
		if err != nil {
			return err
		}
		read := func(filename, destination string, info os.FileInfo) error {
			if !sharedPath(destination) || generatedPath(destination) {
				return fmt.Errorf("sharedFiles contains a generated or unsafe target: %s", destination)
			}
			if localDirectories[destination] {
				return fmt.Errorf("local directory conflicts with shared file: %s", destination)
			}
			for parent := path.Dir(destination); parent != "."; parent = path.Dir(parent) {
				if _, exists := files[parent]; exists {
					return fmt.Errorf("local file conflicts with shared directory: %s", parent)
				}
			}
			*total += info.Size()
			*count += 1
			if *count > 4000 || *total > 128<<20 {
				return fmt.Errorf("recipe exceeds the build context limit")
			}
			if strings.HasPrefix(destination, "variants/") {
				return nil // CLI-only authoring alternatives never enter default ZIPs.
			}
			content, err := os.ReadFile(filename)
			if err != nil {
				return err
			}
			if int64(len(content)) != info.Size() {
				return fmt.Errorf("shared source changed while taking snapshot: %s", destination)
			}
			if _, local := files[destination]; !local {
				files[destination], modes[destination] = content, info.Mode()
			}
			return nil
		}
		if !info.IsDir() {
			if err := read(source, target, info); err != nil {
				return err
			}
			continue
		}
		if err := filepath.WalkDir(source, func(filename string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("shared source contains a symbolic link: %s", filename)
			}
			relative, err := filepath.Rel(source, filename)
			if err != nil {
				return err
			}
			destination := target
			if relative != "." {
				destination += "/" + filepath.ToSlash(relative)
			}
			if !sharedPath(destination) || generatedPath(destination) {
				return fmt.Errorf("sharedFiles contains a generated or unsafe target: %s", destination)
			}
			if entry.IsDir() {
				if _, exists := files[destination]; exists {
					return fmt.Errorf("local file conflicts with shared directory: %s", destination)
				}
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("shared source contains a non-regular file: %s", filename)
			}
			return read(filename, destination, info)
		}); err != nil {
			return err
		}
	}
	for filename := range files {
		for parent := path.Dir(filename); parent != "."; parent = path.Dir(parent) {
			if _, exists := files[parent]; exists {
				return fmt.Errorf("recipe file/directory conflict: %s and %s", parent, filename)
			}
		}
	}
	return nil
}

func (diskRecipes) ReadFile(name string) ([]byte, error) {
	if !strings.HasSuffix(name, ".zip") || filepath.Base(name) != name || strings.Contains(name, "\\") {
		return nil, &fs.PathError{Op: "read recipe", Path: name, Err: fs.ErrInvalid}
	}
	directory, err := ProfileDirectory(strings.TrimSuffix(name, ".zip"))
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	localDirectories := map[string]bool{}
	modes := map[string]os.FileMode{}
	var total int64
	var count int
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == directory {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("recipe contains a symbolic link: %s", relative)
		}
		if entry.IsDir() {
			localDirectories[relative] = true
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("recipe contains a non-regular file: %s", relative)
		}
		if relative == "SHA256SUMS" {
			return nil // User edits are hashed again in this build snapshot.
		}
		total += info.Size()
		count++
		if count > 4000 || total > 128<<20 {
			return fmt.Errorf("recipe exceeds the build context limit")
		}
		if strings.HasPrefix(relative, "variants/") {
			return nil // Authoring-only alternatives are selected explicitly by the CLI.
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if int64(len(content)) != info.Size() {
			return fmt.Errorf("recipe changed while taking snapshot: %s", relative)
		}
		files[relative], modes[relative] = content, info.Mode()
		return nil
	})
	if err != nil {
		return nil, err
	}
	_, canonical := files["recipe.json"]
	_, legacy := files["profile.lock.json"]
	metadata, err := metadataName(canonical, legacy)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", directory, err)
	}
	if canonical {
		if len(files[metadata]) == 0 || len(files[metadata]) > 256<<10 {
			return nil, fmt.Errorf("recipe metadata must contain 1–262144 bytes: %s", metadata)
		}
		var descriptor struct {
			Upstream    json.RawMessage `json:"upstream"`
			SharedFiles json.RawMessage `json:"sharedFiles"`
		}
		if err := uniqueJSONValue(json.NewDecoder(bytes.NewReader(files[metadata]))); err != nil {
			return nil, fmt.Errorf("invalid recipe.json: %w", err)
		}
		if err := json.Unmarshal(files[metadata], &descriptor); err != nil {
			return nil, fmt.Errorf("invalid recipe.json: %w", err)
		}
		if len(descriptor.SharedFiles) != 0 {
			var mappings map[string]string
			if err := json.Unmarshal(descriptor.SharedFiles, &mappings); err != nil || mappings == nil {
				return nil, fmt.Errorf("recipe.json sharedFiles must be an object of target-to-source strings")
			}
			if err := addShared(directory, mappings, files, localDirectories, modes, &total, &count); err != nil {
				return nil, err
			}
			content, err := frozenDescriptor(files[metadata])
			if err != nil {
				return nil, err
			}
			files[metadata] = content
		}
		if len(descriptor.Upstream) != 0 && !bytes.Equal(bytes.TrimSpace(descriptor.Upstream), []byte("null")) {
			var upstream map[string]json.RawMessage
			if err := json.Unmarshal(descriptor.Upstream, &upstream); err != nil || upstream == nil {
				return nil, fmt.Errorf("recipe.json upstream must be an object")
			}
			if _, exists := files["upstream/upstream.json"]; exists {
				return nil, fmt.Errorf("canonical recipe also contains upstream/upstream.json; keep its declaration only in recipe.json")
			}
			var content bytes.Buffer
			if err := json.Indent(&content, descriptor.Upstream, "", "  "); err != nil {
				return nil, err
			}
			files["upstream/upstream.json"], modes["upstream/upstream.json"] = append(content.Bytes(), '\n'), 0644
		}
		files["profile.lock.json"], modes["profile.lock.json"] = files[metadata], modes[metadata]
		delete(files, metadata)
		delete(modes, metadata)
	}
	if len(files["Dockerfile"]) == 0 || len(files["profile.lock.json"]) == 0 {
		return nil, fmt.Errorf("recipe requires Dockerfile and profile.lock.json: %s", directory)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var manifest strings.Builder
	for _, name := range names {
		fmt.Fprintf(&manifest, "%x  %s\n", sha256.Sum256(files[name]), name)
	}
	files["SHA256SUMS"], modes["SHA256SUMS"] = []byte(manifest.String()), 0644
	// Generated locks, upstream metadata and the checksum manifest also count
	// towards the importer limits. Recheck after materializing all of them so
	// every exported ZIP can be imported and extracted without path conflicts.
	var finalBytes int64
	if len(files) > 4000 {
		return nil, fmt.Errorf("recipe exceeds the final build context limit")
	}
	for filename, content := range files {
		finalBytes += int64(len(content))
		if finalBytes > 128<<20 {
			return nil, fmt.Errorf("recipe exceeds the final build context limit")
		}
		if localDirectories[filename] {
			return nil, fmt.Errorf("recipe file/directory conflict: %s", filename)
		}
		for parent := path.Dir(filename); parent != "."; parent = path.Dir(parent) {
			if _, exists := files[parent]; exists {
				return nil, fmt.Errorf("recipe file/directory conflict: %s and %s", parent, filename)
			}
		}
	}
	names = append(names, "SHA256SUMS")
	sort.Strings(names)
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
		header.SetMode(modes[name])
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = writer.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
