// Package resource defines RunDesk-owned resource identities and bindings.
// Descriptors contain no credentials or host locations. A resolver translates a
// binding into a host location only after the caller has authorized its session.
package resource

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Kind string

const (
	KindWorkspace    Kind = "workspace"
	KindOutputs      Kind = "outputs"
	KindRuntimeState Kind = "runtime-state"
	KindArtifact     Kind = "artifact"
	KindSkills       Kind = "skills"
	KindMCP          Kind = "mcp"
	KindPlugins      Kind = "plugins"
	KindPolicy       Kind = "policy"
)

// Storage states who retains the bytes. Reference never implies a backup.
type Storage string

const (
	Reference Storage = "reference"
	Managed   Storage = "managed"
	Snapshot  Storage = "snapshot"
)

type Ref struct {
	Kind    Kind   `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

// Scope is binding provenance, not an authenticated principal. HTTP or other
// entry points must authorize the caller before constructing a session scope.
type Scope struct {
	InstanceID  string `json:"instanceId,omitempty"`
	WorkspaceID string `json:"workspaceId,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	TaskID      string `json:"taskId,omitempty"`
}

type Source struct {
	System string `json:"system"`
	ID     string `json:"id"`
}

// FileSource identifies original workspace bytes without exposing a host path.
type FileSource struct {
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path"`
}

type Role string

const (
	RoleWorkspace      Role = "workspace"
	RoleOutputs        Role = "outputs"
	RoleRuntimeState   Role = "runtime-state"
	RoleProjectSkills  Role = "project-skills"
	RoleInstanceSkills Role = "instance-skills"
	RoleMCP            Role = "mcp"
	RolePlugins        Role = "plugins"
	RolePolicy         Role = "policy"
)

type Access string

const (
	Read  Access = "read"
	Write Access = "write"
)

// Configuration describes a declared source, never an inferred loaded inventory.
// Backend adapters retain their native formats and activation semantics.
type Configuration struct {
	Backend      string   `json:"backend"`
	State        string   `json:"state"` // declared, configured, or unsupported
	NativeFormat string   `json:"nativeFormat,omitempty"`
	Names        []string `json:"names,omitempty"`
}

type Policy struct {
	Sandbox        string `json:"sandbox"`
	ApprovalPolicy string `json:"approvalPolicy"`
	Reviewer       string `json:"reviewer"`
	NetworkAccess  *bool  `json:"networkAccess,omitempty"`
}

type Descriptor struct {
	Ref     Ref     `json:"ref"`
	Owner   Scope   `json:"owner"`
	Storage Storage `json:"storage"`
	Source  Source  `json:"source"`
	// VersionKind is binding (configuration revision), content (immutable
	// digest), or live (no pinned version). A binding revision is not a backup.
	VersionKind   string         `json:"versionKind"`
	Configuration *Configuration `json:"configuration,omitempty"`
	Policy        *Policy        `json:"policy,omitempty"`
}

type Binding struct {
	Role   Role   `json:"role"`
	Ref    Ref    `json:"ref"`
	Access Access `json:"access"`
}

type Manifest struct {
	Schema    int          `json:"schema"`
	Scope     Scope        `json:"scope"`
	Resources []Descriptor `json:"resources"`
	Bindings  []Binding    `json:"bindings"`
}

type Catalog interface {
	List() []Descriptor
	Describe(Ref) (Descriptor, error)
}

type Bindings interface {
	Binding(Role) (Binding, error)
}

type Resolver interface {
	Resolve(Scope, Ref, Access) (Resolved, error)
}

// Location stays private to the host. RelativePath is a portable slash-separated
// relative path. Empty means the root itself. It must never be user-controlled.
type Location struct {
	Root         string `json:"-"`
	RelativePath string `json:"-"`
}

type Record struct {
	Descriptor Descriptor
	Binding    Binding
	Location   *Location `json:"-"`
}

