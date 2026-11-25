package disk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"gitlab.com/gitlab-org/labkit/correlation"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/errortracking"
	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/httperrors"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/slowlogs"
	"gitlab.com/gitlab-org/gitlab-pages/internal/redirects"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving/disk/projectroot"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving/disk/symlink"
	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs"
	vfsServing "gitlab.com/gitlab-org/gitlab-pages/internal/vfs/serving"
)

// Reader is a disk access driver
type Reader struct {
	fileSizeMetric *prometheus.HistogramVec
	vfs            vfs.VFS
}

// Show the user some validation messages for their _redirects file
func (reader *Reader) serveRedirectsStatus(h serving.Handler, redirects *redirects.Redirects) {
	h.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	h.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	h.Writer.WriteHeader(http.StatusOK)
	fmt.Fprintln(h.Writer, redirects.Status())
}

// tryRedirects returns true if it successfully handled request
func (reader *Reader) tryRedirects(h serving.Handler) bool {
	defer slowlogs.Recorder(h.Request.Context(), "reader.tryRedirects")()

	ctx := h.Request.Context()

	root, served := reader.root(h)
	if root == nil {
		return served
	}

	r := redirects.ParseRedirects(ctx, root)

	requestURL := cloneURL(h.Request.URL)
	// Taking value from h.Request.Host as h.Request.URL.Host is not populated
	requestURL.Host = h.Request.Host
	rewrittenURL, status, err := r.Rewrite(h.Request.Context(), requestURL, correlation.ExtractFromContext(h.Request.Context()))
	if err != nil {
		if !errors.Is(err, redirects.ErrNoRedirect) {
			// We assume that rewrite failure is not fatal
			// and we only capture the error

			errortracking.CaptureErrWithReqAndStackTrace(err, h.Request)
		}
		return false
	}

	if status == http.StatusOK {
		h.SubPath = strings.TrimPrefix(rewrittenURL.Path, h.LookupPath.Prefix)
		return reader.tryFile(h)
	}

	copySchemeAndHost(requestURL, rewrittenURL)

	http.Redirect(h.Writer, h.Request, rewrittenURL.String(), status)
	return true
}

func copySchemeAndHost(src, dst *url.URL) {
	if dst.Scheme == "" {
		dst.Scheme = src.Scheme
	}
	if dst.Host == "" {
		dst.Host = src.Host
	}
}

// tryFile returns true if it successfully handled request
func (reader *Reader) tryFile(h serving.Handler) bool {
	defer slowlogs.Recorder(h.Request.Context(), "reader.tryFile")()

	ctx := h.Request.Context()

	root, served := reader.root(h)
	if root == nil {
		return served
	}

	fullPath, err := reader.resolvePath(ctx, root, h.SubPath)

	request := h.Request
	urlPath := request.URL.Path

	var wasNegotiated bool

	var locationDirError *locationDirectoryError
	if errors.As(err, &locationDirError) {
		if endsWithSlash(urlPath) {
			if feature.ContentNegotiation.Enabled() {
				base := strings.TrimSuffix(h.SubPath, "/") + "/index"
				fullPath, wasNegotiated, err = reader.negotiatePath(ctx, root, base, h.Request)
			} else {
				fullPath, err = reader.resolvePath(ctx, root, h.SubPath, "index.html")
			}
		} else {
			http.Redirect(h.Writer, h.Request, redirectPath(h.Request), http.StatusFound)
			return true
		}
	}

	var locationFileError *locationFileNoExtensionError
	if errors.As(err, &locationFileError) {
		if feature.ContentNegotiation.Enabled() {
			fullPath, wasNegotiated, err = reader.negotiatePath(ctx, root, strings.TrimSuffix(h.SubPath, "/"), h.Request)
		} else {
			fullPath, err = reader.resolvePath(ctx, root, strings.TrimSuffix(h.SubPath, "/")+".html")
		}
	}

	if err != nil {
		// We assume that this is mostly missing file type of the error
		// and additional handlers should try to process the request
		return false
	}

	var variantExt string
	if wasNegotiated {
		addVaryAccept(h.Writer.Header())
		variantExt = filepath.Ext(fullPath)
	}

	// Serve status of `_redirects` under `_redirects`
	// We check if the final resolved path is `_redirects` after symlink traversal
	if fullPath == redirects.ConfigFile {
		r := redirects.ParseRedirects(ctx, root)
		reader.serveRedirectsStatus(h, r)
		return true
	}

	return reader.serveFile(ctx, h.Writer, h.Request, root, fullPath, h.LookupPath.SHA256, h.LookupPath.HasAccessControl, variantExt)
}

