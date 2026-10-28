package app

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/shengjuntu/rundesk/examples/profiletemplates"
)

func (m *Manager) buildRecipeFileContext(id, codexVersion string, allowTemplate bool) (BuildRecipe, []byte, bool, error) {
	r, err := m.BuildCenterRecipe(id)
	if err != nil {
		return r, nil, false, err
	}
	if r.Kind == "builtin" {
		data, err := profiletemplates.Files.ReadFile(r.ProfileID + ".zip")
		if err != nil {
			return r, nil, false, err
		}
		if allowTemplate && codexVersion == "" {
			return r, data, true, nil
		}
		data, err = expandRuntimeImageContext(data, codexVersion)
		return r, data, false, err
	}
	if codexVersion != "" {
		return r, nil, false, failure(400, "custom_recipe_frozen", "自定义配方保持导入时的 Codex 版本；请修改文件后重新导入")
	}
	data, err := verifiedFrozenContext(filepath.Join(m.recipeSnapshotDir(r.ID, r.Revision), "context.zip"), r.ContextSHA256)
	return r, data, false, err
}

func (s *Server) buildRecipeFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/build-center/recipes/{rid}/dockerfile", func(w http.ResponseWriter, request *http.Request) {
		recipe, data, template, err := s.Manager.buildRecipeFileContext(request.PathValue("rid"), strings.TrimSpace(request.URL.Query().Get("codexVersion")), true)
		if err != nil {
			respond(w, nil, err)
			return
		}
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			respond(w, nil, err)
			return
		}
		for _, file := range archive.File {
			if file.Name != recipe.Dockerfile {
				continue
			}
			reader, err := file.Open()
			if err != nil {
				respond(w, nil, err)
				return
			}
			content, err := io.ReadAll(io.LimitReader(reader, (2<<20)+1))
			reader.Close()
			if len(content) > 2<<20 {
				err = failure(413, "recipe_file_limit", "Dockerfile 最多预览 2 MiB；请下载完整配方")
			}
			respond(w, map[string]any{"dockerfile": string(content), "path": recipe.Dockerfile, "template": template}, err)
			return
		}
		respond(w, nil, failure(404, "recipe_file_missing", "配方中缺少所声明的 Dockerfile"))
	})
	mux.HandleFunc("GET /api/build-center/recipes/{rid}/context.zip", func(w http.ResponseWriter, request *http.Request) {
		version := strings.TrimSpace(request.URL.Query().Get("codexVersion"))
		recipe, data, _, err := s.Manager.buildRecipeFileContext(request.PathValue("rid"), version, false)
		if err != nil {
			respond(w, nil, err)
			return
		}
		name := recipe.ID + ".zip"
		if recipe.Kind == "builtin" {
			name = recipe.ProfileID + "-codex-" + version + ".zip"
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}
