package acceptance_test

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
	"gitlab.com/gitlab-org/gitlab-pages/test/gitlabstub"
)

type testCase struct {
	name            string
	host            string
	path            string
	status          int
	content         string
	length          int64
	cacheControl    string
	contentType     string
	contentEncoding string
	acceptEncoding  string
}

func TestArtifactProxyRequest(t *testing.T) {
	content := "<!DOCTYPE html><html><head><title>Title of the document</title></head><body></body></html>"
	contentLength := int64(len(content))

	testServer := createArtifactTestServer(t, content)

	tests := createArtifactProxyTestCases(content, contentLength)

	runArtifactProxyTests(t, testServer, tests)
}

// createArtifactTestServer creates and starts a test server for artifact proxy testing
func createArtifactTestServer(t *testing.T, content string) *httptest.Server {
	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleArtifactServerRequest(t, w, r, content)
	}))

	keyFile, certFile := CreateHTTPSFixtureFiles(t)
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	testServer.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	testServer.StartTLS()

	t.Cleanup(func() {
		testServer.Close()
	})

	return testServer
}

// handleArtifactServerRequest handles requests to the artifact test server
func handleArtifactServerRequest(t *testing.T, w http.ResponseWriter, r *http.Request, content string) {
	switch r.URL.RawPath {
	case "/api/v4/projects/group%2Fproject/jobs/1/artifacts/delayed_200.html":
		time.Sleep(2 * time.Second)
		fallthrough
	case "/api/v4/projects/group%2Fproject/jobs/1/artifacts/200.html",
		"/api/v4/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/200.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, content)
	case "/api/v4/projects/group%2Fproject/jobs/1/artifacts/gzip-encoded.html":
		handleEncodedResponse(w, r, content, "gzip", "gzip, deflate")
	case "/api/v4/projects/group%2Fproject/jobs/1/artifacts/deflate-encoded.html":
		handleEncodedResponse(w, r, content, "deflate", "gzip, deflate, br")
	case "/api/v4/projects/group%2Fproject/jobs/1/artifacts/500.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, content)
	default:
		t.Logf("Unexpected r.URL.RawPath: %q", r.URL.RawPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, content)
	}
}

// handleEncodedResponse handles encoded responses for the test server
func handleEncodedResponse(w http.ResponseWriter, r *http.Request, content, encoding, expectedAcceptEncoding string) {
	acceptEncoding := r.Header.Get("Accept-Encoding")
	if acceptEncoding == expectedAcceptEncoding {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", encoding)
		fmt.Fprint(w, content)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}
}

// createArtifactProxyTestCases creates test cases for artifact proxy testing
func createArtifactProxyTestCases(content string, contentLength int64) []testCase {
	return []testCase{
		{
			name:         "basic proxied request",
			host:         "group.gitlab-example.com",
			path:         "/-/project/-/jobs/1/artifacts/200.html",
			status:       http.StatusOK,
			content:      content,
			length:       contentLength,
			cacheControl: "max-age=3600",
			contentType:  "text/html; charset=utf-8",
		},
		{
			name:         "basic proxied request for subgroup",
			host:         "group.gitlab-example.com",
			path:         "/-/subgroup/project/-/jobs/1/artifacts/200.html",
			status:       http.StatusOK,
			content:      content,
			length:       contentLength,
			cacheControl: "max-age=3600",
			contentType:  "text/html; charset=utf-8",
		},
		{
			name:            "gzip encoded response",
			host:            "group.gitlab-example.com",
			path:            "/-/project/-/jobs/1/artifacts/gzip-encoded.html",
			status:          http.StatusOK,
			content:         content,
			length:          contentLength,
			cacheControl:    "max-age=3600",
			contentType:     "text/html; charset=utf-8",
			contentEncoding: "gzip",
			acceptEncoding:  "gzip, deflate",
		},
		{
			name:            "deflate encoded response",
			host:            "group.gitlab-example.com",
			path:            "/-/project/-/jobs/1/artifacts/deflate-encoded.html",
			status:          http.StatusOK,
			content:         content,
			length:          contentLength,
			cacheControl:    "max-age=3600",
			contentType:     "text/html; charset=utf-8",
			contentEncoding: "deflate",
			acceptEncoding:  "gzip, deflate, br",
		},
		{
			name:         "502 error while attempting to proxy",
			host:         "group.gitlab-example.com",
			path:         "/-/project/-/jobs/1/artifacts/delayed_200.html",
			status:       http.StatusBadGateway,
			content:      "",
			length:       0,
			cacheControl: "",
			contentType:  "text/html; charset=utf-8",
		},
		{
			name:         "Proxying 404 from server",
			host:         "group.gitlab-example.com",
			path:         "/-/project/-/jobs/1/artifacts/404.html",
			status:       http.StatusNotFound,
			content:      "",
			length:       0,
			cacheControl: "",
			contentType:  "text/html; charset=utf-8",
		},
		{
			name:         "Proxying 500 from server",
			host:         "group.gitlab-example.com",
			path:         "/-/project/-/jobs/1/artifacts/500.html",
			status:       http.StatusInternalServerError,
			content:      "",
			length:       0,
			cacheControl: "",
			contentType:  "text/html; charset=utf-8",
		},
	}
}

