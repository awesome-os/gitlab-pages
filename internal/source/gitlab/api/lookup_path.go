package api

import (
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

// LookupPath represents a lookup path for a virtual domain
type LookupPath struct {
	ProjectID       int    `json:"project_id,omitempty"`
	AccessControl   bool   `json:"access_control,omitempty"`
	HTTPSOnly       bool   `json:"https_only,omitempty"`
	Prefix          string `json:"prefix,omitempty"`
	Source          Source `json:"source,omitempty"`
	UniqueHost      string `json:"unique_host,omitempty"`
	RootDirectory   string `json:"root_directory,omitempty"`
	PrimaryDomain   string `json:"primary_domain,omitempty"`
	RootNamespaceID int    `json:"root_namespace_id,omitempty"`
}

// Source describes GitLab Page serving variant
type Source struct {
	Type   string `json:"type,omitempty"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Count  int    `json:"file_count,omitempty"`
	Size   int    `json:"file_size,omitempty"`
}

func RedactedLookupPaths(lookupPaths []LookupPath) []LookupPath {
	redacted := make([]LookupPath, len(lookupPaths))

	for i, lookupPath := range lookupPaths {
		redacted[i] = lookupPath
		redacted[i].Source.Path = logging.CleanURL(lookupPath.Source.Path)
	}

	return redacted
}
