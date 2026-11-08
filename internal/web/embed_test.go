package web

import (
	"encoding/json"
	"io/fs"
	"reflect"
	"regexp"
	"strings"
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
		if count := strings.Count(string(body), `src="/i18n.js"`); count != 1 {
			t.Errorf("%s must load i18n.js exactly once, got %d", name, count)
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

func TestEnglishCatalogMatchesGeneratedRuntimeDictionary(t *testing.T) {
	authored, err := fs.ReadFile(Files, "locales-en.json")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := fs.ReadFile(Files, "i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	var expected, embedded map[string]string
	if err := json.Unmarshal(authored, &expected); err != nil {
		t.Fatal(err)
	}
	source := string(runtime)
	const marker = "const catalog="
	start := strings.Index(source, marker)
	if start < 0 || strings.Count(source, marker) != 1 {
		t.Fatal("expected one generated catalog declaration")
	}
	start += len(marker)
	decoder := json.NewDecoder(strings.NewReader(source[start:]))
	if err := decoder.Decode(&embedded); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(source[start+int(decoder.InputOffset()):], ";\n const language=") {
		t.Fatal("runtime dictionary extensions must be moved into locales-en.json")
	}
	if !reflect.DeepEqual(expected, embedded) {
		t.Fatalf("locales-en.json and i18n.js dictionaries differ (%d authored, %d runtime); run scripts/update-i18n.py", len(expected), len(embedded))
	}
}
