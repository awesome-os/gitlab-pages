package vfs

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/metrics"
)

// Root abstracts the things Pages needs to serve a static site from a given root rootPath.
type Root interface {
	Lstat(ctx context.Context, name string) (os.FileInfo, error)
	Readlink(ctx context.Context, name string) (string, error)
	Open(ctx context.Context, name string) (File, error)
}

type instrumentedRoot struct {
	root     Root
	name     string
	rootPath string
}

func (i *instrumentedRoot) increment(operation string, err error) {
	metrics.VFSOperations.WithLabelValues(i.name, operation, strconv.FormatBool(err == nil)).Inc()
}

func (i *instrumentedRoot) log(ctx context.Context) *slog.Logger {
	return logv2.FromContext(ctx).With(
		slog.String("vfs", i.name),
		slog.String("root-path", logging.CleanURL(i.rootPath)),
	)
}

func (i *instrumentedRoot) Lstat(ctx context.Context, name string) (os.FileInfo, error) {
	fi, err := i.root.Lstat(ctx, name)

	i.increment("Lstat", err)
	i.log(ctx).Log(ctx, logging.LevelTrace, "Lstat call",
		slog.String("name", name),
		logging.NillableError(err),
	)

	return fi, err
}

func (i *instrumentedRoot) Readlink(ctx context.Context, name string) (string, error) {
	target, err := i.root.Readlink(ctx, name)

	i.increment("Readlink", err)
	i.log(ctx).Log(ctx, logging.LevelTrace, "Readlink call",
		slog.String("name", name),
		slog.String("ret-target", target),
		logging.NillableError(err),
	)

	return target, err
}

func (i *instrumentedRoot) Open(ctx context.Context, name string) (File, error) {
	f, err := i.root.Open(ctx, name)

	i.increment("Open", err)
	i.log(ctx).Log(ctx, logging.LevelTrace, "Open call",
		slog.String("name", name),
		logging.NillableError(err),
	)

	return f, err
}
