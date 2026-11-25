package projectroot

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs"
)

// In case this moch struct ever grows in complexity,
// consider using mockgen instead:
// e.g. internal/domain/mock/resolver_mock.go
type mockRoot struct {
	lstatCalledWith    string
	readlinkCalledWith string
	openCalledWith     string
}

func (m *mockRoot) Lstat(ctx context.Context, name string) (os.FileInfo, error) {
	m.lstatCalledWith = name
	return nil, nil
}

func (m *mockRoot) Readlink(ctx context.Context, name string) (string, error) {
	m.readlinkCalledWith = name
	return "", nil
}

func (m *mockRoot) Open(ctx context.Context, name string) (vfs.File, error) {
	m.openCalledWith = name
	return nil, nil
}

func TestProjectRoot(t *testing.T) {
	tests := map[string]struct {
		path         string
		rootDir      string
		expectedPath string
	}{
		"when the root dir is provided": {
			path:         "some/path",
			rootDir:      "my_root_dir",
			expectedPath: "my_root_dir/some/path",
		},
		"when the root dir is not provided": {
			path:         "some/path",
			rootDir:      "",
			expectedPath: "public/some/path",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			originalRoot := &mockRoot{}
			wrappedRoot := New(test.rootDir, originalRoot)

			wrappedRoot.Lstat(context.Background(), test.path)
			require.Equal(t, test.expectedPath, originalRoot.lstatCalledWith)

			wrappedRoot.Readlink(context.Background(), test.path)
			require.Equal(t, test.expectedPath, originalRoot.readlinkCalledWith)

			wrappedRoot.Open(context.Background(), test.path)
			require.Equal(t, test.expectedPath, originalRoot.openCalledWith)
		})
	}
}
