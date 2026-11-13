// Package skillsafety provides bounded, non-executing inspection of Skill source.
package skillsafety

import (
	"encoding/json"
	"io/fs"
)

const (
	Version       = "1.0.0"
	MaxFiles      = 1000
	MaxFileBytes  = 2 << 20
	MaxTotalBytes = 32 << 20
	MaxFindings   = 1000
)

type File struct {
	Path string
	Data []byte
	Mode fs.FileMode
}

type Finding struct {
	ID             string `json:"id"`
	RuleID         string `json:"ruleId"`
	Severity       string `json:"severity"`
	Category       string `json:"category"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	Evidence       string `json:"evidence"`
	Reason         string `json:"reason"`
	Recommendation string `json:"recommendation"`
}

type Skipped struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

type Coverage struct {
	FilesSeen    int       `json:"filesSeen"`
	FilesScanned int       `json:"filesScanned"`
	FilesSkipped int       `json:"filesSkipped"`
	Skipped      []Skipped `json:"skipped"`
}

type Report struct {
	Engine         string          `json:"engine"`
	EngineVersion  string          `json:"engineVersion,omitempty"`
	Digest         string          `json:"digest,omitempty"`
	Status         string          `json:"status"`
	Recommendation string          `json:"recommendation"`
	Findings       []Finding       `json:"findings"`
	Coverage       Coverage        `json:"coverage"`
	Warnings       []string        `json:"warnings"`
	Raw            json.RawMessage `json:"raw,omitempty"`
}