type Resolved struct {
	Descriptor   Descriptor `json:"descriptor"`
	Path         string     `json:"-"`
	RelativePath string     `json:"-"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(code, message string) error { return &Error{Code: code, Message: message} }

// Set is an immutable, session-bound projection of canonical platform settings.
// It owns no original files and does not persist another copy of their contents.
type Set struct {
	scope   Scope
	records []Record
}

func NewSet(scope Scope, records []Record) (*Set, error) {
	s := &Set{scope: scope}
	roles, refs := map[Role]bool{}, map[string]bool{}
	for _, record := range records {
		r := cloneRecord(record)
		if r.Descriptor.Ref.Kind == "" || r.Descriptor.Ref.ID == "" || r.Binding.Role == "" || r.Binding.Ref != r.Descriptor.Ref {
			return nil, fail("resource_invalid", "resource identity and binding must agree")
		}
		if r.Binding.Access != Read && r.Binding.Access != Write {
			return nil, fail("resource_invalid", "unsupported binding access")
		}
		if r.Descriptor.Storage != Reference && r.Descriptor.Storage != Managed && r.Descriptor.Storage != Snapshot {
			return nil, fail("resource_invalid", "unsupported storage ownership")
		}
		if !owns(scope, r.Descriptor.Owner) {
			return nil, fail("resource_scope_mismatch", "resource owner is outside the session binding")
		}
		if r.Descriptor.VersionKind != "live" && r.Descriptor.VersionKind != "binding" && r.Descriptor.VersionKind != "content" {
			return nil, fail("resource_invalid", "version kind must be explicit")
		}
		if r.Descriptor.VersionKind != "live" && r.Descriptor.Ref.Version == "" {
			return nil, fail("resource_invalid", "versioned resources require a version")
		}
		key := string(r.Descriptor.Ref.Kind) + "/" + r.Descriptor.Ref.ID
		if roles[r.Binding.Role] || refs[key] {
			return nil, fail("resource_duplicate", "resource identities and binding roles must be unique")
		}
		roles[r.Binding.Role], refs[key] = true, true
		if r.Location != nil {
			if err := validateLocation(*r.Location); err != nil {
				return nil, err
			}
		}
		s.records = append(s.records, r)
	}
	return s, nil
}

func validateLocation(l Location) error {
	if !filepath.IsAbs(l.Root) || strings.ContainsRune(l.Root, 0) {
		return fail("resource_path_invalid", "resource root must be absolute")
	}
	if l.RelativePath != "" && (l.RelativePath == "." || !filepath.IsLocal(filepath.FromSlash(l.RelativePath)) || path.Clean(l.RelativePath) != l.RelativePath || strings.ContainsAny(l.RelativePath, "\\:\x00")) {
		return fail("resource_path_invalid", "resource location must remain inside its root")
	}
	return nil
}

func owns(scope, owner Scope) bool {
	return (owner.InstanceID == "" || owner.InstanceID == scope.InstanceID) &&
		(owner.WorkspaceID == "" || owner.WorkspaceID == scope.WorkspaceID) &&
		(owner.SessionID == "" || owner.SessionID == scope.SessionID) &&
		(owner.TaskID == "" || owner.TaskID == scope.TaskID)
}

// Reject stable aliases below an authorized root. The root itself comes from
// registered metadata and may be a user's chosen native home. This check is not
// a sandbox or a guarantee against concurrent filesystem mutation: consumers
// opening user bytes must continue to use os.Root and native sandbox controls.
func checkLocation(l Location) error {
	root, err := os.OpenRoot(l.Root)
	if os.IsNotExist(err) {
		return nil
	} // Native runtime may create its state root.
	if err != nil {
		return fail("resource_unavailable", "resource root is unavailable")
	}
	defer root.Close()
	if l.RelativePath == "" {
		return nil
	}
	parts := strings.Split(l.RelativePath, "/")
	for index := range parts {
		info, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:index+1], "/")))
		if os.IsNotExist(err) {
			return nil
		} // The host may create the remaining path.
		if err != nil {
			return fail("resource_unavailable", "resource location is unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fail("resource_path_invalid", "resource location contains a symbolic link")
		}
	}
	return nil
}

func (s *Set) List() []Descriptor {
	out := make([]Descriptor, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, cloneDescriptor(r.Descriptor))
	}
	return out
}

func (s *Set) Describe(ref Ref) (Descriptor, error) {
	for _, r := range s.records {
		if r.Descriptor.Ref.Kind == ref.Kind && r.Descriptor.Ref.ID == ref.ID {
			if r.Descriptor.Ref.Version != ref.Version {
				return Descriptor{}, fail("resource_version_changed", "resource binding version has changed")
			}
			return cloneDescriptor(r.Descriptor), nil
		}
	}
	return Descriptor{}, fail("resource_not_bound", "resource is not bound to this session")
}

func (s *Set) Binding(role Role) (Binding, error) {
	for _, r := range s.records {
		if r.Binding.Role == role {
			return r.Binding, nil
		}
	}
	return Binding{}, fail("resource_not_bound", fmt.Sprintf("resource role %q is not bound", role))
}

func (s *Set) Resolve(scope Scope, ref Ref, access Access) (Resolved, error) {
	if scope != s.scope {
		return Resolved{}, fail("resource_scope_mismatch", "resource belongs to another session binding")
	}
	if access != Read && access != Write {
		return Resolved{}, fail("resource_access_invalid", "unsupported resource access")
	}
	d, err := s.Describe(ref)
	if err != nil {
		return Resolved{}, err
	}
	for _, r := range s.records {
		if r.Descriptor.Ref != ref {
			continue
		}
		if access == Write && r.Binding.Access != Write {
			return Resolved{}, fail("resource_read_only", "resource binding is read only")
		}
		if r.Location == nil {
			return Resolved{}, fail("resource_not_file", "resource uses a native configuration service, not a file location")
		}
		if err := checkLocation(*r.Location); err != nil {
			return Resolved{}, err
		}
		return Resolved{Descriptor: d, Path: filepath.Join(r.Location.Root, filepath.FromSlash(r.Location.RelativePath)), RelativePath: r.Location.RelativePath}, nil
	}
	return Resolved{}, fail("resource_not_bound", "resource is not bound")
}

func (s *Set) Manifest() Manifest {
	m := Manifest{Schema: 1, Scope: s.scope, Resources: s.List(), Bindings: make([]Binding, 0, len(s.records))}
	for _, r := range s.records {
		m.Bindings = append(m.Bindings, r.Binding)
	}
	return m
}

func cloneRecord(r Record) Record {
	r.Descriptor = cloneDescriptor(r.Descriptor)
	if r.Location != nil {
		l := *r.Location
		r.Location = &l
	}
	return r
}

func cloneDescriptor(d Descriptor) Descriptor {
	if d.Configuration != nil {
		c := *d.Configuration
		c.Names = append([]string(nil), c.Names...)
		d.Configuration = &c
	}
	if d.Policy != nil {
		p := *d.Policy
		if p.NetworkAccess != nil {
			v := *p.NetworkAccess
			p.NetworkAccess = &v
		}
		d.Policy = &p
	}
	return d
}

var _ Catalog = (*Set)(nil)
var _ Bindings = (*Set)(nil)
var _ Resolver = (*Set)(nil)
