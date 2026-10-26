package kunproto

import (
	"strings"
	"testing"
)

func TestKunFixtureValidation(t *testing.T) {
	for _, v := range []*WorkspaceFixture{
		{Files: []FixtureFile{{Path: "../escape"}}},
		{Files: []FixtureFile{{Path: "a"}, {Path: "A"}}},
		{Files: []FixtureFile{{Path: "a"}, {Path: "a/b"}}},
		{Files: []FixtureFile{{Path: "a/b"}, {Path: "a"}}},
		{Files: []FixtureFile{{Path: "a", Content: strings.Repeat("a", 32769)}}},
		{Files: []FixtureFile{{Path: "a", Content: "\x00"}}},
		{Files: make([]FixtureFile, 17)},
	} {
		if ValidateWorkspaceFixture(v) == nil {
			t.Fatal("invalid fixture accepted", v)
		}
	}
	good := &WorkspaceFixture{Files: []FixtureFile{{Path: "输入/a.txt", Content: ""}}}
	if err := ValidateWorkspaceFixture(good); err != nil {
		t.Fatal(err)
	}
	large := &WorkspaceFixture{}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		large.Files = append(large.Files, FixtureFile{Path: name, Content: strings.Repeat("a", 32768)})
	}
	if ValidateWorkspaceFixture(large) == nil {
		t.Fatal("aggregate bound missing")
	}
	if _, err := FixtureRelativeRoot("../../escape"); err == nil {
		t.Fatal("invalid identity accepted")
	}
}
