package vfs

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

type fixedVFS struct{}

func (fixedVFS) Root(context.Context, string, string) (Root, error) {
	return fixedRoot{}, nil
}

func (fixedVFS) Name() string {
	return "fixed"
}

func (fixedVFS) Reconfigure(*config.Config) error {
	return nil
}

func (fixedVFS) Stop() {}

type fixedRoot struct{}

func (fixedRoot) Lstat(context.Context, string) (os.FileInfo, error) {
	return nil, fs.ErrNotExist
}

func (fixedRoot) Readlink(context.Context, string) (string, error) {
	return "target", nil
}

func (fixedRoot) Open(context.Context, string) (File, error) {
	return nil, fs.ErrNotExist
}

func TestInstrumentedVFSLogsRootURLWithoutQuery(t *testing.T) {
	buf := &bytes.Buffer{}
	level := logging.LevelTrace
	logger := logv2.NewWithConfig(&logv2.Config{Writer: buf, LogLevel: &level})
	ctx := logv2.WithLogger(context.Background(), logger)

	root, err := Instrumented(fixedVFS{}).Root(ctx, "https://object-storage.example/archive.zip?token=archive-secret#fragment-secret", "cache-key")
	require.NoError(t, err)
	_, err = root.Readlink(ctx, "link")
	require.NoError(t, err)

	require.NotEmpty(t, buf.String())
	require.NotContains(t, buf.String(), "archive-secret")
	require.NotContains(t, buf.String(), "fragment-secret")
}
