package kunproto

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Artifact rules are authored before execution. Values are never supplied by a model.
type ArtifactRule struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}
type ArtifactResult struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Reason string `json:"reason"`
	Bytes  *int64 `json:"bytes,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}
type ArtifactEvidence struct {
	RulesHash string           `json:"rulesHash"`
	Results   []ArtifactResult `json:"results"`
}

func ArtifactRulesHash(rules []ArtifactRule) string {
	return fmt.Sprintf("%x", sha256.Sum256(JSON(rules)))
}
func ValidateArtifactRules(rules []ArtifactRule) error {
	if len(rules) > 8 {
		return fmt.Errorf("最多 8 条产物规则")
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if !utf8.ValidString(r.Path) || len(r.Path) == 0 || len(r.Path) > 512 || strings.ContainsAny(r.Path, "\\:\x00") {
			return fmt.Errorf("产物路径必须是工作区内的相对路径，使用 /，最多 512 字节")
		}
		for _, part := range strings.Split(r.Path, "/") {
			stem := strings.ToUpper(strings.Split(part, ".")[0])
			reserved := stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9')
			if part == "" || part == "." || part == ".." || strings.TrimRight(part, ". ") != part || reserved {
				return fmt.Errorf("产物路径不能包含空段、上级目录或设备名")
			}
		}
		for _, ch := range r.Path {
			if ch < 32 || ch == 127 || strings.ContainsRune("<>\"|?*", ch) {
				return fmt.Errorf("产物路径含无效字符")
			}
		}
		if !utf8.ValidString(r.Value) || len(r.Value) > 8192 {
			return fmt.Errorf("产物规则值最多 8 KiB UTF-8")
		}
		switch r.Kind {
		case "file_exists":
			if r.Value != "" {
				return fmt.Errorf("文件存在规则不接受值")
			}
		case "file_contains":
			if r.Value == "" {
				return fmt.Errorf("文件包含规则需要非空值")
			}
		case "file_equals":
		case "file_sha256":
			if len(r.Value) != 64 || strings.Trim(r.Value, "0123456789abcdef") != "" {
				return fmt.Errorf("SHA-256 必须为 64 位小写十六进制")
			}
		default:
			return fmt.Errorf("不支持的产物规则")
		}
		key := string(JSON(r))
		if seen[key] {
			return fmt.Errorf("产物规则不能重复")
		}
		seen[key] = true
	}
	return nil
}
