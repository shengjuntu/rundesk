package kun

import (
	"fmt"
	p "github.com/your-org/kun/sdk/protocol"
	"os"
	"path/filepath"
)

// No recursive reset or workspace copy. An existing evaluation directory is
// never adopted, overwritten or removed, including an abandoned partial setup.
func prepareWorkspaceFixture(in p.Start) (*p.WorkspaceFixtureState, error) {
	if in.WorkspaceFixture == nil {
		return nil, nil
	}
	if err := p.ValidateWorkspaceFixture(in.WorkspaceFixture); err != nil {
		return nil, err
	}
	rel, err := p.FixtureRelativeRoot(in.SessionID)
	if err != nil {
		return nil, err
	}
	base, err := os.OpenRoot(in.Workspace)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	if err = base.MkdirAll(filepath.Dir(filepath.FromSlash(rel)), 0700); err != nil {
		return nil, err
	}
	if err = base.Mkdir(filepath.FromSlash(rel), 0700); err != nil {
		return nil, fmt.Errorf("fresh evaluation directory required; do not reuse or reset: %w", err)
	}
	root, err := base.OpenRoot(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	absolute, _ := p.FixtureRoot(in.Workspace, in.SessionID)
	out := &p.WorkspaceFixtureState{Mode: p.FixtureWorkspaceMode, Root: absolute, Hash: p.FixtureHash(in.WorkspaceFixture), FileCount: len(in.WorkspaceFixture.Files)}
	for _, file := range in.WorkspaceFixture.Files {
		name := filepath.FromSlash(file.Path)
		if err = root.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return nil, err
		}
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, writeErr := f.WriteString(file.Content)
		closeErr := f.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out.Bytes += len(file.Content)
	}
	return out, nil
}
