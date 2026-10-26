package app

import (
	"path/filepath"
	"sort"
	"strconv"

	"github.com/shengjuntu/rundesk/internal/resource"
)

// RuntimeResourceService resolves host-owned bindings after task admission.
// It does not authenticate HTTP users or replace the native runtime sandbox.
type RuntimeResourceService interface {
	Resolve(Session, resource.Role) (resource.Resolved, error)
}

type managerResourceService struct{ manager *Manager }

func (s managerResourceService) Resolve(session Session, role resource.Role) (resource.Resolved, error) {
	return s.manager.ResolveSessionResource(session, role)
}

func (m *Manager) ResourceService() RuntimeResourceService { return managerResourceService{m} }

func resourceScope(s Session) resource.Scope {
	return resource.Scope{InstanceID: s.InstanceID, WorkspaceID: s.WorkspaceID, SessionID: s.ID, TaskID: s.TaskID}
}

// SessionResources is a read-through view of the current binding definitions.
// It neither freezes an accepted run nor copies native state. In particular,
// declared skill roots and native config sources are not a loaded inventory.
func (m *Manager) SessionResources(s Session) (resource.Manifest, error) {
	set, err := m.SessionResourceSet(s)
	if err != nil {
		return resource.Manifest{}, err
	}
	return set.Manifest(), nil
}

// SessionResourceSet is internal host authority, not a public lookup by arbitrary
// path. Entry points authorize the session, then resolve only its registered
// workspace/instance. Every local path comes from platform-owned metadata.
func (m *Manager) SessionResourceSet(s Session) (*resource.Set, error) {
	if s.ID == "" || s.ID == "." || !safePath(s.ID) || filepath.Base(s.ID) != s.ID {
		return nil, &resource.Error{Code: "resource_scope_invalid", Message: "session identity must be a single path component"}
	}
	w, err := m.Workspace(s.WorkspaceID)
	if err != nil {
		return nil, err
	}
	i, err := m.Instance(s.InstanceID)
	if err != nil {
		return nil, err
	}
	permissions := i.Permissions.normalized()
	backend := runtimeKind(s.RuntimeKind)
	if backend != "kun" && backend != "codex" {
		return nil, &resource.Error{Code: "resource_backend_unsupported", Message: "runtime has no registered resource bindings"}
	}
	scope := resourceScope(s)
	workspaceOwner := resource.Scope{WorkspaceID: w.ID}
	instanceOwner := resource.Scope{InstanceID: i.ID}
	configurationOwner := instanceOwner
	workspaceAccess := resource.Write
	if permissions.Sandbox == "read-only" {
		workspaceAccess = resource.Read
	}
	home, homeID, homeStorage := i.CodexHome, i.ID, resource.Reference
	if i.Managed || m.Demo {
		homeStorage = resource.Managed
	}
	if s.ExecutionMode == "docker" {
		id := environmentID(i.ID, w.ID)
		if s.EnvironmentID != "" && s.EnvironmentID != id {
			return nil, &resource.Error{Code: "resource_scope_mismatch", Message: "session environment does not match its instance and workspace"}
		}
		home, homeID, homeStorage = filepath.Join(m.environmentRoot(id), "codex"), id, resource.Managed
		configurationOwner.WorkspaceID = w.ID
		for _, environment := range m.Environments(i.ID) {
			if environment.ID == id {
				home = environment.CodexHome
				break
			}
		}
	}
	var records []resource.Record
	add := func(kind resource.Kind, id, version, versionKind string, owner resource.Scope, storage resource.Storage, source resource.Source, role resource.Role, access resource.Access, location *resource.Location, configuration *resource.Configuration) {
		ref := resource.Ref{Kind: kind, ID: id, Version: version}
		records = append(records, resource.Record{
			Descriptor: resource.Descriptor{Ref: ref, Owner: owner, Storage: storage, Source: source, VersionKind: versionKind, Configuration: configuration},
			Binding:    resource.Binding{Role: role, Ref: ref, Access: access}, Location: location,
		})
	}
	add(resource.KindWorkspace, w.ID, strconv.Itoa(w.Revision), "binding", workspaceOwner, resource.Reference,
		resource.Source{System: "workspace", ID: w.ID}, resource.RoleWorkspace, workspaceAccess, &resource.Location{Root: w.Path}, nil)
	add(resource.KindOutputs, s.ID, "", "live", scope, resource.Reference,
		resource.Source{System: "workspace", ID: w.ID}, resource.RoleOutputs, workspaceAccess,
		&resource.Location{Root: w.Path, RelativePath: "outputs/" + s.ID}, nil)
	if backend == "kun" {
		add(resource.KindRuntimeState, "kun/"+s.ID, "", "live", scope, resource.Managed,
			resource.Source{System: "kun", ID: s.ID}, resource.RoleRuntimeState, resource.Write,
			&resource.Location{Root: filepath.Join(m.Data, "kun", "sessions"), RelativePath: s.ID}, nil)
	} else {
		add(resource.KindRuntimeState, "codex/"+homeID, "", "live", configurationOwner, homeStorage,
			resource.Source{System: "codex", ID: homeID}, resource.RoleRuntimeState, resource.Write,
			&resource.Location{Root: home}, nil)
	}
	add(resource.KindSkills, "project/"+w.ID, "", "live", workspaceOwner, resource.Reference,
		resource.Source{System: "workspace", ID: w.ID}, resource.RoleProjectSkills, resource.Read,
		&resource.Location{Root: w.Path, RelativePath: ".agents/skills"},
		&resource.Configuration{Backend: backend, State: "declared", NativeFormat: "SKILL.md"})
	add(resource.KindSkills, "instance/"+homeID, "", "live", configurationOwner, resource.Reference,
		resource.Source{System: "instance-skills", ID: homeID}, resource.RoleInstanceSkills, resource.Read,
		&resource.Location{Root: home, RelativePath: "skills"},
		&resource.Configuration{Backend: backend, State: "declared", NativeFormat: "SKILL.md"})
	if backend == "kun" {
		config, err := m.kunMCPConfig(i.ID)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(config.Servers))
		for name := range config.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		add(resource.KindMCP, "kun/"+i.ID, strconv.Itoa(config.Revision), "binding", instanceOwner, resource.Managed,
			resource.Source{System: "kun-mcp-config", ID: i.ID}, resource.RoleMCP, resource.Read, nil,
			&resource.Configuration{Backend: backend, State: "configured", NativeFormat: "kun-mcp-config", Names: names})
	} else {
		add(resource.KindMCP, "codex/"+homeID, "", "live", configurationOwner, resource.Reference,
			resource.Source{System: "codex-native-config", ID: homeID}, resource.RoleMCP, resource.Read, nil,
			&resource.Configuration{Backend: backend, State: "declared", NativeFormat: "codex-native-config"})
	}
	pluginState := "declared"
	if backend == "kun" {
		pluginState = "unsupported"
	}
	add(resource.KindPlugins, backend+"/"+homeID, "", "live", configurationOwner, resource.Reference,
		resource.Source{System: backend + "-native-config", ID: homeID}, resource.RolePlugins, resource.Read, nil,
		&resource.Configuration{Backend: backend, State: pluginState, NativeFormat: backend + "-native-plugin"})
	policy := policySnapshot(i)
	records = append(records, resource.Record{
		Descriptor: resource.Descriptor{Ref: policy.Ref, Owner: instanceOwner, Storage: resource.Managed,
			Source: resource.Source{System: "instance-permissions", ID: i.ID}, VersionKind: "binding",
			Policy: &resource.Policy{Sandbox: policy.Permissions.Sandbox, ApprovalPolicy: policy.Permissions.ApprovalPolicy, Reviewer: policy.Permissions.Reviewer, NetworkAccess: policy.Permissions.NetworkAccess}},
		Binding: resource.Binding{Role: resource.RolePolicy, Ref: policy.Ref, Access: resource.Read},
	})
	return resource.NewSet(scope, records)
}

