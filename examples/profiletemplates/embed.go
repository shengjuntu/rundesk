// Package profiletemplates embeds generated, credential-free build contexts.
// Run scripts/embed-profiles.py after changing examples/profiles.py or its inputs.
package profiletemplates

import "embed"

//go:embed news2douyin.zip people-search.zip video-build.zip hyperframes.zip video-use.zip openmontage.zip
var Files embed.FS
