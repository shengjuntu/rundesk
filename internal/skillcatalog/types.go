// Package skillcatalog reads public Skill catalogs and immutable source files.
// It never installs or executes downloaded content.
package skillcatalog

import (
	"io/fs"
	"net/http"
	"time"
)

const (
	MaxFiles            = 200
	MaxFileBytes  int64 = 2 << 20
	MaxTotalBytes int64 = 20 << 20
	maxAPIBytes   int64 = 8 << 20
)

type Asset struct {
	Path string
	Data []byte
	Mode fs.FileMode
}

type Bundle struct {
	Name      string  `json:"name"`
	SourceURL string  `json:"sourceUrl"`
	Revision  string  `json:"revision"`
	Assets    []Asset `json:"-"`
}

type Origin struct {
	Source string `json:"source"`
	URL    string `json:"url"`
}

type Metric struct {
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Source string  `json:"source"`
}

type Item struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	URL           string   `json:"url"`
	RepositoryURL string   `json:"repositoryUrl,omitempty"`
	Repository    string   `json:"repository,omitempty"`
	SkillPath     string   `json:"skillPath,omitempty"`
	Source        string   `json:"source"`
	Slug          string   `json:"slug,omitempty"`
	Kind          string   `json:"kind"`
	ImportURL     string   `json:"importUrl,omitempty"`
	CanImport     bool     `json:"canImport"`
	Origins       []Origin `json:"origins"`
	Metrics       []Metric `json:"metrics"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
	ScanState     string   `json:"scanState"`
}

type ProviderStatus struct {
	Source  string `json:"source"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Count   int    `json:"count"`
	Code    string `json:"code,omitempty"`
}

type SearchResult struct {
	Items     []Item           `json:"items"`
	Providers []ProviderStatus `json:"providers"`
	Complete  bool             `json:"complete"`
	Warnings  []string         `json:"warnings"`
}

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string         { return e.Message }
func fail(code, message string) *Error { return &Error{Code: code, Message: message} }

type Client struct{ HTTP *http.Client }

func NewClient() *Client { return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}} }

var DefaultSources = []string{"clawhub", "skillsmp", "skills.sh"}
