package artifact_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/artifact"
	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
)

func TestTryMakeRequest(t *testing.T) {
	content := "<!DOCTYPE html><html><head><title>Title of the document</title></head><body></body></html>"
	contentType := "text/html; charset=utf-8"

	cases := []struct {
		Path                   string
		Token                  string
		Status                 int
		Content                string
		APIUsesChunkedEncoding bool
		Length                 string
		CacheControl           string
		ContentType            string
		ContentEncoding        string
		AcceptEncoding         string
		Description            string
		RemoteAddr             string
		ForwardedIP            string
	}{
		{
			"/200.html",
			"",
			http.StatusOK,
			content,
			false,
			"90",
			"max-age=3600",
			"text/html; charset=utf-8",
			"",
			"",
			"basic successful request",
			"1.2.3.4:8000",
			"1.2.3.4",
		},
		{
			"/200.html",
			"token",
			http.StatusOK,
			content,
			false,
			"90",
			"",
			"text/html; charset=utf-8",
			"",
			"",
			"basic successful request",
			"1.2.3.4",
			"1.2.3.4",
		},
		{
			"/max-caching.html",
			"",
			http.StatusIMUsed,
			content,
			false,
			"90",
			"max-age=3600",
			"text/html; charset=utf-8",
			"",
			"",
			"max caching request",
			"1.2.3.4",
			"1.2.3.4",
		},
		{
			"/non-caching.html",
			"",
			http.StatusTeapot,
			content,
			false,
			"90",
			"",
			"text/html; charset=utf-8",
			"",
			"",
			"no caching request",
			"1.2.3.4",
			"1.2.3.4",
		},
		{
			"/200.html",
			"",
			http.StatusOK,
			content,
			true,
			"",
			"max-age=3600",
			"text/html; charset=utf-8",
			"",
			"",
			"basic successful request",
			"1.2.3.4:8000",
			"1.2.3.4",
		},
		{
			"/gzip-encoded.html",
			"",
			http.StatusOK,
			content,
			false,
			"90",
			"max-age=3600",
			"text/html; charset=utf-8",
			"gzip",
			"gzip, deflate",
			"gzip encoded response",
			"1.2.3.4",
			"1.2.3.4",
		},
		{
			"/deflate-encoded.html",
			"",
			http.StatusOK,
			content,
			false,
			"90",
			"max-age=3600",
			"text/html; charset=utf-8",
			"deflate",
			"gzip, deflate, br",
			"deflate encoded response",
			"1.2.3.4",
			"1.2.3.4",
		},
	}

	for _, c := range cases {
		t.Run(c.Description, func(t *testing.T) {
			testServer := makeArtifactServerStub(t, content, contentType, c.ForwardedIP, c.APIUsesChunkedEncoding, c.ContentEncoding, c.AcceptEncoding)
			defer testServer.Close()

			url := "https://group.gitlab-example.io/-/subgroup/project/-/jobs/1/artifacts" + c.Path
			r, err := http.NewRequest(http.MethodGet, url, nil)
			require.NoError(t, err)

			r.RemoteAddr = c.RemoteAddr
			if c.AcceptEncoding != "" {
				r.Header.Set("Accept-Encoding", c.AcceptEncoding)
			}

			art := artifact.New(testServer.URL, 1, "gitlab-example.io", config.HTTPClientCfg{})

			result := httptest.NewRecorder()

			require.True(t, art.TryMakeRequest(result, r, c.Token, func(resp *http.Response) bool { return false }))
			require.Equal(t, c.Status, result.Code)
			require.Equal(t, c.ContentType, result.Header().Get("Content-Type"))
			require.Equal(t, c.Length, result.Header().Get("Content-Length"))
			require.Equal(t, c.CacheControl, result.Header().Get("Cache-Control"))
			if c.ContentEncoding == "" {
				require.Equal(t, 0, len(result.Header().Values("Content-Encoding")))
			}
			require.Equal(t, c.ContentEncoding, result.Header().Get("Content-Encoding"))
			require.Equal(t, []string{"Accept-Encoding"}, result.Header().Values("Vary"))
			require.Equal(t, c.Content, result.Body.String())
		})
	}
}

