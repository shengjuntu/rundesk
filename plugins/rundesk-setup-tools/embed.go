// Package setuptools supplies a task-local setup repair Skill. It does not
// install a native plugin or grant general host command access.
package setuptools

import "embed"

//go:embed skills
var Files embed.FS
