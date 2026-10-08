package web

import (
	"io/fs"
	"regexp"
	"testing"
)

// Exercise the shipped filesystem, not the source directory used by Node stubs.
func TestHTMLScriptAndStyleAssetsAreEmbedded(t *testing.T) {
	assets := regexp.MustCompile(`(?:src|href)="/([^"?]+\.(?:js|css))"`)
	for _, name := range []string{"index.html", "member.html", "collaboration.html"} {
		body, err := fs.ReadFile(Files, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range assets.FindAllSubmatch(body, -1) {
			file := string(match[1])
			info, err := fs.Stat(Files, file)
			if err != nil || info.Size() == 0 {
				t.Errorf("%s references missing/empty embedded asset %s: %v", name, file, err)
			}
		}
	}
}
