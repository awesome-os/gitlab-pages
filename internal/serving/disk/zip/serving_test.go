package zip

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/confighelper"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func TestZip_ServeFileHTTP(t *testing.T) {
	testServerURL, cleanup := newZipFileServerURL(t, "group/zip.gitlab.io/public-without-dirs.zip")
	defer cleanup()

	wd, err := os.Getwd()
	require.NoError(t, err)

	httpURL := testServerURL + "/public.zip"
	fileURL := "file://" + wd + "/group/zip.gitlab.io/public-without-dirs.zip"

	tests := map[string]struct {
		vfsPath        string
		path           string
		expectedStatus int
		expectedBody   string
		extraHeaders   http.Header
	}{
		"accessing /index.html": {
			vfsPath:        httpURL,
			path:           "/index.html",
			expectedStatus: http.StatusOK,
			expectedBody:   "zip.gitlab.io/project/index.html\n",
		},
		"accessing /index.html from disk": {
			vfsPath:        fileURL,
			path:           "/index.html",
			expectedStatus: http.StatusOK,
			expectedBody:   "zip.gitlab.io/project/index.html\n",
		},
		"accessing /": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "zip.gitlab.io/project/index.html\n",
		},
		"accessing / If-Modified-Since": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusNotModified,
			extraHeaders: http.Header{
				"If-Modified-Since": {time.Now().Format(http.TimeFormat)},
			},
		},
		"accessing / If-Modified-Since fails": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "zip.gitlab.io/project/index.html\n",
			extraHeaders: http.Header{
				"If-Modified-Since": {time.Now().AddDate(-10, 0, 0).Format(http.TimeFormat)},
			},
		},
		"accessing / If-Unmodified-Since": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusPreconditionFailed,
			extraHeaders: http.Header{
				"If-Unmodified-Since": {time.Now().AddDate(-10, 0, 0).Format(http.TimeFormat)},
			},
		},
		"accessing / If-Unmodified-Since fails": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "zip.gitlab.io/project/index.html\n",
			extraHeaders: http.Header{
				"If-Unmodified-Since": {time.Now().Format(http.TimeFormat)},
			},
		},
		"accessing / from disk": {
			vfsPath:        fileURL,
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "zip.gitlab.io/project/index.html\n",
		},
		"accessing without /": {
			vfsPath:        httpURL,
			path:           "",
			expectedStatus: http.StatusFound,
			expectedBody:   "<a href=\"//zip.gitlab.io/zip/\">Found</a>.\n\n",
		},
		"accessing without / from disk": {
			vfsPath:        fileURL,
			path:           "",
			expectedStatus: http.StatusFound,
			expectedBody:   "<a href=\"//zip.gitlab.io/zip/\">Found</a>.\n\n",
		},
		"accessing archive that is 404": {
			vfsPath: testServerURL + "/invalid.zip",
			path:    "/index.html",
			// we expect the status to not be set
			expectedStatus: 0,
		},
		"accessing archive that is 500": {
			vfsPath:        testServerURL + "/500",
			path:           "/index.html",
			expectedStatus: http.StatusInternalServerError,
		},
		"accessing file:// outside of allowedPaths": {
			vfsPath:        "file:///some/file/outside/path",
			path:           "/index.html",
			expectedStatus: http.StatusInternalServerError,
		},
		"accessing / If-None-Match": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusNotModified,
			extraHeaders: http.Header{
				"If-None-Match": {fmt.Sprintf("%q", testhelpers.Sha(httpURL))},
			},
		},
		"accessing / If-None-Match fails": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "zip.gitlab.io/project/index.html\n",
			extraHeaders: http.Header{
				"If-None-Match": {fmt.Sprintf("%q", "badetag")},
			},
		},
		"accessing / If-Match": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "zip.gitlab.io/project/index.html\n",
			extraHeaders: http.Header{
				"If-Match": {fmt.Sprintf("%q", testhelpers.Sha(httpURL))},
			},
		},
		"accessing / If-Match fails": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusPreconditionFailed,
			extraHeaders: http.Header{
				"If-Match": {fmt.Sprintf("%q", "wrongetag")},
			},
		},
		"accessing / If-Match fails2": {
			vfsPath:        httpURL,
			path:           "/",
			expectedStatus: http.StatusPreconditionFailed,
			extraHeaders: http.Header{
				"If-Match": {","},
			},
		},
	}

	s := NewInstance()
	defer s.Stop()

	zipCfg := confighelper.DefaultZipCfgForTest
	zipCfg.AllowedPaths = []string{wd}
	cfg := &config.Config{
		Zip: zipCfg,
	}

	// This is required to register file transport protocol
	err = s.Reconfigure(cfg)
	require.NoError(t, err)

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			w.Code = 0 // ensure that code is not set, and it is being set by handler
			r := httptest.NewRequest(http.MethodGet, "http://zip.gitlab.io/zip"+test.path, nil)

			if test.extraHeaders != nil {
				r.Header = test.extraHeaders
			}

			handler := serving.Handler{
				Writer:  w,
				Request: r,
				LookupPath: &serving.LookupPath{
					Prefix: "/zip/",
					Path:   test.vfsPath,
					SHA256: testhelpers.Sha(test.vfsPath),
				},
				SubPath: test.path,
			}

			if test.expectedStatus == 0 {
				require.False(t, s.ServeFileHTTP(handler))
				require.Zero(t, w.Code, "we expect status to not be set")
				return
			}

			require.True(t, s.ServeFileHTTP(handler))

			resp := w.Result()
			testhelpers.Close(t, resp.Body)

			require.Equal(t, test.expectedStatus, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			if test.expectedStatus == http.StatusOK {
				require.NotEmpty(t, resp.Header.Get("Last-Modified"))
				require.NotEmpty(t, resp.Header.Get("ETag"))
			}

			if test.expectedStatus != http.StatusInternalServerError {
				require.Equal(t, test.expectedBody, string(body))
			}
		})
	}
}