// runArtifactProxyTests runs the artifact proxy tests with the given test server and test cases
func runArtifactProxyTests(t *testing.T, testServer *httptest.Server, tests []testCase) {
	// Ensure the IP address is used in the URL, as we're relying on IP SANs to
	// validate
	artifactServerURL := testServer.URL + "/api/v4"
	t.Log("Artifact server URL", artifactServerURL)

	args := []string{"-artifacts-server=" + artifactServerURL, "-artifacts-server-timeout=1"}

	_, certFile := CreateHTTPSFixtureFiles(t)
	t.Setenv("SSL_CERT_FILE", certFile)

	RunPagesProcess(t,
		withListeners([]ListenSpec{httpListener}),
		withArguments(args),
	)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validateArtifactProxyResponse(t, tt)
		})
	}
}

// validateArtifactProxyResponse validates the response from an artifact proxy request
func validateArtifactProxyResponse(t *testing.T, tt testCase) {
	var resp *http.Response
	var err error

	if tt.acceptEncoding != "" {
		header := http.Header{}
		header.Set("Accept-Encoding", tt.acceptEncoding)
		resp, err = GetPageFromListenerWithHeaders(t, httpListener, tt.host, tt.path, header)
	} else {
		resp, err = GetPageFromListener(t, httpListener, tt.host, tt.path)
	}

	require.NoError(t, err)
	testhelpers.Close(t, resp.Body)

	require.Equal(t, tt.status, resp.StatusCode)
	require.Equal(t, tt.contentType, resp.Header.Get("Content-Type"))
	require.Equal(t, tt.contentEncoding, resp.Header.Get("Content-Encoding"))

	if tt.status == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, tt.content, string(body))
		require.Equal(t, tt.length, resp.ContentLength)
		require.Equal(t, tt.cacheControl, resp.Header.Get("Cache-Control"))
		// The CORS middleware adds Origin, which must be kept.
		require.ElementsMatch(t, []string{"Origin", "Accept-Encoding"}, resp.Header.Values("Vary"))
	}
}

