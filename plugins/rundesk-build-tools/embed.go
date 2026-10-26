// Package buildtools embeds the build Skill for dedicated task workspaces.
// Copying this Skill is intentionally not a native plugin installation.
package buildtools

import "embed"

//go:embed skills
var Files embed.FS