// provide stub for testing different artifact responses
func makeArtifactServerStub(t *testing.T, content string, contentType string,
	expectedForwardedIP string, chunkedEncoding bool, contentEncoding string, expectedAcceptEncoding string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, expectedForwardedIP, r.Header.Get("X-Forwarded-For"))
		if expectedAcceptEncoding != "" {
			require.Equal(t, expectedAcceptEncoding, r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Type", contentType)
		if contentEncoding != "" {
			w.Header().Set("Content-Encoding", contentEncoding)
		}

		statusCode := getStatusCodeForPath(t, r.URL.RawPath)
		w.WriteHeader(statusCode)

		fmt.Fprint(w, content)
		if chunkedEncoding {
			// this forces go to use chunked encoding
			// and omitting the "Content-Length" Header
			w.(http.Flusher).Flush()
		}
	}))
}

func getStatusCodeForPath(t *testing.T, rawPath string) int {
	switch rawPath {
	case "/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/200.html":
		return http.StatusOK
	case "/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/max-caching.html":
		return http.StatusIMUsed
	case "/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/non-caching.html":
		return http.StatusTeapot
	case "/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/gzip-encoded.html":
		return http.StatusOK
	case "/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/deflate-encoded.html":
		return http.StatusOK
	case "/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/500.html":
		return http.StatusInternalServerError
	case "/projects/group%2Fsubgroup%2Fgroup%2Fproject/jobs/1/artifacts/404.html":
		return http.StatusNotFound
	default:
		t.Log("Surprising r.URL.RawPath", rawPath)
		return 999
	}
}

