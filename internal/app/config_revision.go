package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/shengjuntu/rundesk/internal/resource"
)

const configurationRevisionKind = "configuration-revision"

// ConfigurationRevision freezes RunDesk's declarations for an execution scope.
// References marked live describe a source; their contents can change externally.
// The model selected for a particular run belongs on the run record.
type ConfigurationRevision struct {
	ID               string                 `json:"id,omitempty"`
	SchemaVersion    int                    `json:"schemaVersion"`
	Basis            string                 `json:"basis"`
	InstanceID       string                 `json:"instanceId"`
	InstanceRevision int                    `json:"instanceRevision"`
	WorkspaceID      string                 `json:"workspaceId"`
	Engine           string                 `json:"engine"`
	DefaultModel     string                 `json:"defaultModel,omitempty"`
	Environment      DeclaredEnvironment    `json:"environment"`
	Provider         DeclaredProvider       `json:"provider"`
	Bindings         []ConfigurationBinding `json:"bindings"`
}

type DeclaredEnvironment struct {
	ID       string        `json:"id,omitempty"`
	Revision int           `json:"revision,omitempty"`
	Spec     ExecutionSpec `json:"spec"`
}

// Provider records configuration identity only. Native credentials, endpoints
// with deployment-specific paths and raw command arguments stay in their owner.
type DeclaredProvider struct {
	Source   string `json:"source"`
	Revision int    `json:"revision,omitempty"`
	State    string `json:"state"`
	Model    string `json:"model,omitempty"`
	WireAPI  string `json:"wireAPI,omitempty"`
}

type ConfigurationBinding struct {
	Role     resource.Role       `json:"role"`
	Access   resource.Access     `json:"access"`
	Resource resource.Descriptor `json:"resource"`
}

func (r ConfigurationRevision) digest() (string, error) {
	r.ID = ""
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (m *Manager) declaredProvider(i Instance, w string) (DeclaredProvider, error) {
	switch runtimeKind(i.AgentRuntime.Kind) {
	case "codex":
		b, err := m.codexProviderBinding(i.ID, w)
		if errors.Is(err, sql.ErrNoRows) {
			return DeclaredProvider{Source: "codex-native-config", State: "unmanaged"}, nil
		}
		if err != nil {
			return DeclaredProvider{}, err
		}
		return DeclaredProvider{Source: "codex-provider-binding", State: b.State, Revision: b.Revision, Model: b.Model, WireAPI: b.WireAPI}, nil
	case "kun":
		return DeclaredProvider{Source: "kun-runtime-config", State: "declared", Revision: i.Revision, Model: i.AgentRuntime.Model}, nil
	default:
		return DeclaredProvider{}, failure(400, "configuration_adapter_unsupported", "此引擎没有已注册的配置适配器")
	}
}

// Do not provision an environment to describe it. In Docker the pair's
// previously selected spec can differ from the instance's current image.
func (m *Manager) declaredEnvironment(i Instance, s Session) (DeclaredEnvironment, error) {
	env := DeclaredEnvironment{Spec: i.Execution.normalized()}
	mode := s.ExecutionMode
	if mode == "" {
		mode = "local"
	}
	if env.Spec.Mode != mode {
		return env, failure(409, "session_execution_changed", "执行方式发生变化，请重新选择运行环境")
	}
	if mode == "docker" {
		env.ID = environmentID(i.ID, s.WorkspaceID)
		if s.EnvironmentID != "" && s.EnvironmentID != env.ID {
			return env, failure(409, "session_environment_mismatch", "工作区与运行环境不匹配")
		}
		m.environmentMu.Lock()
		if v := m.environments[env.ID]; v != nil {
			env.Revision, env.Spec = v.Revision, v.Spec.normalized()
		}
		m.environmentMu.Unlock()
	}
	return env, nil
}

func (m *Manager) captureConfigurationRevision(s Session, manifest resource.Manifest) (ConfigurationRevision, error) {
	var r ConfigurationRevision
	if manifest.Schema != 1 || s.InstanceID == "" || s.WorkspaceID == "" || manifest.Scope != resourceScope(s) {
		return r, failure(409, "configuration_scope_mismatch", "配置声明与会话范围不符")
	}
	i, err := m.Instance(s.InstanceID)
	if err != nil {
		return r, err
	}
	if runtimeKind(i.AgentRuntime.Kind) != runtimeKind(s.RuntimeKind) {
		return r, failure(409, "session_runtime_changed", "运行引擎发生变化，请重新选择执行环境")
	}
	r = ConfigurationRevision{SchemaVersion: 1, Basis: "declared_at_admission", InstanceID: i.ID, InstanceRevision: i.Revision, WorkspaceID: s.WorkspaceID, Engine: runtimeKind(s.RuntimeKind), DefaultModel: i.DefaultModel}
	if r.Environment, err = m.declaredEnvironment(i, s); err != nil {
		return r, err
	}
	if r.Provider, err = m.declaredProvider(i, s.WorkspaceID); err != nil {
		return r, err
	}
	// Only reusable configuration declarations contribute to identity. Output
	// locations and native session state are unique to each conversation.
	descriptors := make(map[resource.Ref]resource.Descriptor, len(manifest.Resources))
	for _, d := range manifest.Resources {
		descriptors[d.Ref] = d
	}
	for _, b := range manifest.Bindings {
		switch b.Role {
		case resource.RoleProjectSkills, resource.RoleInstanceSkills, resource.RoleMCP, resource.RolePlugins, resource.RolePolicy:
			d, ok := descriptors[b.Ref]
			if !ok {
				return r, failure(409, "configuration_binding_missing", "配置绑定缺少资源声明")
			}
			if b.Role == resource.RolePolicy && b.Ref.Version != strconv.Itoa(i.Revision) {
				return r, failure(409, "configuration_changed", "执行配置已变化，请重新提交")
			}
			r.Bindings = append(r.Bindings, ConfigurationBinding{Role: b.Role, Access: b.Access, Resource: d})
		}
	}
	sort.Slice(r.Bindings, func(a, b int) bool { return r.Bindings[a].Role < r.Bindings[b].Role })
	if r.ID, err = r.digest(); err != nil {
		return r, err
	}
	return r, m.Store.PutImmutable(configurationRevisionKind, r.ID, r)
}

// The caller authorizes the source session/run before resolving its private
// configuration reference. A digest alone grants no access to other scopes.
func (m *Manager) configurationRevisionForScope(id, instanceID, workspaceID string) (ConfigurationRevision, error) {
	var r ConfigurationRevision
	sum, err := hex.DecodeString(id)
	if err != nil || len(sum) != sha256.Size {
		return r, failure(404, "configuration_revision_not_found", "配置版本不存在")
	}
	if err = m.Store.Get(configurationRevisionKind, id, &r); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConfigurationRevision{}, failure(404, "configuration_revision_not_found", "配置版本不存在")
		}
		return ConfigurationRevision{}, err
	}
	if r.InstanceID != instanceID || r.WorkspaceID != workspaceID {
		return ConfigurationRevision{}, failure(404, "configuration_revision_not_found", "配置版本不存在")
	}
	expected, err := r.digest()
	if err != nil || r.SchemaVersion != 1 || r.ID != id || expected != id {
		return ConfigurationRevision{}, failure(500, "configuration_revision_invalid", "配置版本完整性校验失败")
	}
	return r, nil
}