func TestPrivateArtifactProxyRequest(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		path   string
		status int
	}{
		{
			name:   "basic proxied request for private project",
			host:   "group.gitlab-example.com",
			path:   "/-/private/-/jobs/1/artifacts/200.html",
			status: http.StatusOK,
		},
		{
			name:   "basic proxied request for subgroup",
			host:   "group.gitlab-example.com",
			path:   "/-/subgroup/private/-/jobs/1/artifacts/200.html",
			status: http.StatusOK,
		},
		{
			name:   "502 error while attempting to proxy",
			host:   "group.gitlab-example.com",
			path:   "/-/private/-/jobs/1/artifacts/delayed_200.html",
			status: http.StatusBadGateway,
		},
		{
			name:   "Proxying 404 from server",
			host:   "group.gitlab-example.com",
			path:   "/-/private/-/jobs/1/artifacts/404.html",
			status: http.StatusNotFound,
		},
		{
			name:   "Proxying 500 from server",
			host:   "group.gitlab-example.com",
			path:   "/-/private/-/jobs/1/artifacts/500.html",
			status: http.StatusInternalServerError,
		},
	}

	configFile := defaultConfigFileWith(t)

	keyFile, certFile := CreateHTTPSFixtureFiles(t)
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	t.Setenv("SSL_CERT_FILE", certFile)

	RunPagesProcess(t,
		withListeners([]ListenSpec{httpsListener}),
		withArguments([]string{
			"-config=" + configFile,
		}),
		withPublicServer,
		withExtraArgument("auth-redirect-uri", "https://projects.gitlab-example.com/auth"),
		withExtraArgument("artifacts-server-timeout", "1"),
		withStubOptions(gitlabstub.WithCertificate(cert)),
	)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp, err := GetRedirectPage(t, httpsListener, tt.host, tt.path)
			require.NoError(t, err)
			testhelpers.Close(t, resp.Body)

			require.Equal(t, http.StatusFound, resp.StatusCode)

			cookie := resp.Header.Get("Set-Cookie")

			// Redirects to the projects under gitlab pages domain for authentication flow
			url, err := url.Parse(resp.Header.Get("Location"))
			require.NoError(t, err)
			require.Equal(t, "projects.gitlab-example.com", url.Host)
			require.Equal(t, "/auth", url.Path)
			state := url.Query().Get("state")

			resp, err = GetRedirectPage(t, httpsListener, url.Host, url.Path+"?"+url.RawQuery)

			require.NoError(t, err)
			testhelpers.Close(t, resp.Body)

			require.Equal(t, http.StatusFound, resp.StatusCode)
			pagesDomainCookie := resp.Header.Get("Set-Cookie")

			// Go to auth page with correct state will cause fetching the token
			authrsp, err := GetRedirectPageWithCookie(t, httpsListener, "projects.gitlab-example.com", "/auth?code=1&state="+
				state, pagesDomainCookie)

			require.NoError(t, err)
			testhelpers.Close(t, authrsp.Body)

			// Will redirect auth callback to correct host
			url, err = url.Parse(authrsp.Header.Get("Location"))
			require.NoError(t, err)
			require.Equal(t, tt.host, url.Host)
			require.Equal(t, "/auth", url.Path)

			// Request auth callback in project domain
			authrsp, err = GetRedirectPageWithCookie(t, httpsListener, url.Host, url.Path+"?"+url.RawQuery, cookie)
			require.NoError(t, err)
			testhelpers.Close(t, authrsp.Body)

			// server returns the ticket, user will be redirected to the project page
			require.Equal(t, http.StatusFound, authrsp.StatusCode)
			cookie = authrsp.Header.Get("Set-Cookie")
			resp, err = GetRedirectPageWithCookie(t, httpsListener, tt.host, tt.path, cookie)

			require.Equal(t, tt.status, resp.StatusCode)

			require.NoError(t, err)
			testhelpers.Close(t, resp.Body)
		})
	}
}

