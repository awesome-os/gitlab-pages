package namespaceinpath

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiddleware_ServeHTTP(t *testing.T) {
	pagesDomain := "example.com"
	statusPath := "/-/readiness"

	tests := []struct {
		name            string
		requestURL      string
		authRedirectURI string
		expectedStatus  int
		expectedHost    string
		expectedPath    string
	}{
		{
			name:            "Valid namespace in path",
			requestURL:      "http://example.com/namespace/a-path",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "namespace.example.com",
			expectedPath:    "/a-path",
		},
		{
			name:            "Valid namespace in path with port",
			requestURL:      "http://example.com:8080/namespace/a-path",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "namespace.example.com:8080",
			expectedPath:    "/a-path",
		},
		{
			name:            "Namespace in host should return 404",
			requestURL:      "http://namespace.example.com/a-path",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusNotFound,
		},
		{
			name:            "Auth URL without namespace should not be modified",
			requestURL:      "http://example.com/auth?domain=http://example.com/namespace&state=test",
			authRedirectURI: "http://example.com/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "",
			expectedPath:    "/auth",
		},
		{
			name:            "Auth URL with namespace should be modified",
			requestURL:      "http://example.com/projects/auth?domain=http://example.com/namespace&state=test",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "projects.example.com",
			expectedPath:    "/auth",
		},
		{
			name:            "Custom domain should not be modified",
			requestURL:      "http://another.com/a-path",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "another.com",
			expectedPath:    "/a-path",
		},
		{
			name:            "Custom domain ending with the pages domain should not be modified",
			requestURL:      "http://myexample.com/a-path",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "myexample.com",
			expectedPath:    "/a-path",
		},
		{
			name:            "Valid namespace root with trailing slash",
			requestURL:      "http://example.com/group/",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "group.example.com",
			expectedPath:    "/",
		},
		{
			name:            "Valid namespace in path with extra path",
			requestURL:      "http://example.com/group/path/to/file",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "group.example.com",
			expectedPath:    "/path/to/file",
		},
		{
			name:            "Valid namespace in path with extra path with trailing slash",
			requestURL:      "http://example.com/group/path/to/file/",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "group.example.com",
			expectedPath:    "/path/to/file/",
		},
		{
			name:            "Health check URL should not be modified",
			requestURL:      "http://example.com/-/readiness",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "example.com",
			expectedPath:    "/-/readiness",
		},
		{
			name:            "Health check URL with different path should be processed as namespace",
			requestURL:      "http://example.com/root/-/healthcheck",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "root.example.com",
			expectedPath:    "/-/healthcheck",
		},
		{
			name:            "Health check URL with query parameters should not be modified",
			requestURL:      "http://example.com/-/readiness?param=value",
			authRedirectURI: "http://example.com/projects/auth",
			expectedStatus:  http.StatusOK,
			expectedHost:    "example.com",
			expectedPath:    "/-/readiness",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			nipMiddleware := NewMiddleware(nextHandler, pagesDomain, tt.authRedirectURI, statusPath)

			req := httptest.NewRequest("GET", tt.requestURL, nil)
			rec := httptest.NewRecorder()

			nipMiddleware.ServeHTTP(rec, req)

			result := rec.Result()
			require.Equal(t, tt.expectedStatus, result.StatusCode)

			if tt.expectedStatus == http.StatusOK {
				if tt.expectedHost != "" {
					require.Equal(t, tt.expectedHost, req.Host)
				}
				if tt.expectedPath != "" {
					require.Equal(t, tt.expectedPath, req.URL.Path)
				}
			}
		})
	}
}