func (m *Manager) ResolveSessionResource(s Session, role resource.Role) (resource.Resolved, error) {
	set, err := m.SessionResourceSet(s)
	if err != nil {
		return resource.Resolved{}, err
	}
	binding, err := set.Binding(role)
	if err != nil {
		return resource.Resolved{}, err
	}
	return set.Resolve(resourceScope(s), binding.Ref, resource.Read)
}

// PolicyService owns the platform permission declaration. Runtime backends map
// that declaration to native enforcement and approval mechanisms; this view
// does not claim that a backend supports an operation it cannot implement.
type PolicyService interface {
	ForSession(Session) (PolicySnapshot, error)
}

type PolicySnapshot struct {
	Ref         resource.Ref `json:"ref"`
	Permissions Permissions  `json:"permissions"`
}

type policyInstances interface {
	Instance(...string) (Instance, error)
}
type instancePolicyService struct{ instances policyInstances }

func (m *Manager) PolicyService() PolicyService { return instancePolicyService{instances: m} }

func (s instancePolicyService) ForSession(session Session) (PolicySnapshot, error) {
	i, err := s.instances.Instance(session.InstanceID)
	if err != nil {
		return PolicySnapshot{}, err
	}
	return policySnapshot(i), nil
}

func policySnapshot(i Instance) PolicySnapshot {
	permissions := i.Permissions.normalized()
	if permissions.NetworkAccess != nil {
		value := *permissions.NetworkAccess
		permissions.NetworkAccess = &value
	}
	return PolicySnapshot{Ref: resource.Ref{Kind: resource.KindPolicy, ID: i.ID, Version: strconv.Itoa(i.Revision)}, Permissions: permissions}
}

var _ RuntimeResourceService = managerResourceService{}
var _ PolicyService = instancePolicyService{}
