package api

// VirtualDomain represents a GitLab Pages virtual domain that is being sent
// from GitLab API
type VirtualDomain struct {
	Certificate       string `json:"certificate,omitempty"`
	Key               string `json:"key,omitempty"`
	ClientCertificate string `json:"client_certificate,omitempty"`
	RootNamespaceID   int    `json:"root_namespace_id,omitempty"`

	LookupPaths []LookupPath `json:"lookup_paths"`
}