func TestMiddleware_ExtractNamespaceFromPath(t *testing.T) {
	statusPath := "/-/readiness"

	tests := []struct {
		name              string
		url               string
		authRedirectURI   string
		expectedHost      string
		expectedPath      string
		expectedNamespace string
	}{
		{
			name:              "Extract namespace from path",
			url:               "http://example.com/namespace/a-path",
			authRedirectURI:   "http://example.com/projects/auth",
			expectedHost:      "namespace.example.com",
			expectedPath:      "/a-path",
			expectedNamespace: "namespace",
		},
		{
			name:              "Auth URL not having namespace should not be modified",
			url:               "http://example.com/auth?domain=http://example.com/namespace&state=test",
			authRedirectURI:   "http://example.com/auth",
			expectedHost:      "example.com",
			expectedPath:      "/auth",
			expectedNamespace: "",
		},
		{
			name:              "Auth URL having namespace should be modified",
			url:               "http://example.com/projects/auth?domain=http://example.com/namespace&state=test",
			authRedirectURI:   "http://example.com/projects/auth",
			expectedHost:      "projects.example.com",
			expectedPath:      "/auth",
			expectedNamespace: "projects",
		},
		{
			name:              "Custom domain should not be modified",
			url:               "http://another.com/a-path",
			authRedirectURI:   "http://example.com/projects/auth",
			expectedHost:      "another.com",
			expectedPath:      "/a-path",
			expectedNamespace: "",
		},
		{
			name:              "Health check URL should not be modified",
			url:               "http://example.com/-/readiness",
			authRedirectURI:   "http://example.com/projects/auth",
			expectedHost:      "example.com",
			expectedPath:      "/-/readiness",
			expectedNamespace: "",
		},
		{
			name:              "Health check URL with different path should be processed as namespace",
			url:               "http://example.com/root/-/healthcheck",
			authRedirectURI:   "http://example.com/projects/auth",
			expectedHost:      "root.example.com",
			expectedPath:      "/-/healthcheck",
			expectedNamespace: "root",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", test.url, nil)

			nipMiddleware := &middleware{
				pagesDomain:     "example.com",
				authRedirectURI: test.authRedirectURI,
				statusPath:      statusPath,
				skipAuthRewrite: isAuthRedirectURLWithoutNamespace(test.authRedirectURI),
			}

			nipMiddleware.extractNamespaceFromPath(req)

			require.Equal(t, test.expectedHost, req.Host)
			require.Equal(t, test.expectedPath, req.URL.Path)
			if test.expectedNamespace != "" {
				require.Equal(t, test.expectedNamespace, req.Header.Get("X-Gitlab-Namespace-In-Path"))
			}
		})
	}
}

func TestMiddleware_IsNamespaceInHost(t *testing.T) {
	pagesDomain := "example.com"

	nipMiddleware := &middleware{
		pagesDomain: pagesDomain,
	}

	tests := []struct {
		name        string
		requestURL  string
		expectedRes bool
	}{
		{
			name:        "Namespace in host",
			requestURL:  "http://namespace.example.com/a-path",
			expectedRes: true,
		},
		{
			name:        "No namespace in host",
			requestURL:  "http://example.com/a-path",
			expectedRes: false,
		},
		{
			name:        "Custom domain",
			requestURL:  "http://namespace.another.com/a-path",
			expectedRes: false,
		},
		{
			name:        "Custom domain ending with the pages domain",
			requestURL:  "http://myexample.com/a-path",
			expectedRes: false,
		},
		{
			name:        "Custom domain ending with the pages domain with port",
			requestURL:  "http://myexample.com:8080/a-path",
			expectedRes: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL, _ := url.Parse(tt.requestURL)
			res := nipMiddleware.isNamespaceInHost(reqURL)

			require.Equal(t, tt.expectedRes, res)
		})
	}
}

func TestIsAuthRedirectURL(t *testing.T) {
	authRedirectURI := "http://example.com/projects/auth"

	tests := []struct {
		name        string
		requestURL  string
		expectedRes bool
	}{
		{
			name:        "Auth redirect URL",
			requestURL:  authRedirectURI + "?domain=http://example.com/namespace&state=test",
			expectedRes: true,
		},
		{
			name:        "URL with auth segment but non auth redirect URL",
			requestURL:  "http://example.com/auth",
			expectedRes: false,
		},
		{
			name:        "Non-auth URL",
			requestURL:  "http://example.com/not-auth",
			expectedRes: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL, _ := url.Parse(tt.requestURL)
			res := isAuthRedirectURL(reqURL, authRedirectURI)

			require.Equal(t, tt.expectedRes, res)
		})
	}
}

func TestIsAuthRedirectURLWithoutNamespace(t *testing.T) {
	tests := []struct {
		name         string
		authRedirect string
		expected     bool
	}{
		{
			name:         "Empty string",
			authRedirect: "",
			expected:     false,
		},
		{
			name:         "URL with different path",
			authRedirect: "http://www.example.com/not-auth",
			expected:     false,
		},
		{
			name:         "URL with namespace path",
			authRedirect: "http://www.example.com/namespace/auth",
			expected:     false,
		},
		{
			name:         "URL with auth path",
			authRedirect: "http://www.example.com/auth",
			expected:     true,
		},
		{
			name:         "URL with auth path and query",
			authRedirect: "http://www.example.com/auth?query=1",
			expected:     true,
		},
		{
			name:         "URL without schema",
			authRedirect: "www.example.com/auth",
			expected:     false,
		},
		{
			name:         "Only path",
			authRedirect: "/auth",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthRedirectURLWithoutNamespace(tt.authRedirect)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHealthCheckPath(t *testing.T) {
	statusPath := "/-/readiness"

	middleware := &middleware{
		statusPath: statusPath,
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "Health check path with readiness",
			path:     "/-/readiness",
			expected: true,
		},
		{
			name:     "Health check path with healthcheck",
			path:     "/-/healthcheck",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := middleware.isHealthCheckPath(tt.path)
			require.Equal(t, tt.expected, result)
		})
	}
}
