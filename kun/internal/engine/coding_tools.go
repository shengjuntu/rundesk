package kun

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

func relativeToolPath(value string) (string, error) {
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\\\x00") {
		return "", fmt.Errorf("path must be workspace-relative")
	}
	clean := filepath.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return clean, nil
}

type textEdit struct {
	Old string `json:"oldText"`
	New string `json:"newText"`
}

func applyTextEdits(original string, edits []textEdit) (string, error) {
	if len(edits) == 0 || len(edits) > 32 {
		return "", fmt.Errorf("1–32 edits required")
	}
	type span struct {
		start, end  int
		replacement string
	}
	var spans []span
	for _, edit := range edits {
		if edit.Old == "" || !utf8.ValidString(edit.New) || strings.ContainsRune(edit.New, 0) {
			return "", fmt.Errorf("oldText must be nonempty and newText valid UTF-8 without NUL")
		}
		start := strings.Index(original, edit.Old)
		if start < 0 || strings.Contains(original[start+1:], edit.Old) {
			return "", fmt.Errorf("oldText must match exactly one region of the original file")
		}
		spans = append(spans, span{start, start + len(edit.Old), edit.New})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var out strings.Builder
	cursor := 0
	for _, s := range spans {
		if s.start < cursor {
			return "", fmt.Errorf("edits overlap")
		}
		out.WriteString(original[cursor:s.start])
		out.WriteString(s.replacement)
		cursor = s.end
	}
	out.WriteString(original[cursor:])
	if out.Len() > 128<<10 {
		return "", fmt.Errorf("edited file exceeds 128 KiB")
	}
	return out.String(), nil
}
func executeEdit(workspace string, call p.ToolCall) (string, error) {
	var args struct {
		Path  string     `json:"path"`
		Edits []textEdit `json:"edits"`
	}
	d := json.NewDecoder(strings.NewReader(call.Function.Arguments))
	d.DisallowUnknownFields()
	if err := d.Decode(&args); err != nil {
		return "", err
	}
	clean, err := relativeToolPath(args.Path)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat(clean)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("edit requires a regular file, not a symlink")
	}
	f, err := root.OpenFile(clean, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	opened, statErr := f.Stat()
	if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		f.Close()
		return "", fmt.Errorf("file changed before editing")
	}
	data, readErr := io.ReadAll(io.LimitReader(f, (128<<10)+1))
	_ = f.Close()
	if readErr != nil {
		return "", readErr
	}
	if len(data) > 128<<10 || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", fmt.Errorf("edit requires UTF-8 text at most 128 KiB without NUL")
	}
	edited, err := applyTextEdits(string(data), args.Edits)
	if err != nil {
		return "", err
	}
	random := make([]byte, 12)
	if _, err = rand.Read(random); err != nil {
		return "", err
	}
	temp := filepath.Join(filepath.Dir(clean), fmt.Sprintf(".kun-edit-%x", random))
	w, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return "", err
	}
	defer root.Remove(temp)
	err = w.Chmod(info.Mode().Perm())
	if err == nil {
		_, err = w.WriteString(edited)
	}
	if err == nil {
		err = w.Sync()
	}
	closeErr := w.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	// Detect intervening changes before replace. External writers are not locked.
	current, err := root.Lstat(clean)
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, current) || info.Size() != current.Size() || !info.ModTime().Equal(current.ModTime()) {
		return "", fmt.Errorf("file changed while editing; reread before retry")
	}
	if err = root.Rename(temp, clean); err != nil {
		return "", err
	}
	return fmt.Sprintf("Applied %d edits to %s (%d bytes)", len(args.Edits), args.Path, len(edited)), nil
}

