package projectroot

import (
	"context"
	"os"
	"path/filepath"

	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs"
)

// projectRoot implements the more low-level vfs.Root interface and can be used in its
// stead. The difference is, it always resolves the files inside the project's
// rootDirectory by prepending that dir to any open request.
type projectRoot struct {
	rootDirectory string
	vfsRoot       vfs.Root
}

func New(rootDirectory string, vfsRoot vfs.Root) vfs.Root {
	if rootDirectory == "" {
		// In case the GitLab API is not up-to-date this string may be empty.
		// In that case default to the legacy behavior
		rootDirectory = "public"
	}

	return &projectRoot{
		rootDirectory: rootDirectory,
		vfsRoot:       vfsRoot,
	}
}

func (r *projectRoot) Open(ctx context.Context, name string) (vfs.File, error) {
	return r.vfsRoot.Open(ctx, r.getPath(name))
}

func (r *projectRoot) Lstat(ctx context.Context, name string) (os.FileInfo, error) {
	return r.vfsRoot.Lstat(ctx, r.getPath(name))
}

func (r *projectRoot) Readlink(ctx context.Context, name string) (string, error) {
	return r.vfsRoot.Readlink(ctx, r.getPath(name))
}

func (r *projectRoot) getPath(name string) string {
	return filepath.Join(r.rootDirectory, name)
}
