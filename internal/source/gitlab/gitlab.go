package gitlab

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/api"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/cache"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/client"
)

// Gitlab source represent a new domains configuration source. We fetch all the
// information about domains from GitLab instance.
type Gitlab struct {
	client     api.Resolver
	enableDisk bool
}

// New returns a new instance of gitlab domain source.
func New(cfg *config.GitLab) (*Gitlab, error) {
	glClient, err := client.NewFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	g := &Gitlab{
		client:     cache.NewCache(glClient, &cfg.Cache),
		enableDisk: cfg.EnableDisk,
	}

	return g, nil
}

// GetDomain return a representation of a domain that we have fetched from
// GitLab
func (g *Gitlab) GetDomain(ctx context.Context, name string) (*domain.Domain, error) {
	lookup := g.client.Resolve(ctx, name)

	if lookup.Error != nil {
		if errors.Is(lookup.Error, client.ErrUnauthorizedAPI) {
			logging.Logger.Error("Pages cannot communicate with an instance of the GitLab API. Please sync your gitlab-secrets.json file: https://docs.gitlab.com/ee/administration/pages/#pages-cannot-communicate-with-an-instance-of-the-gitlab-api", "error", lookup.Error)
		}

		return nil, lookup.Error
	}

	// TODO introduce a second-level cache for domains, invalidate using etags
	// from first-level cache
	d := domain.New(name, lookup.Domain.Certificate, lookup.Domain.Key, lookup.Domain.ClientCertificate, lookup.Domain.RootNamespaceID, g)

	return d, nil
}

// Resolve is supposed to return the serving request containing lookup path,
// subpath for a given lookup and the serving itself created based on a request
// from GitLab pages domains source
func (g *Gitlab) Resolve(r *http.Request) (*serving.Request, error) {
	host := request.GetHostWithoutPort(r)

	response := g.client.Resolve(r.Context(), host)
	if response.Error != nil {
		return nil, response.Error
	}

	urlPath := path.Clean(r.URL.Path)
	if urlPath == "." {
		// If the result of Clean process is an empty string, it returns the string "."
		urlPath = "/"
	}

	lowerURLPath := strings.ToLower(urlPath)
	size := len(response.Domain.LookupPaths)

	for _, lookup := range response.Domain.LookupPaths {
		lookupPrefix := strings.ToLower(lookup.Prefix)
		isSubPath := strings.HasPrefix(lowerURLPath, lookupPrefix)
		isRootPath := lowerURLPath == path.Clean(lookupPrefix)

		if isSubPath || isRootPath {
			subPath := ""
			if isSubPath {
				subPath = urlPath[len(lookupPrefix):]
			}

			srv, err := g.fabricateServing(lookup)
			if err != nil {
				return nil, err
			}

			return &serving.Request{
				Serving:    srv,
				LookupPath: fabricateLookupPath(size, lookup),
				SubPath:    subPath}, nil
		}
	}

	logging.Logger.ErrorContext(logging.LogRequest(r), "could not find project lookup path",
		"error", domain.ErrDomainDoesNotExist,
		"lookup_paths_count", size,
		"lookup_paths", api.RedactedLookupPaths(response.Domain.LookupPaths),
	)

	return nil, domain.ErrDomainDoesNotExist
}
