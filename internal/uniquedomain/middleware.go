package uniquedomain

import (
	"net"
	"net/http"
	"strings"

	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

func NewMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uniqueURL := getUniqueURL(r)
		if uniqueURL == "" {
			logging.Logger.DebugContext(logging.LogRequest(r), "unique domain: doing nothing", "uniqueURL", logging.CleanURL(uniqueURL))

			handler.ServeHTTP(w, r)
			return
		}

		logging.Logger.InfoContext(logging.LogRequest(r), "redirecting to unique domain", "uniqueURL", logging.CleanURL(uniqueURL))

		http.Redirect(w, r, uniqueURL, http.StatusPermanentRedirect)
	})
}

func getUniqueURL(r *http.Request) string {
	domain := domain.FromRequest(r)
	lookupPath, err := domain.GetLookupPath(r)
	if err != nil {
		logging.Logger.ErrorContext(logging.LogRequest(r), "uniqueDomain: failed to get lookupPath", "error", err)
		return ""
	}

	// No uniqueHost to redirect
	if lookupPath.UniqueHost == "" {
		return ""
	}

	requestHost, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		requestHost = r.Host
	}

	// Already serving the uniqueHost
	if lookupPath.UniqueHost == requestHost {
		return ""
	}

	uniqueURL := *r.URL
	if port == "" {
		uniqueURL.Host = lookupPath.UniqueHost
	} else {
		uniqueURL.Host = net.JoinHostPort(lookupPath.UniqueHost, port)
	}

	// Ensure to redirect to the same path requested
	uniqueURL.Path = strings.TrimPrefix(
		r.URL.Path,
		strings.TrimSuffix(lookupPath.Prefix, "/"),
	)

	return uniqueURL.String()
}