func TestArtifactProxyPathTraversal(t *testing.T) {
	const upstreamContent = "artifact content that must not leak"

	var mu sync.Mutex
	var requested []string

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.RequestURI)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, upstreamContent)
	}))

	keyFile, certFile := CreateHTTPSFixtureFiles(t)
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	testServer.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	testServer.StartTLS()
	t.Cleanup(testServer.Close)

	t.Setenv("SSL_CERT_FILE", certFile)

	RunPagesProcess(t,
		withListeners([]ListenSpec{httpListener}),
		withArguments([]string{
			"-artifacts-server=" + testServer.URL + "/api/v4",
			"-artifacts-server-timeout=1",
		}),
	)

	takeRequested := func() []string {
		mu.Lock()
		defer mu.Unlock()

		taken := requested
		requested = nil

		return taken
	}

	t.Run("a request within the top-level group reaches the artifacts server", func(t *testing.T) {
		resp, err := GetPageFromListener(t, httpListener, "group.gitlab-example.com", "/-/project/-/jobs/1/artifacts/200.html")
		require.NoError(t, err)
		testhelpers.Close(t, resp.Body)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, upstreamContent, string(body))
		require.Equal(t,
			[]string{"/api/v4/projects/group%2Fproject/jobs/1/artifacts/200.html"},
			takeRequested())
	})

	tests := []struct {
		name string
		host string
		path string
	}{
		{
			name: "encoded parent segment in the project path",
			host: "group.gitlab-example.com",
			path: "/-/..%2Fvictim%2Fproject/-/jobs/1/artifacts/200.html",
		},
		{
			name: "parent segment in the project path",
			host: "group.gitlab-example.com",
			path: "/-/../victim/project/-/jobs/1/artifacts/200.html",
		},
		{
			name: "parent segments in the middle of the project path",
			host: "group.gitlab-example.com",
			path: "/-/subgroup%2F..%2F..%2Fvictim%2Fproject/-/jobs/1/artifacts/200.html",
		},
		{
			name: "parent segments in the artifact path",
			host: "group.gitlab-example.com",
			path: "/-/project/-/jobs/1/artifacts/../../../../user",
		},
		{
			name: "encoded parent segments in the artifact path",
			host: "group.gitlab-example.com",
			path: "/-/project/-/jobs/1/artifacts/..%2F..%2F..%2F..%2Fuser",
		},
		{
			name: "empty top-level group",
			host: ".gitlab-example.com",
			path: "/-/victim/project/-/jobs/1/artifacts/200.html",
		},
		{
			name: "parent segment as top-level group",
			host: "...gitlab-example.com",
			path: "/-/victim/project/-/jobs/1/artifacts/200.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := GetPageFromListener(t, httpListener, tt.host, tt.path)
			require.NoError(t, err)
			testhelpers.Close(t, resp.Body)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			require.Equal(t, http.StatusNotFound, resp.StatusCode)
			require.NotContains(t, string(body), upstreamContent)
			require.Empty(t, takeRequested())
		})
	}
}

func testArtifactProxyRequestWithMTLS(t *testing.T, content string, tests []testCase, clientCertPath, clientKeyPath, caCertPath string) {
	t.Helper()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RawPath {
		case "/api/v4/projects/group%2Fproject/jobs/1/artifacts/200.html",
			"/api/v4/projects/group%2Fsubgroup%2Fproject/jobs/1/artifacts/200.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, content)
		case "/api/v4/projects/group%2Fproject/jobs/1/artifacts/gzip-encoded.html":
			acceptEncoding := r.Header.Get("Accept-Encoding")
			if acceptEncoding == "gzip, deflate" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Content-Encoding", "gzip")
				fmt.Fprint(w, content)
			} else {
				w.WriteHeader(http.StatusBadRequest)
			}
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.RawPath)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, content)
		}
	}))

	keyFile, certFile := CreateHTTPSFixtureFiles(t)
	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	tlsCfg := &tls.Config{
		ClientCAs:    testhelpers.CertPool(t, caCertPath),
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{serverCert, testhelpers.Cert(t, clientCertPath, clientKeyPath)},
		MinVersion:   tls.VersionTLS12,
	}

	testServer.TLS = tlsCfg
	testServer.StartTLS()

	t.Cleanup(func() {
		testServer.Close()
	})

	// Ensure the IP address is used in the URL, as we're relying on IP SANs to
	// validate
	artifactServerURL := testServer.URL + "/api/v4"
	t.Log("Artifact server URL", artifactServerURL)

	args := []string{"-artifacts-server=" + artifactServerURL, "-artifacts-server-timeout=1"}

	t.Setenv("SSL_CERT_FILE", certFile)

	clientCert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	require.NoError(t, err)

	caCert, err := loadCACertificate(caCertPath)
	require.NoError(t, err)

	RunPagesProcess(t,
		withListeners([]ListenSpec{httpListener}),
		withArguments(args),
		withExtraArgument("client-cert", clientCertPath),
		withExtraArgument("client-key", clientKeyPath),
		withExtraArgument("client-ca-certs", caCertPath),
		withStubOptions(gitlabstub.WithCertificate(clientCert), gitlabstub.WithMutualTLS(caCert)),
	)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var resp *http.Response
			var err error

			if tt.acceptEncoding != "" {
				header := http.Header{}
				header.Set("Accept-Encoding", tt.acceptEncoding)
				resp, err = GetPageFromListenerWithHeaders(t, httpListener, tt.host, tt.path, header)
			} else {
				resp, err = GetPageFromListener(t, httpListener, tt.host, tt.path)
			}

			require.NoError(t, err)
			testhelpers.Close(t, resp.Body)

			require.Equal(t, tt.status, resp.StatusCode)
			require.Equal(t, tt.contentType, resp.Header.Get("Content-Type"))
			require.Equal(t, tt.contentEncoding, resp.Header.Get("Content-Encoding"))

			if tt.status == http.StatusOK {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Equal(t, tt.content, string(body))
				require.Equal(t, tt.length, resp.ContentLength)
				require.Equal(t, tt.cacheControl, resp.Header.Get("Cache-Control"))
			}
		})
	}
}

