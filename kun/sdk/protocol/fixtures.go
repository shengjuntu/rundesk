package kunproto

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const FixtureWorkspaceMode = "isolated-fixture-v1"

type FixtureFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Non-nil, including an empty Files list, requests a fresh evaluation directory.
type WorkspaceFixture struct {
	Files []FixtureFile `json:"files"`
}
type WorkspaceFixtureState struct {
	Mode      string `json:"mode"`
	Root      string `json:"root"`
	Hash      string `json:"hash"`
	FileCount int    `json:"fileCount"`
	Bytes     int    `json:"bytes"`
}

func FixtureHash(v *WorkspaceFixture) string { return fmt.Sprintf("%x", sha256.Sum256(JSON(v))) }
func FixtureMode(v *WorkspaceFixture) string {
	if v != nil {
		return FixtureWorkspaceMode
	}
	return ""
}

var fixtureSessionName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func FixtureRelativeRoot(session string) (string, error) {
	if !fixtureSessionName.MatchString(session) {
		return "", fmt.Errorf("invalid fixture session identity")
	}
	return "outputs/" + session + "/evaluation", nil
}
func FixtureRoot(workspace, session string) (string, error) {
	rel, err := FixtureRelativeRoot(session)
	if err != nil {
		return "", err
	}
	return filepath.Join(workspace, filepath.FromSlash(rel)), nil
}
func ValidateWorkspaceFixture(v *WorkspaceFixture) error {
	if v == nil {
		return nil
	}
	if len(v.Files) > 16 {
		return fmt.Errorf("最多 16 个固定输入文件")
	}
	total := 0
	seen := map[string]bool{}
	for _, f := range v.Files {
		if err := ValidateArtifactRules([]ArtifactRule{{Path: f.Path, Kind: "file_exists"}}); err != nil {
			return err
		}
		if !utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0) || len(f.Content) > 32<<10 {
			return fmt.Errorf("每个固定输入文件最多 32 KiB UTF-8，不能包含 NUL")
		}
		key := strings.ToLower(f.Path)
		if seen[key] {
			return fmt.Errorf("固定文件路径重复（忽略大小写）")
		}
		for other := range seen {
			if strings.HasPrefix(other, key+"/") || strings.HasPrefix(key, other+"/") {
				return fmt.Errorf("固定文件路径与目录冲突")
			}
		}
		seen[key] = true
		total += len(f.Content)
	}
	if total > 128<<10 {
		return fmt.Errorf("固定输入文件总计最多 128 KiB")
	}
	return nil
}
