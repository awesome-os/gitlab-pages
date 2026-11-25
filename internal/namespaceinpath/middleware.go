package namespaceinpath

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"gitlab.com/gitlab-org/gitlab-pages/internal/httperrors"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/utils"
)

const authPath = "/auth"

type middleware struct {
	next            http.Handler
	pagesDomain     string
	authRedirectURI string
	skipAuthRewrite bool
	statusPath      string
}

// NewMiddleware creates a new middleware
func NewMiddleware(h http.Handler, pagesDomain string, authRedirectURI string, statusPath string) http.Handler {
	return &middleware{
		next:            h,
		pagesDomain:     pagesDomain,
		authRedirectURI: authRedirectURI,
		statusPath:      statusPath,
		// Based on the documentation: https://gitlab.com/gitlab-org/gitlab-pages#gitlab-access-control
		// When auth redirect URI is without reserved namespace, we skip url rewrite.
		skipAuthRewrite: isAuthRedirectURLWithoutNamespace(authRedirectURI),
	}
}

// ServeHTTP handles the HTTP request and rewrites the URL from namespace in path to namespace in host.
// Example: example.com/namespace/a-path rewrites to namespace.example.com/a-path
func (m *middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Host == "" {
		r.URL.Host = r.Host
	}

	// When namespace in path is enabled, do not serve namespace in host urls.
	if m.isNamespaceInHost(r.URL) {
		httperrors.Serve404(w)
		return
	}

	m.extractNamespaceFromPath(r)

	crw := newResponseWriter(r.Context(), w, m.pagesDomain, m.authRedirectURI)

	m.next.ServeHTTP(crw, r)
}

// isHealthCheckPath checks if the request path is a health check path
// Health check paths should match the configured StatusPath and should not be processed as namespace paths
func (m *middleware) isHealthCheckPath(path string) bool {
	return m.statusPath != "" && path == m.statusPath
}

// extractNamespaceFromPath extracts the namespace from the request URL path
// and updates the request object accordingly.
func (m *middleware) extractNamespaceFromPath(r *http.Request) {
	if m.isHealthCheckPath(r.URL.Path) {
		return
	}

	if m.skipAuthRewrite && isAuthRedirectURL(r.URL, m.authRedirectURI) {
		return
	}

	oldHost := r.Host
	newURL := &customURL{URL: r.URL, pagesDomain: m.pagesDomain}
	if err := newURL.convertFromNamespaceInPath(); err != nil {
		logging.Logger.ErrorContext(logging.LogRequest(r), "can't convert URL",
			"orig_host", r.Host,
			"orig_path", r.URL.Path,
			"pages_domain", m.pagesDomain,
			"error", err,
		)

		return
	}

	logging.Logger.DebugContext(logging.LogRequest(r), "Rewrite namespace host",
		"new_host", newURL.Host,
		"new_path", newURL.Path,
	)

	r.Host = newURL.Host
	r.URL = newURL.URL

	namespace := strings.TrimSuffix(newURL.Host, "."+oldHost)
	r.Header.Set("X-Gitlab-Namespace-In-Path", namespace)

	r.RequestURI = strings.TrimPrefix(r.RequestURI, "/"+namespace)
}

// isNamespaceInHost checks if the request is for a namespace in the host
// rather than a namespace in the path.
func (m *middleware) isNamespaceInHost(reqURL *url.URL) bool {
	reqHost, _, err := net.SplitHostPort(reqURL.Host)
	if err != nil {
		reqHost = reqURL.Host
	}

	return strings.HasSuffix(reqHost, "."+m.pagesDomain)
}

// isAuthRedirectURL checks if the request URL matches the configured auth redirect URL.
func isAuthRedirectURL(reqURL *url.URL, authRedirect string) bool {
	authURL, valid := utils.ParseURL(authRedirect)
	if !valid {
		return false
	}

	// Compare the host and path of the request URL with the auth redirect URL
	return reqURL.Host == authURL.Host && strings.HasPrefix(reqURL.Path, authURL.Path)
}

// isAuthRedirectURLWithoutNamespace checks if the auth redirect URL has only "/auth" as the path.
func isAuthRedirectURLWithoutNamespace(authRedirect string) bool {
	authURL, valid := utils.ParseURL(authRedirect)
	if !valid {
		return false
	}

	// Compare the path of the auth redirect URL with "/auth" as the path.
	return authURL.Path == authPath
}