func redirectPath(request *http.Request) string {
	url := *request.URL

	// This ensures that path starts with `//<host>/`
	url.Scheme = ""
	url.Host = request.Host
	url.Path = strings.TrimPrefix(url.Path, "/") + "/"

	return strings.TrimSuffix(url.String(), "?")
}

func addVaryAccept(header http.Header) {
	for _, v := range header.Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Accept") {
				return
			}
		}
	}
	header.Add("Vary", "Accept")
}

func (reader *Reader) tryNotFound(h serving.Handler) bool {
	ctx := h.Request.Context()

	root, served := reader.root(h)
	if root == nil {
		return served
	}

	page404, err := reader.resolvePath(ctx, root, "404.html")
	if err != nil {
		// We assume that this is mostly missing file type of the error
		// and additional handlers should try to process the request
		return false
	}

	err = reader.serveCustomFile(ctx, h.Writer, h.Request, http.StatusNotFound, root, page404)
	if err != nil {
		// The response is still uncommitted here. A client that already left is
		// not a server failure: https://gitlab.com/gitlab-org/gitlab-pages/-/issues/669
		if clientGone(ctx, err) {
			logging.Logger.WarnContext(logging.LogRequest(h.Request), "user cancelled request", logv2.Error(err))
			return false
		}

		httperrors.Serve500WithRequest(h.Writer, h.Request, "serveCustomFile", err)
		return true
	}

	return true
}

// Resolve the HTTP request to a path on disk, converting requests for
// directories to requests for index.html inside the directory if appropriate.
func (reader *Reader) resolvePath(ctx context.Context, root vfs.Root, subPath ...string) (string, error) {
	// Don't use filepath.Join as cleans the path,
	// where we want to traverse full path as supplied by user
	// (including ..)
	testPath := strings.Join(subPath, "/")
	fullPath, err := symlink.EvalSymlinks(ctx, root, testPath)

	if err != nil {
		if endsWithoutHTMLExtension(testPath) {
			return "", &locationFileNoExtensionError{
				FullPath: fullPath,
			}
		}

		return "", err
	}

	fi, err := root.Lstat(ctx, fullPath)
	if err != nil {
		return "", err
	}

	// The requested path is a directory, so try index.html via recursion
	if fi.IsDir() {
		return "", &locationDirectoryError{
			FullPath:     fullPath,
			RelativePath: testPath,
		}
	}

	// The file exists, but is not a supported type to serve. Perhaps a block
	// special device or something else that may be a security risk.
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s: is not a regular file", fullPath)
	}

	return fullPath, nil
}

func (reader *Reader) serveFile(ctx context.Context, w http.ResponseWriter, r *http.Request, root vfs.Root, origPath, sha string, accessControl bool, variantExt string) bool {
	defer slowlogs.Recorder(r.Context(), "reader.serveFile")()

	// The body depends on Accept-Encoding, so shared caches must not reuse
	// this response for a client with a different Accept-Encoding.
	w.Header().Add("Vary", "Accept-Encoding")

	fullPath, encoding := getPathWithEncoding(ctx, r, root, origPath)

	file, err := root.Open(ctx, fullPath)
	if err != nil {
		httperrors.Serve500WithRequest(w, r, "root.Open", err)
		return true
	}

	defer file.Close()

	fi, err := root.Lstat(ctx, fullPath)
	if err != nil {
		httperrors.Serve500WithRequest(w, r, "root.Lstat", err)
		return true
	}

	contentType, err := detectContentType(ctx, root, origPath)
	if err != nil {
		httperrors.Serve500WithRequest(w, r, "detectContentType", err)
		return true
	}

	// Set headers only after all failure points so they never leak into an error page.
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}

	rs, seekable := file.(vfs.SeekableFile)

	// http.ServeContent doesn't set Content-Length if Content-Encoding is set,
	// and non-seekable files are served without computing it.
	if encoding != "" || !seekable {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	}

	w.Header().Set("ETag", fmt.Sprintf("%q", etag(variantExt, encoding, sha)))

	if !accessControl {
		// Set caching headers
		w.Header().Set("Cache-Control", "max-age=600")
		w.Header().Set("Expires", time.Now().Add(10*time.Minute).Format(time.RFC1123))
	}

	w.Header().Set("Content-Type", contentType)

	reader.fileSizeMetric.WithLabelValues(reader.vfs.Name()).Observe(float64(fi.Size()))

	// Support vfs.SeekableFile if available (uncompressed files)
	if seekable {
		http.ServeContent(w, r, origPath, fi.ModTime(), rs)
	} else {
		vfsServing.ServeCompressedFile(w, r, fi.ModTime(), file)
	}

	return true
}

