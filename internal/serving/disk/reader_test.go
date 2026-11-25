package disk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	logtest "gitlab.com/gitlab-org/labkit/v2/testing/logtest"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs"
)

func Test_redirectPath(t *testing.T) {
	tests := map[string]struct {
		request      *http.Request
		expectedPath string
	}{
		"simple_url_no_path": {
			request:      newRequest(t, "https://domain.gitlab.io"),
			expectedPath: "//domain.gitlab.io/",
		},
		"path_only": {
			request:      newRequest(t, "https://domain.gitlab.io/index.html"),
			expectedPath: "//domain.gitlab.io/index.html/",
		},
		"query_only": {
			request:      newRequest(t, "https://domain.gitlab.io?query=test"),
			expectedPath: "//domain.gitlab.io/?query=test",
		},
		"empty_query": {
			request:      newRequest(t, "https://domain.gitlab.io?"),
			expectedPath: "//domain.gitlab.io/",
		},
		"fragment_only": {
			request:      newRequest(t, "https://domain.gitlab.io#fragment"),
			expectedPath: "//domain.gitlab.io/#fragment",
		},
		"path_and_query": {
			request:      newRequest(t, "https://domain.gitlab.io/index.html?query=test"),
			expectedPath: "//domain.gitlab.io/index.html/?query=test",
		},
		"path_and_fragment": {
			request:      newRequest(t, "https://domain.gitlab.io/index.html#fragment"),
			expectedPath: "//domain.gitlab.io/index.html/#fragment",
		},
		"query_and_fragment": {
			request:      newRequest(t, "https://domain.gitlab.io?query=test#fragment"),
			expectedPath: "//domain.gitlab.io/?query=test#fragment",
		},
		"path_query_and_fragment": {
			request:      newRequest(t, "https://domain.gitlab.io/index.html?query=test#fragment"),
			expectedPath: "//domain.gitlab.io/index.html/?query=test#fragment",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := redirectPath(test.request)
			require.Equal(t, test.expectedPath, got)
		})
	}
}

func newRequest(t *testing.T, url string) *http.Request {
	t.Helper()

	r, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)

	return r
}

func TestDetectContentTypeByExtension(t *testing.T) {
	tests := map[string]struct {
		path         string
		expectedType string
	}{
		"js file": {
			path:         "script.js",
			expectedType: "",
		},
		"text file": {
			path:         "myfile.txt",
			expectedType: "text/plain; charset=UTF-8",
		},
		"unknown extension": {
			path:         "file.unknown",
			expectedType: "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			contentType := detectContentTypeByExtension(filepath.Ext(tc.path))

			require.Equal(t, tc.expectedType, contentType)
		})
	}
}

func TestReader_tryNotFound_customFile(t *testing.T) {
	tests := map[string]struct {
		method    string
		openErr   error
		writeErr  error
		readErr   error
		cancelCtx bool

		wantHandled bool
		wantStatus  int
		wantLevels  []slog.Level
	}{
		"http2 stream reset by client": {
			writeErr:    errors.New("http2: stream closed"),
			cancelCtx:   true,
			wantHandled: true,
			wantStatus:  http.StatusNotFound,
			wantLevels:  []slog.Level{slog.LevelInfo},
		},
		"http2 connection dropped before the context is cancelled": {
			writeErr:    errors.New("client disconnected"),
			wantHandled: true,
			wantStatus:  http.StatusNotFound,
			wantLevels:  []slog.Level{slog.LevelInfo},
		},
		"http1 connection closed by client": {
			writeErr:    &net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE},
			cancelCtx:   true,
			wantHandled: true,
			wantStatus:  http.StatusNotFound,
			wantLevels:  []slog.Level{slog.LevelInfo},
		},
		"http1 connection reset before the context is cancelled": {
			writeErr:    &net.OpError{Op: "write", Net: "tcp", Err: syscall.ECONNRESET},
			wantHandled: true,
			wantStatus:  http.StatusNotFound,
			wantLevels:  []slog.Level{slog.LevelInfo},
		},
		"read canceled": {
			readErr:     vfs.NewReadError(context.Canceled),
			wantHandled: true,
			wantStatus:  http.StatusNotFound,
			wantLevels:  []slog.Level{slog.LevelInfo},
		},
		"read failure": {
			readErr:     vfs.NewReadError(errors.New("boom")),
			wantHandled: true,
			wantStatus:  http.StatusNotFound,
			wantLevels:  []slog.Level{slog.LevelError},
		},
		"head request": {
			method:      http.MethodHead,
			writeErr:    errors.New("body must not be written"),
			wantHandled: true,
			wantStatus:  http.StatusNotFound,
		},
		"open failure before the headers are sent": {
			openErr:     errors.New("boom"),
			wantHandled: true,
			wantStatus:  http.StatusInternalServerError,
			wantLevels:  []slog.Level{slog.LevelError},
		},
		"open failure after the client left": {
			openErr:     errors.New("boom"),
			cancelCtx:   true,
			wantHandled: false,
			wantLevels:  []slog.Level{slog.LevelWarn},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := loggingtest.Record(t)
			reader := newDiskReader(t, dirRoot{openErr: tc.openErr, readErr: tc.readErr}, []byte("<h1>custom 404</h1>"))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelCtx {
				cancel()
			}

			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req, err := http.NewRequestWithContext(ctx, method, "https://domain.gitlab.io/missing", nil)
			require.NoError(t, err)
			w := &recordingWriter{ResponseWriter: httptest.NewRecorder(), writeErr: tc.writeErr}

			handled := reader.tryNotFound(serving.Handler{Writer: w, Request: req, LookupPath: &serving.LookupPath{}})

			require.Equal(t, tc.wantHandled, handled)
			require.Equal(t, tc.wantStatus, w.status)
			require.Equal(t, tc.wantLevels, levels(recorder))
			for _, record := range recorder.Records() {
				require.True(t, slices.ContainsFunc(record.Attrs, slog.String("host", "domain.gitlab.io").Equal))
			}
		})
	}
}

