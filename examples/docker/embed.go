// Package dockertemplate exposes the same template shipped in examples/docker.
package dockertemplate

import "embed"

//go:embed Dockerfile .dockerignore seed README.md build.sh
var Files embed.FS
