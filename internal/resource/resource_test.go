package resource

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fileSet(t *testing.T, root, relative string) (*Set, Scope, Ref) {
	t.Helper()
	scope := Scope{InstanceID: "instance", WorkspaceID: "workspace", SessionID: "session"}
	ref := Ref{Kind: KindOutputs, ID: "session", Version: "version-2"}
	set, err := NewSet(scope, []Record{{
		Descriptor: Descriptor{Ref: ref, Owner: scope, Storage: Reference, Source: Source{System: "workspace", ID: "workspace"}, VersionKind: "binding"},
		Binding:    Binding{Role: RoleOutputs, Ref: ref, Access: Read}, Location: &Location{Root: root, RelativePath: relative},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return set, scope, ref
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestResolveRejectsDifferentScopeVersionAndWriteAccess(t *testing.T) {
	set, scope, ref := fileSet(t, t.TempDir(), "outputs/session")
	other := scope
	other.SessionID = "other"
	_, err := set.Resolve(other, ref, Read)
	requireCode(t, err, "resource_scope_mismatch")
	old := ref
	old.Version = "version-1"
	_, err = set.Resolve(scope, old, Read)
	requireCode(t, err, "resource_version_changed")
	_, err = set.Resolve(scope, ref, Write)
	requireCode(t, err, "resource_read_only")
	unknown := ref
	unknown.ID = "not-bound"
	_, err = set.Resolve(scope, unknown, Read)
	requireCode(t, err, "resource_not_bound")
}

func TestPortableLocationsAndManifestDoNotLeakHostPath(t *testing.T) {
	root := t.TempDir()
	set, scope, ref := fileSet(t, root, "outputs/session")
	resolved, err := set.Resolve(scope, ref, Read)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != filepath.Join(root, "outputs", "session") || resolved.RelativePath != "outputs/session" {
		t.Fatalf("portable binding resolved incorrectly: %+v", resolved)
	}
	data, err := json.Marshal(set.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), filepath.Base(root)) || strings.Contains(string(data), "relativePath") {
		t.Fatalf("public manifest leaked location: %s", data)
	}
	resolvedJSON, err := json.Marshal(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(resolvedJSON), filepath.Base(root)) {
		t.Fatal("resolved host location leaked through serialization")
	}
	if _, err := os.Stat(resolved.Path); !os.IsNotExist(err) {
		t.Fatalf("resolution created native directories: %v", err)
	}
}

func TestRejectLocationsEscapingRootsAndSymlinkAliases(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"../outside", "a/../../outside", "/absolute", "a/../outside", "a\\outside", "C:/outside", ".", "a\x00b"} {
		if err := validateLocation(Location{Root: root, RelativePath: relative}); err == nil {
			t.Fatalf("unsafe or noncanonical relative path accepted: %q", relative)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outputs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	set, scope, ref := fileSet(t, root, "outputs/session")
	_, err := set.Resolve(scope, ref, Read)
	requireCode(t, err, "resource_path_invalid")
}

func TestSetOwnsImmutableMetadataAndRejectsForeignOwner(t *testing.T) {
	value := true
	ref := Ref{Kind: KindPolicy, ID: "instance", Version: "4"}
	scope := Scope{InstanceID: "instance", SessionID: "session"}
	record := Record{Descriptor: Descriptor{Ref: ref, Owner: Scope{InstanceID: "instance"}, Storage: Managed, VersionKind: "binding", Policy: &Policy{NetworkAccess: &value}}, Binding: Binding{Role: RolePolicy, Ref: ref, Access: Read}}
	set, err := NewSet(scope, []Record{record})
	if err != nil {
		t.Fatal(err)
	}
	value = false
	manifest := set.Manifest()
	if !*manifest.Resources[0].Policy.NetworkAccess {
		t.Fatal("caller mutated constructed snapshot")
	}
	*manifest.Resources[0].Policy.NetworkAccess = false
	current, err := set.Describe(ref)
	if err != nil || !*current.Policy.NetworkAccess {
		t.Fatal("caller mutated returned snapshot")
	}
	record.Descriptor.Owner.InstanceID = "other-instance"
	_, err = NewSet(scope, []Record{record})
	requireCode(t, err, "resource_scope_mismatch")
	_, err = set.Resolve(scope, ref, Read)
	requireCode(t, err, "resource_not_file")
}