func TestReader_errorPageHeaders(t *testing.T) {
	gzipped := []byte("not really gzip but long enough to differ in length")
	errBoom := errors.New("boom")

	tests := map[string]struct {
		files    map[string][]byte
		openErrs map[string]error
		serve    func(reader *Reader, h serving.Handler) bool
		subPath  string
	}{
		"serveFile when opening the compressed file fails": {
			files:    map[string][]byte{"index.html": []byte("<h1>index</h1>"), "index.html.gz": gzipped},
			openErrs: map[string]error{"public/index.html.gz": errBoom},
			serve:    (*Reader).tryFile,
			subPath:  "index.html",
		},
		"serveFile when detecting the content type fails": {
			files:    map[string][]byte{"file": []byte("plain"), "file.gz": gzipped},
			openErrs: map[string]error{"public/file": errBoom},
			serve:    (*Reader).tryFile,
			subPath:  "file",
		},
		"serveCustomFile when opening the compressed file fails": {
			files:    map[string][]byte{"404.html": []byte("<h1>custom 404</h1>"), "404.html.gz": gzipped},
			openErrs: map[string]error{"public/404.html.gz": errBoom},
			serve:    (*Reader).tryNotFound,
			subPath:  "missing",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			loggingtest.Record(t)
			reader := newDiskReaderWithFiles(t, dirRoot{openErrs: tc.openErrs}, tc.files)

			req := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io/"+tc.subPath, nil)
			req.Header.Set("Accept-Encoding", "gzip")
			w := httptest.NewRecorder()

			handled := tc.serve(reader, serving.Handler{
				Writer:     w,
				Request:    req,
				LookupPath: &serving.LookupPath{SHA256: "sha"},
				SubPath:    tc.subPath,
			})

			require.True(t, handled)
			require.Equal(t, http.StatusInternalServerError, w.Code)
			require.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
			// Vary is set by serveFile/serveCustomFile before the lookup, so
			// it must be sent even on the error page.
			require.Equal(t, []string{"Accept-Encoding"}, w.Header().Values("Vary"))
			for _, header := range []string{"Content-Encoding", "Content-Length", "ETag", "Cache-Control", "Expires"} {
				require.Empty(t, w.Header().Values(header), header)
			}
		})
	}
}

func TestReader_serveFile_compressedHeaders(t *testing.T) {
	gzipped := []byte("not really gzip but long enough to differ in length")
	reader := newDiskReaderWithFiles(t, dirRoot{}, map[string][]byte{
		"index.html":    []byte("<h1>index</h1>"),
		"index.html.gz": gzipped,
	})

	req := httptest.NewRequest(http.MethodGet, "https://domain.gitlab.io/index.html", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handled := reader.tryFile(serving.Handler{
		Writer:     w,
		Request:    req,
		LookupPath: &serving.LookupPath{SHA256: "sha"},
		SubPath:    "index.html",
	})

	require.True(t, handled)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	require.Equal(t, []string{"Accept-Encoding"}, w.Header().Values("Vary"))
	require.Equal(t, strconv.Itoa(len(gzipped)), w.Header().Get("Content-Length"))
	require.Equal(t, `"sha-gzip"`, w.Header().Get("ETag"))
	require.Equal(t, "max-age=600", w.Header().Get("Cache-Control"))
	require.NotEmpty(t, w.Header().Get("Expires"))
	require.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, gzipped, w.Body.Bytes())
}

func Test_clientGone(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := map[string]struct {
		ctx  context.Context
		err  error
		want bool
	}{
		"request context canceled": {
			ctx:  canceled,
			err:  errors.New("http2: stream closed"),
			want: true,
		},
		"error wraps context canceled": {
			ctx:  context.Background(),
			err:  vfs.NewReadError(context.Canceled),
			want: true,
		},
		"server side failure": {
			ctx:  context.Background(),
			err:  vfs.NewReadError(errors.New("boom")),
			want: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, clientGone(tc.ctx, tc.err))
		})
	}
}