func executeSearch(ctx context.Context, workspace string, call p.ToolCall) (string, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignoreCase"`
		Literal    bool   `json:"literal"`
		Limit      int    `json:"limit"`
	}
	d := json.NewDecoder(strings.NewReader(call.Function.Arguments))
	d.DisallowUnknownFields()
	if err := d.Decode(&args); err != nil {
		return "", err
	}
	if args.Path == "" {
		args.Path = "."
	}
	clean, err := relativeToolPath(args.Path)
	if err != nil {
		return "", err
	}
	if args.Limit == 0 {
		args.Limit = 100
	}
	if args.Limit < 1 || args.Limit > 500 || len(args.Pattern) == 0 || len(args.Pattern) > 4096 {
		return "", fmt.Errorf("invalid pattern or limit")
	}
	glob := args.Glob
	if call.Function.Name == "find" {
		glob = args.Pattern
	}
	if glob != "" {
		if _, err = path.Match(glob, "x"); err != nil {
			return "", err
		}
		if strings.Contains(glob, "**") {
			return "", fmt.Errorf("recursive ** glob is not supported; use a basename pattern such as *.go")
		}
	}
	var re *regexp.Regexp
	if call.Function.Name == "grep" {
		pattern := args.Pattern
		if args.Literal {
			pattern = regexp.QuoteMeta(pattern)
		}
		if args.IgnoreCase {
			pattern = "(?i)" + pattern
		}
		re, err = regexp.Compile(pattern)
		if err != nil {
			return "", err
		}
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return "", err
	}
	defer root.Close()
	startInfo, err := root.Lstat(clean)
	if err != nil {
		return "", err
	}
	if startInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("search path must not be a symlink")
	}
	type match struct {
		Path string `json:"path"`
		Line int    `json:"line,omitempty"`
		Text string `json:"text,omitempty"`
	}
	out := struct {
		Matches   []match `json:"matches"`
		Truncated bool    `json:"truncated"`
		Skipped   int     `json:"skippedFiles"`
	}{Matches: []match{}}
	visited, totalBytes, outputBytes := 0, 0, 0
	err = fs.WalkDir(root.FS(), filepath.ToSlash(clean), func(name string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > 10000 {
			out.Truncated = true
			return fs.SkipAll
		}
		if walkErr != nil {
			if name == filepath.ToSlash(clean) {
				return walkErr
			}
			out.Skipped++
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			out.Skipped++
			return nil
		}
		if entry.IsDir() {
			if name != filepath.ToSlash(clean) && entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			out.Skipped++
			return nil
		}
		if glob != "" {
			m, _ := path.Match(glob, name)
			n, _ := path.Match(glob, entry.Name())
			if !m && !n {
				return nil
			}
		}
		add := func(v match) bool {
			if len(out.Matches) >= args.Limit || outputBytes+len(v.Path)+len(v.Text) > 128<<10 {
				out.Truncated = true
				return false
			}
			out.Matches = append(out.Matches, v)
			outputBytes += len(v.Path) + len(v.Text)
			return true
		}
		if re == nil {
			if !add(match{Path: name}) {
				return fs.SkipAll
			}
			return nil
		}
		if totalBytes >= 16<<20 {
			out.Truncated = true
			return fs.SkipAll
		}
		f, e := root.OpenFile(filepath.FromSlash(name), os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if e != nil {
			out.Skipped++
			return nil
		}
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			f.Close()
			out.Skipped++
			return nil
		}
		remaining := (16 << 20) - totalBytes
		data, e := io.ReadAll(io.LimitReader(f, int64(min((1<<20)+1, remaining))))
		f.Close()
		totalBytes += len(data)
		if int64(len(data)) < info.Size() && len(data) == remaining {
			out.Truncated = true
			return fs.SkipAll
		}
		if e != nil || len(data) > 1<<20 || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			out.Skipped++
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				if len(line) > 2048 {
					line = strings.ToValidUTF8(line[:2048], "�") + " [line truncated]"
					out.Truncated = true
				}
				if !add(match{Path: name, Line: i + 1, Text: line}) {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return string(p.JSON(out)), nil
}
