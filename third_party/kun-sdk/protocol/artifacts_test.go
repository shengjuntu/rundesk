package kunproto

import (
	"strings"
	"testing"
)

func TestKunArtifactRulesRejectTraversalAndInvalidDefinitions(t *testing.T) {
	for _, path := range []string{"", "../outside", "/absolute", "a/../b", "a//b", "a/./b", "a\\b", "C:/file", "CON", "nul.txt", "a/LPT1.txt", "trailing.", "a\x00b", "a?b", strings.Repeat("a", 513)} {
		if ValidateArtifactRules([]ArtifactRule{{Path: path, Kind: "file_exists"}}) == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
	for _, r := range []ArtifactRule{{Path: "a", Kind: "script"}, {Path: "a", Kind: "file_exists", Value: "x"}, {Path: "a", Kind: "file_contains"}, {Path: "a", Kind: "file_sha256", Value: strings.Repeat("A", 64)}, {Path: "a", Kind: "file_equals", Value: strings.Repeat("x", 8193)}} {
		if ValidateArtifactRules([]ArtifactRule{r}) == nil {
			t.Fatal("invalid rule accepted", r.Kind)
		}
	}
	good := ArtifactRule{Path: "产物/result.txt", Kind: "file_equals"}
	if ValidateArtifactRules([]ArtifactRule{good}) != nil {
		t.Fatal("empty equality rejected")
	}
	if ValidateArtifactRules([]ArtifactRule{good, good}) == nil {
		t.Fatal("duplicate accepted")
	}
}