func etag(variantExt, contentEncoding, sha string) string {
	tag := sha
	if variantExt != "" {
		tag += variantExt
	}
	if contentEncoding != "" {
		tag = fmt.Sprintf("%s-%s", tag, contentEncoding)
	}
	return tag
}

// clientGone reports whether err stems from the client going away rather
// than from a server-side failure. net/http cancels the request context when
// the client disconnects, and the VFS surfaces a read interrupted by that
// cancellation as a wrapped context.Canceled.
func clientGone(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled)
}

func (reader *Reader) serveCustomFile(ctx context.Context, w http.ResponseWriter, r *http.Request, code int, root vfs.Root, origPath string) error {
	// The body depends on Accept-Encoding, so shared caches must not reuse
	// this response for a client with a different Accept-Encoding.
	w.Header().Add("Vary", "Accept-Encoding")

	fullPath, encoding := getPathWithEncoding(ctx, r, root, origPath)

	// Open and serve content of file
	file, err := root.Open(ctx, fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	fi, err := root.Lstat(ctx, fullPath)
	if err != nil {
		return err
	}

	contentType, err := detectContentType(ctx, root, origPath)
	if err != nil {
		return err
	}

	reader.fileSizeMetric.WithLabelValues(reader.vfs.Name()).Observe(float64(fi.Size()))

	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	w.WriteHeader(code)

	if r.Method == http.MethodHead {
		return nil
	}

	// The status is committed from here on, so copy errors cannot become a 500.
	out := &failureWriter{Writer: w}
	if _, err := io.CopyN(out, file, fi.Size()); err != nil {
		logCopyError(ctx, r, err, out.encounteredWriteFailure)
	}

	return nil
}

// failureWriter records whether the wrapped writer ever returned an error, so
// that a body copy failure can be attributed to the response side.
type failureWriter struct {
	io.Writer
	encounteredWriteFailure bool
}

func (w *failureWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err != nil {
		w.encounteredWriteFailure = true
	}

	return n, err
}

// logCopyError records a body copy failure after the status was committed. A
// failed response write only means the response can no longer be delivered to
// the client, whichever transport error carries it and whether or not the
// request context has been cancelled yet, so it is never a server failure. The
// client may have left, or a configured server write timeout may have expired
// on a slow client, so the message does not attribute the cause. There is
// nothing for an operator to act on, so it is logged at info level. A failed
// file read is a server failure unless the client left.
func logCopyError(ctx context.Context, r *http.Request, err error, writeFailed bool) {
	if writeFailed || clientGone(ctx, err) {
		logging.Logger.InfoContext(logging.LogRequest(r), "client did not receive custom 404 page", logv2.Error(err))
		return
	}

	logging.Logger.ErrorContext(logging.LogRequest(r), "error reading custom 404 page", logv2.Error(err))
	errortracking.CaptureErrWithReqAndStackTrace(err, r)
}

// root tries to resolve the vfs.Root, wrap it in a projectroot.Root and handles
// errors for it.
// It returns whether we served the response or not.
func (reader *Reader) root(h serving.Handler) (vfs.Root, bool) {
	vfsRoot, err := reader.vfs.Root(h.Request.Context(), h.LookupPath.Path,
		h.LookupPath.SHA256)

	if err == nil {
		// The project root directory changes based on the response obtained
		// from the API.
		root := projectroot.New(h.LookupPath.RootDirectory, vfsRoot)
		return root, false
	}

	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}

	if errors.Is(err, context.Canceled) {
		// Handle context.Canceled error as not found exist https://gitlab.com/gitlab-org/gitlab-pages/-/issues/669
		httperrors.Serve404(h.Writer)
		return nil, true
	}

	httperrors.Serve500WithRequest(h.Writer, h.Request, "vfs.Root", err)
	return nil, true
}
