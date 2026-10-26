package validation

import (
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"regexp"
	"strings"
)

var fastName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,47}$`)

// ValidateFastProgram is shared by the host and worker. No code, backward jumps,
// network schemas or constant terminal answers are accepted by this interpreter.
func ValidateFastProgram(v p.FastProgram) error {
	if v.Schema != 1 || strings.TrimSpace(v.Domain) == "" || len(v.Domain) > 120 || len(v.Steps) < 1 || len(v.Steps) > 64 || len(p.JSON(v)) > 256<<10 {
		return fmt.Errorf("invalid fast program bounds")
	}
	if _, err := CompileArguments(v.InputSchema); err != nil {
		return fmt.Errorf("input schema: %w", err)
	}
	names := map[string]bool{"input": true}
	expr := func(x p.FastValue) error {
		if (x.Ref == "") == (len(x.Literal) == 0) {
			return fmt.Errorf("value needs exactly one ref or literal")
		}
		if x.Ref != "" {
			name := strings.SplitN(x.Ref, "#", 2)[0]
			if !names[name] {
				return fmt.Errorf("unknown value %s", name)
			}
		} else if _, err := StrictJSON(x.Literal); err != nil {
			return err
		}
		return nil
	}
	for n, s := range v.Steps {
		if s.Store != "" {
			if !fastName.MatchString(s.Store) || names[s.Store] {
				return fmt.Errorf("invalid or duplicate store")
			}
		}
		switch s.Op {
		case "tool":
			if s.Tool == "" || len(s.Args) > 64 || s.Store == "" {
				return fmt.Errorf("invalid tool instruction")
			}
			for _, x := range s.Args {
				if err := expr(x); err != nil {
					return err
				}
			}
		case "extract", "assert", "branch", "finish":
			if err := expr(s.Value); err != nil {
				return err
			}
			switch s.Op {
			case "extract":
				if s.Store == "" || len(s.Pointer) > 1024 || (s.Pointer != "" && !strings.HasPrefix(s.Pointer, "/")) {
					return fmt.Errorf("invalid extraction")
				}
			case "assert":
				if _, err := CompileArguments(s.Schema); err != nil {
					return err
				}
			case "branch":
				if s.Then <= n || s.Else <= n || s.Then >= len(v.Steps) || s.Else >= len(v.Steps) {
					return fmt.Errorf("branches must move forward inside program")
				}
				if _, err := StrictJSON(s.Equals); err != nil {
					return err
				}
			case "finish":
				if s.Value.Ref == "" || s.Value.Ref == "input" || strings.HasPrefix(s.Value.Ref, "input#") {
					return fmt.Errorf("finish needs tool-derived evidence")
				}
			}
		default:
			return fmt.Errorf("unsupported fast instruction %s", s.Op)
		}
		if s.Store != "" {
			if s.Op != "tool" && s.Op != "extract" {
				return fmt.Errorf("store only supported for tool/extract")
			}
			names[s.Store] = true
		}
	}
	if v.Steps[len(v.Steps)-1].Op != "finish" {
		return fmt.Errorf("last instruction must finish")
	}
	g := v.Guard
	if g.WorkspaceMode != "" && g.WorkspaceMode != p.FixtureWorkspaceMode {
		return fmt.Errorf("unsupported workspace mode guard")
	}
	if g.HarnessHash == "" || g.EngineVersion == "" || g.ConfigHash == "" || g.SkillsHash == "" || g.MCPHash == "" || g.Workspace == "" || g.CatalogHash == "" || g.ApprovalPolicy == "" {
		return fmt.Errorf("all runtime guards are required")
	}
	return nil
}

// FastGuardFor freezes the actual resolved environment and authorized catalog.
func FastGuardFor(s p.State) p.FastGuard {
	if s.Manifest == nil {
		return p.FastGuard{}
	}
	m := s.Manifest
	return p.FastGuard{WorkspaceMode: m.WorkspaceMode, ContextRevision: m.ContextRevision, HarnessHash: Fingerprint(s.Harness), EngineVersion: m.EngineVersion, ConfigHash: m.ConfigHash, SkillsHash: m.SkillsHash, MCPHash: m.MCPHash, Workspace: m.Workspace, CatalogHash: CatalogFingerprint(s), ApprovalPolicy: s.ApprovalPolicy}
}