func TestArtifactProxyRequestWithValidMTLS(t *testing.T) {
	content := "<!DOCTYPE html><html><head><title>Title of the document</title></head><body></body></html>"
	contentLength := int64(len(content))

	clientCertPath := "../../test/testdata/mutualtls/valid/client.crt"
	clientKeyPath := "../../test/testdata/mutualtls/valid/client.key"
	caCertPath := "../../test/testdata/mutualtls/valid/ca.crt"

	tests := []testCase{
		{
			name:         "basic proxied request",
			host:         "group.gitlab-example.com",
			path:         "/-/project/-/jobs/1/artifacts/200.html",
			status:       http.StatusOK,
			content:      content,
			length:       contentLength,
			cacheControl: "max-age=3600",
			contentType:  "text/html; charset=utf-8",
		},
		{
			name:         "basic proxied request for subgroup",
			host:         "group.gitlab-example.com",
			path:         "/-/subgroup/project/-/jobs/1/artifacts/200.html",
			status:       http.StatusOK,
			content:      content,
			length:       contentLength,
			cacheControl: "max-age=3600",
			contentType:  "text/html; charset=utf-8",
		},
		{
			name:            "gzip encoded response with mTLS",
			host:            "group.gitlab-example.com",
			path:            "/-/project/-/jobs/1/artifacts/gzip-encoded.html",
			status:          http.StatusOK,
			content:         content,
			length:          contentLength,
			cacheControl:    "max-age=3600",
			contentType:     "text/html; charset=utf-8",
			contentEncoding: "gzip",
			acceptEncoding:  "gzip, deflate",
		},
	}

	testArtifactProxyRequestWithMTLS(t, content, tests, clientCertPath, clientKeyPath, caCertPath)
}

func TestArtifactProxyRequestWithInvalidMTLS(t *testing.T) {
	content := "<!DOCTYPE html><html><head><title>Title of the document</title></head><body></body></html>"

	clientCertPath := "../../test/testdata/mutualtls/invalid/client.crt"
	clientKeyPath := "../../test/testdata/mutualtls/invalid/client.key"
	caCertPath := "../../test/testdata/mutualtls/invalid/ca.crt"

	tests := []testCase{
		{
			name:         "basic proxied request",
			host:         "group.gitlab-example.com",
			path:         "/-/project/-/jobs/1/artifacts/200.html",
			status:       http.StatusBadGateway,
			content:      "",
			length:       0,
			cacheControl: "",
			contentType:  "text/html; charset=utf-8",
		},
		{
			name:         "basic proxied request for subgroup",
			host:         "group.gitlab-example.com",
			path:         "/-/subgroup/project/-/jobs/1/artifacts/200.html",
			status:       http.StatusBadGateway,
			content:      "",
			length:       0,
			cacheControl: "",
			contentType:  "text/html; charset=utf-8",
		},
	}
	testArtifactProxyRequestWithMTLS(t, content, tests, clientCertPath, clientKeyPath, caCertPath)
}
