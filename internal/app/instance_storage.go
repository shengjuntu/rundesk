package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	p "github.com/your-org/kun/sdk/protocol"
)

const instanceRecordSchema = 1

func requireCodexExecutionAdapter(i Instance) error {
	if runtimeKind(i.AgentRuntime.Kind) != "codex" {
		return failure(409, "backend_unsupported", "此引擎没有注册 Codex 执行适配器")
	}
	return nil
}

// Engine records use the established agentRuntime object on disk. Only the
// discriminator belongs to RunDesk. The selected adapter owns the other keys.
// codexHome retains its legacy spelling for the registered native-state home.
type persistedInstance Instance
type instanceFields Instance

type instanceStorageEnvelope struct {
	instanceFields
	SchemaVersion json.RawMessage `json:"schemaVersion,omitempty"`
	Engine        json.RawMessage `json:"agentRuntime"`
}

// restoredEngine holds opaque fields of a previously registered adapter.
// It is private: serving instance metadata must not expose unrecognized options.
type restoredEngine struct {
	kind    string
	options json.RawMessage
}

func (v *persistedInstance) UnmarshalJSON(data []byte) error {
	var env instanceStorageEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	versioned := len(env.SchemaVersion) != 0
	var version int
	if versioned && (json.Unmarshal(env.SchemaVersion, &version) != nil || version != instanceRecordSchema) {
		return fmt.Errorf("unsupported instance storage schema %s", env.SchemaVersion)
	}
	var fields map[string]json.RawMessage
	if len(env.Engine) != 0 && !bytes.Equal(env.Engine, []byte("null")) {
		if err := json.Unmarshal(env.Engine, &fields); err != nil {
			return fmt.Errorf("invalid engine options: %w", err)
		}
	}
	var kind string
	if k := fields["kind"]; k != nil {
		if err := json.Unmarshal(k, &kind); err != nil {
			return fmt.Errorf("invalid engine kind: %w", err)
		}
	}
	if versioned && (kind == "" || !slug.MatchString(kind)) {
		return errors.New("versioned instance requires an engine kind")
	}
	i := Instance(env.instanceFields)
	kind = runtimeKind(kind)
	if kind == "codex" || kind == "kun" {
		if len(env.Engine) != 0 {
			if err := json.Unmarshal(env.Engine, &i.AgentRuntime); err != nil {
				return err
			}
		}
	}
	i.AgentRuntime.Kind = kind
	i.restoredEngine = restoredEngine{kind: kind, options: env.Engine}
	*v = persistedInstance(i)
	return nil
}

func (v persistedInstance) MarshalJSON() ([]byte, error) {
	i := Instance(v)
	kind := runtimeKind(i.AgentRuntime.Kind)
	if !slug.MatchString(kind) {
		return nil, errors.New("invalid engine kind")
	}
	fields := map[string]json.RawMessage{}
	if i.restoredEngine.kind == kind && len(i.restoredEngine.options) > 0 {
		if err := json.Unmarshal(i.restoredEngine.options, &fields); err != nil {
			return nil, err
		}
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
	}
	if kind == "codex" || kind == "kun" {
		// Merge only changes in the supported adapter fields. A rename, model
		// preference or execution edit preserves newer native options verbatim.
		var prior p.Config
		if len(fields) > 0 {
			raw, err := json.Marshal(fields)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(raw, &prior); err != nil {
				return nil, err
			}
		}
		original, err := configFields(prior)
		if err != nil {
			return nil, err
		}
		updated, err := configFields(i.AgentRuntime)
		if err != nil {
			return nil, err
		}
		if err := mergeInstanceOptions(fields, original, updated); err != nil {
			return nil, err
		}
	}
	fields["kind"], _ = json.Marshal(kind)
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	version, err := json.Marshal(instanceRecordSchema)
	if err != nil {
		return nil, err
	}
	return json.Marshal(instanceStorageEnvelope{instanceFields: instanceFields(i), SchemaVersion: version, Engine: raw})
}

// Preserve options that this version cannot interpret even when a known
// sibling option in the same nested object has changed.
func mergeInstanceOptions(stored, before, after map[string]json.RawMessage) error {
	for key, next := range after {
		old := before[key]
		if bytes.Equal(old, next) {
			continue
		}
		if len(stored[key]) > 0 && stored[key][0] == '{' && len(old) > 0 && old[0] == '{' && len(next) > 0 && next[0] == '{' {
			var existing, from, to map[string]json.RawMessage
			if err := json.Unmarshal(stored[key], &existing); err != nil {
				return err
			}
			if err := json.Unmarshal(old, &from); err != nil {
				return err
			}
			if err := json.Unmarshal(next, &to); err != nil {
				return err
			}
			if err := mergeInstanceOptions(existing, from, to); err != nil {
				return err
			}
			changed, err := json.Marshal(existing)
			if err != nil {
				return err
			}
			stored[key] = changed
		} else {
			stored[key] = next
		}
	}
	for key := range before {
		if _, present := after[key]; !present {
			delete(stored, key)
		}
	}
	return nil
}

func configFields(config p.Config) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	err = json.Unmarshal(raw, &fields)
	return fields, err
}
