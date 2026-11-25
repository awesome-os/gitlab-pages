package gitlab

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving/disk/zip"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/api"
)

var (
	ErrDiskDisabled = errors.New("gitlab: disk access is disabled via enable-disk=false")
)

// fabricateLookupPath fabricates a serving LookupPath based on the API LookupPath
// `size` argument is DEPRECATED, see
// https://gitlab.com/gitlab-org/gitlab-pages/issues/272
func fabricateLookupPath(size int, lookup api.LookupPath) *serving.LookupPath {
	return &serving.LookupPath{
		ServingType:        lookup.Source.Type,
		Path:               lookup.Source.Path,
		SHA256:             lookup.Source.SHA256,
		Prefix:             lookup.Prefix,
		IsNamespaceProject: lookup.Prefix == "/" && size > 1,
		IsHTTPSOnly:        lookup.HTTPSOnly,
		HasAccessControl:   lookup.AccessControl,
		ProjectID:          uint64(lookup.ProjectID), //nolint:gosec
		UniqueHost:         lookup.UniqueHost,
		RootDirectory:      lookup.RootDirectory,
		PrimaryDomain:      lookup.PrimaryDomain,
		RootNamespaceID:    lookup.RootNamespaceID,
	}
}

// fabricateServing fabricates serving based on the GitLab API response
func (g *Gitlab) fabricateServing(lookup api.LookupPath) (serving.Serving, error) {
	source := lookup.Source
	if err := g.checkDiskAllowed(lookup.ProjectID, source); err != nil {
		return nil, err
	}

	switch source.Type {
	case "zip":
		return zip.Instance(), nil
	}

	return nil, fmt.Errorf("gitlab: unknown serving source type: %q", source.Type)
}

func (g *Gitlab) checkDiskAllowed(projectID int, source api.Source) error {
	if !g.enableDisk {
		if source.Type == "file" || strings.HasPrefix(source.Path, "file://") {
			logging.Logger.ErrorContext(context.Background(), "cannot serve from disk",
				logv2.Error(ErrDiskDisabled),
				slog.Int("project_id", projectID),
				slog.String("source_path", logging.CleanURL(source.Path)),
				slog.String("source_type", source.Type),
			)

			return ErrDiskDisabled
		}
	}

	return nil
}