var chdirSet = false

func newZipFileServerURL(t *testing.T, zipFilePath string) (string, func()) {
	t.Helper()

	chdir := testhelpers.ChdirInPath(t, "../../../../shared/pages", &chdirSet)

	m := http.NewServeMux()
	m.HandleFunc("/public.zip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, zipFilePath)
	}))
	m.HandleFunc("/500", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	testServer := httptest.NewServer(m)

	return testServer.URL, func() {
		chdir()
		testServer.Close()
	}
}

func TestZip_ServeFileHTTP_VaryAcceptEncoding(t *testing.T) {
	testServerURL, cleanup := newZipFileServerURL(t, "group/group.gitlab-example.com/public.zip")
	defer cleanup()

	s := NewInstance()
	defer s.Stop()
	require.NoError(t, s.Reconfigure(&config.Config{Zip: confighelper.DefaultZipCfgForTest}))

	vfsPath := testServerURL + "/public.zip"

	tests := map[string]struct {
		acceptEncoding          string
		rangeHeader             string
		expectedStatus          int
		expectedContentEncoding string
	}{
		"gzip": {
			acceptEncoding:          "gzip",
			expectedStatus:          http.StatusOK,
			expectedContentEncoding: "gzip",
		},
		"brotli": {
			acceptEncoding:          "br",
			expectedStatus:          http.StatusOK,
			expectedContentEncoding: "br",
		},
		"no Accept-Encoding": {
			expectedStatus: http.StatusOK,
		},
		"range request with gzip accepted": {
			acceptEncoding: "gzip",
			rangeHeader:    "bytes=0-3",
			expectedStatus: http.StatusPartialContent,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "http://group.gitlab-example.com/index.html", nil)
			if test.acceptEncoding != "" {
				r.Header.Set("Accept-Encoding", test.acceptEncoding)
			}
			if test.rangeHeader != "" {
				r.Header.Set("Range", test.rangeHeader)
			}

			handler := serving.Handler{
				Writer:  w,
				Request: r,
				LookupPath: &serving.LookupPath{
					Prefix: "/",
					Path:   vfsPath,
					SHA256: testhelpers.Sha(vfsPath),
				},
				SubPath: "index.html",
			}

			require.True(t, s.ServeFileHTTP(handler))

			resp := w.Result()
			testhelpers.Close(t, resp.Body)

			require.Equal(t, test.expectedStatus, resp.StatusCode)
			require.Equal(t, test.expectedContentEncoding, resp.Header.Get("Content-Encoding"))
			require.Equal(t, []string{"Accept-Encoding"}, resp.Header.Values("Vary"))
		})
	}
}

func TestZip_ServeNotFoundHTTP_VaryAcceptEncoding(t *testing.T) {
	testServerURL, cleanup := newZipFileServerURL(t, "group/zip.gitlab.io/public-without-dirs.zip")
	defer cleanup()

	s := NewInstance()
	defer s.Stop()
	require.NoError(t, s.Reconfigure(&config.Config{Zip: confighelper.DefaultZipCfgForTest}))

	vfsPath := testServerURL + "/public.zip"

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://zip.gitlab.io/missing.html", nil)
	r.Header.Set("Accept-Encoding", "gzip")

	handler := serving.Handler{
		Writer:  w,
		Request: r,
		LookupPath: &serving.LookupPath{
			Prefix: "/",
			Path:   vfsPath,
			SHA256: testhelpers.Sha(vfsPath),
		},
		SubPath: "missing.html",
	}

	s.ServeNotFoundHTTP(handler)

	resp := w.Result()
	testhelpers.Close(t, resp.Body)

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, []string{"Accept-Encoding"}, resp.Header.Values("Vary"))
}
