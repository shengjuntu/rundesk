package app

import (
	"net/http"
	"os"
	"strings"
)

// File grants follow application/project ownership, not a particular API key.
// Rotating a key therefore does not orphan its uploads.
type uploadAccess struct {
	AppID       string `json:"appId"`
	InstanceID  string `json:"instanceId"`
	WorkspaceID string `json:"workspaceId"`
}

func (s *Server) recordUploadAccess(r *http.Request, wid, path string) error {
	k := principal(r)
	if k == nil {
		return nil
	}
	return s.Manager.Store.Put("upload-access", libraryLinkID(wid, path), uploadAccess{k.AppID, k.InstanceID, wid})
}

func (s *Server) checkFileAccess(r *http.Request, wid, path string) error {
	k := principal(r)
	if k == nil {
		return nil
	}
	if !k.workspace(wid) || !k.scope("files") || !safePath(path) {
		return forbidden()
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && parts[0] == "outputs" {
		session, err := s.Manager.Session(parts[1])
		if err == nil && session.WorkspaceID == wid && sessionVisible(r, session) {
			return s.checkFilePath(wid, path)
		}
	}
	if len(parts) == 2 && parts[0] == "uploads" {
		var access uploadAccess
		if s.Manager.Store.Get("upload-access", libraryLinkID(wid, path), &access) == nil && access.WorkspaceID == wid && access.AppID == k.AppID && access.InstanceID == k.InstanceID {
			return s.checkFilePath(wid, path)
		}
	}
	return forbidden()
}

func (s *Server) checkInputFiles(r *http.Request, wid string, in Input) error {
	for _, path := range in.Files {
		if err := s.checkFileAccess(r, wid, path); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) normalizeApplicationTask(r *http.Request, k *ApplicationKey, t *TaskSpec) error {
	if err := normalizeTask(k, t); err != nil {
		return err
	}
	return s.checkInputFiles(r, t.WorkspaceID, t.Input)
}

// Reject stable symlink aliases inside a shared workspace. This is not an OS
// sandbox: hostile processes with write access require separate environments.
func (s *Server) checkFilePath(wid, path string) error {
	ws, err := s.Manager.Workspace(wid)
	if err != nil {
		return forbidden()
	}
	root, err := os.OpenRoot(ws.Path)
	if err != nil {
		return forbidden()
	}
	defer root.Close()
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return forbidden()
		}
	}
	return nil
}