// TestReader_tryNotFound_http2ClientAbort drives tryNotFound through a real
// HTTP/2 server behind the access logger, the way the incident reached it:
// a client that resets the stream or drops the connection mid body must leave
// a 404 in the access log and one info record, never an error.
func TestReader_tryNotFound_http2ClientAbort(t *testing.T) {
	tests := map[string]struct {
		abort func(conn net.Conn, resp *http.Response)
	}{
		// Closing the body before EOF sends RST_STREAM; the server cancels the
		// stream context before it unblocks the handler's write.
		"stream reset": {
			abort: func(_ net.Conn, resp *http.Response) { _ = resp.Body.Close() },
		},
		// Dropping the connection unblocks the handler's write before the stream
		// context is cancelled, so only the failed write identifies the abort.
		"connection dropped": {
			abort: func(conn net.Conn, _ *http.Response) { _ = conn.Close() },
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := loggingtest.Record(t)
			// Larger than the client's stream window below, so the handler blocks
			// in Write until the client aborts.
			reader := newDiskReader(t, dirRoot{}, bytes.Repeat([]byte("x"), 512<<10))

			var handled bool
			done := make(chan struct{}, 1)
			notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handled = reader.tryNotFound(serving.Handler{Writer: w, Request: r, LookupPath: &serving.LookupPath{}})
			})
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				logging.BasicAccessLogger(notFound, "").ServeHTTP(w, r)
				done <- struct{}{}
			}))
			srv.EnableHTTP2 = true
			srv.StartTLS()
			t.Cleanup(srv.Close)

			var (
				mu   sync.Mutex
				conn net.Conn
			)
			transport := srv.Client().Transport.(*http.Transport)
			transport.HTTP2 = &http.HTTP2Config{MaxReceiveBufferPerStream: 64 << 10}
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
				if err == nil {
					mu.Lock()
					conn = c
					mu.Unlock()
				}

				return c, err
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/missing", nil)
			require.NoError(t, err)
			resp, err := srv.Client().Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, 2, resp.ProtoMajor)
			require.Equal(t, http.StatusNotFound, resp.StatusCode)
			_, err = io.ReadFull(resp.Body, make([]byte, 1024))
			require.NoError(t, err)

			mu.Lock()
			dialed := conn
			mu.Unlock()
			tc.abort(dialed, resp)

			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the handler did not return after the client aborted")
			}

			require.True(t, handled)
			records := recorder.Records()
			require.Len(t, records, 2)
			require.Equal(t, slog.LevelInfo, records[0].Level)
			require.Equal(t, "client did not receive custom 404 page", records[0].Message)
			require.Equal(t, "access", records[1].Message)
			require.Equal(t, int64(http.StatusNotFound), attr(records[1].Attrs, "status").Int64())
		})
	}
}

// attr returns the value of the first attribute with the given key.
func attr(attrs []slog.Attr, key string) slog.Value {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value
		}
	}

	return slog.Value{}
}

// newDiskReader returns a Reader that serves a directory holding public/404.html
// with the given body through the dirVFS test double.
func newDiskReader(t *testing.T, root dirRoot, page404 []byte) *Reader {
	t.Helper()

	return newDiskReaderWithFiles(t, root, map[string][]byte{"404.html": page404})
}

func newDiskReaderWithFiles(t *testing.T, root dirRoot, files map[string][]byte) *Reader {
	t.Helper()

	root.dir = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root.dir, "public"), 0o755))
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root.dir, "public", name), body, 0o644))
	}

	return &Reader{
		fileSizeMetric: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "file_size"}, []string{"vfs_name"}),
		vfs:            dirVFS{root: root},
	}
}

// levels returns the level of every record captured so far, in order.
func levels(recorder *logtest.LogRecorder) []slog.Level {
	var levels []slog.Level
	for _, record := range recorder.Records() {
		levels = append(levels, record.Level)
	}

	return levels
}

// recordingWriter keeps the last status written, like the access log and
// metrics middlewares do, and optionally fails every body write.
type recordingWriter struct {
	http.ResponseWriter
	writeErr error
	status   int
}

func (w *recordingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}

	return w.ResponseWriter.Write(b)
}

type dirVFS struct {
	root dirRoot
}

func (v dirVFS) Root(context.Context, string, string) (vfs.Root, error) { return v.root, nil }
func (v dirVFS) Name() string                                           { return "dir" }
func (v dirVFS) Reconfigure(*config.Config) error                       { return nil }
func (v dirVFS) Stop()                                                  {}

type dirRoot struct {
	dir     string
	openErr error
	readErr error
	// openErrs fails Open for the given paths while Lstat still succeeds.
	openErrs map[string]error
}

func (r dirRoot) Lstat(_ context.Context, name string) (os.FileInfo, error) {
	return os.Lstat(filepath.Join(r.dir, name))
}

func (r dirRoot) Readlink(_ context.Context, name string) (string, error) {
	return os.Readlink(filepath.Join(r.dir, name))
}

func (r dirRoot) Open(_ context.Context, name string) (vfs.File, error) {
	if r.openErr != nil {
		return nil, r.openErr
	}

	if err := r.openErrs[name]; err != nil {
		return nil, err
	}

	if r.readErr != nil {
		return io.NopCloser(errReader{err: r.readErr}), nil
	}

	return os.Open(filepath.Join(r.dir, name))
}

type errReader struct {
	err error
}

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
