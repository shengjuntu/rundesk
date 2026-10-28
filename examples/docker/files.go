// Package dockertemplate reads the editable generic template from recipes/generic.
package dockertemplate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shengjuntu/rundesk/examples/profiletemplates"
)

var Files diskTemplate

type diskTemplate struct{}

func (diskTemplate) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:\x00\r\n") {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	directory, err := profiletemplates.ProfileDirectory("generic")
	if err != nil {
		return nil, err
	}
	path := directory
	if name != "." {
		for _, part := range strings.Split(name, "/") {
			path = filepath.Join(path, part)
			info, err := os.Lstat(path)
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
				return nil, fmt.Errorf("generic template contains a link or special file: %s", name)
			}
		}
	}
	return os.Open(path)
}

func (files diskTemplate) ReadFile(name string) ([]byte, error) {
	file, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if len(data) > 32<<20 {
		return nil, fmt.Errorf("generic template file exceeds 32 MiB")
	}
	return data, err
}