func TestBuildURL(t *testing.T) {
	cases := []struct {
		RawServer   string
		Host        string
		Path        string
		Expected    string
		PagesDomain string
		Ok          bool
		Description string
	}{
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1/artifacts/",
			`https://gitlab.com/api/v4/projects/group%2Fproject/jobs/1/artifacts/`,
			"gitlab.io",
			true,
			"Basic case",
		},
		{
			"https://gitlab.com/api/v4/",
			"group.gitlab.io",
			"/-/project/-/jobs/1/artifacts/",
			"https://gitlab.com/api/v4/projects/group%2Fproject/jobs/1/artifacts/",
			"gitlab.io",
			true,
			"API URL has trailing slash",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1/artifacts/path/to/file {!#1.txt",
			"https://gitlab.com/api/v4/projects/group%2Fproject/jobs/1/artifacts/path/to/file%20%7B%21%231.txt",
			"gitlab.io",
			true,
			"Special characters in name",
		},
		{
			"https://gitlab.com/api/v4/",
			"GROUP.GITLAB.IO",
			"/-/SUBGROUP/PROJECT/-/JOBS/1/ARTIFACTS/PATH/TO/FILE.txt",
			"https://gitlab.com/api/v4/projects/GROUP%2FSUBGROUP%2FPROJECT/jobs/1/artifacts/PATH/TO/FILE.txt",
			"gitlab.io",
			true,
			"Uppercase names",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1foo1/artifacts/",
			"",
			"gitlab.io",
			false,
			"Job ID has letters",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1$1/artifacts/",
			"",
			"gitlab.io",
			false,
			"Job ID has special characters",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1/artifacts/path/to/file.txt",
			"https://gitlab.com/api/v4/projects/group%2Fproject/jobs/1/artifacts/path/to/file.txt",
			"gitlab.io",
			true,
			"Artifact in subdirectory",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/subgroup1/sub.group2/project/-/jobs/1/artifacts/path/to/file.txt",
			"https://gitlab.com/api/v4/projects/group%2Fsubgroup1%2Fsub.group2%2Fproject/jobs/1/artifacts/path/to/file.txt",
			"gitlab.io",
			true,
			"Basic subgroup case",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-//project/-/jobs/1/artifacts/",
			"https://gitlab.com/api/v4/projects/group%2Fproject/jobs/1/artifacts/",
			"gitlab.io",
			true,
			"Leading / in remainder of project path",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/subgroup/project//-/jobs/1/artifacts/",
			"https://gitlab.com/api/v4/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/",
			"gitlab.io",
			true,
			"Trailing / in remainder of project path",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-//subgroup/project//-/jobs/1/artifacts/",
			"https://gitlab.com/api/v4/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/",
			"gitlab.io",
			true,
			"Leading and trailing /",
		},
		{
			"https://gitlab.com/api/v4",
			"group.name.gitlab.io",
			"/-/subgroup/project/-/jobs/1/artifacts/",
			"https://gitlab.com/api/v4/projects/group.name%2Fsubgroup%2Fproject/jobs/1/artifacts/",
			"gitlab.io",
			true,
			"Toplevel group has period",
		},
		{
			"https://gitlab.com/api/v4",
			"gitlab.io.gitlab.io",
			"/-/project/-/jobs/1/artifacts/",
			"https://gitlab.com/api/v4/projects/gitlab.io%2Fproject/jobs/1/artifacts/",
			"gitlab.io",
			true,
			"Toplevel group matches pages domain",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1/artifacts",
			"",
			"gitlab.io",
			false,
			"No artifact specified",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/index.html",
			"",
			"example.com",
			false,
			"non matching domain and request",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/../victim/project/-/jobs/1/artifacts/secrets.txt",
			"",
			"gitlab.io",
			false,
			"parent segment in project path escapes the top-level group",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/subgroup/../../victim/project/-/jobs/1/artifacts/secrets.txt",
			"",
			"gitlab.io",
			false,
			"parent segments in the middle of the project path",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/../-/jobs/1/artifacts/secrets.txt",
			"",
			"gitlab.io",
			false,
			"project path is a single parent segment",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/./project/-/jobs/1/artifacts/secrets.txt",
			"",
			"gitlab.io",
			false,
			"current segment in project path",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1/artifacts/../../../../user",
			"",
			"gitlab.io",
			false,
			"parent segments in artifact path escape the artifacts endpoint",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1/artifacts/path/./file.txt",
			"",
			"gitlab.io",
			false,
			"current segment in artifact path",
		},
		{
			"https://gitlab.com/api/v4",
			".gitlab.io",
			"/-/victim/project/-/jobs/1/artifacts/secrets.txt",
			"",
			"gitlab.io",
			false,
			"empty top-level group",
		},
		{
			"https://gitlab.com/api/v4",
			"...gitlab.io",
			"/-/victim/project/-/jobs/1/artifacts/secrets.txt",
			"",
			"gitlab.io",
			false,
			"top-level group is a parent segment",
		},
		{
			"https://gitlab.com/api/v4",
			"..gitlab.io",
			"/-/victim/project/-/jobs/1/artifacts/secrets.txt",
			"",
			"gitlab.io",
			false,
			"top-level group is a current segment",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/..project/-/jobs/1/artifacts/secrets.txt",
			"https://gitlab.com/api/v4/projects/group%2F..project/jobs/1/artifacts/secrets.txt",
			"gitlab.io",
			true,
			"leading dots in a project segment are not parent segments",
		},
		{
			"https://gitlab.com/api/v4",
			"group.gitlab.io",
			"/-/project/-/jobs/1/artifacts/..file.txt",
			"https://gitlab.com/api/v4/projects/group%2Fproject/jobs/1/artifacts/..file.txt",
			"gitlab.io",
			true,
			"leading dots in an artifact segment are not parent segments",
		},
	}

	for _, c := range cases {
		t.Run(c.Description, func(t *testing.T) {
			a := artifact.New(c.RawServer, 1, c.PagesDomain, config.HTTPClientCfg{})
			u, ok := a.BuildURL(c.Host, c.Path)

			msg := c.Description + " - generated URL: "
			if u != nil {
				msg = msg + u.String()
			}

			require.Equal(t, c.Ok, ok, msg)
			if c.Ok {
				require.Equal(t, c.Expected, u.String(), c.Description)
			}
		})
	}
}

func TestContextCanceled(t *testing.T) {
	content := "<!DOCTYPE html><html><head><title>Title of the document</title></head><body></body></html>"
	contentType := "text/html; charset=utf-8"
	testServer := makeArtifactServerStub(t, content, contentType, "", false, "", "")
	t.Cleanup(testServer.Close)

	result := httptest.NewRecorder()
	url := "https://group.gitlab-example.io/-/subgroup/project/-/jobs/1/artifacts/200.html"
	r, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	r = r.WithContext(ctx)
	// cancel context explicitly
	cancel()
	art := artifact.New(testServer.URL, 1, "gitlab-example.io", config.HTTPClientCfg{})

	require.True(t, art.TryMakeRequest(result, r, "", func(resp *http.Response) bool { return false }))
	require.Equal(t, http.StatusNotFound, result.Code)
}
