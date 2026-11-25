package primarydomain

import (
	"net"
	"net/http"
	"strings"

	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/utils"
)

func NewMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryURL := getPrimaryURL(r)
		if primaryURL == "" {
			logging.Logger.DebugContext(logging.LogRequest(r), "default domain: doing nothing")

			handler.ServeHTTP(w, r)
			return
		}

		logging.Logger.InfoContext(logging.LogRequest(r), "redirecting to primary domain", "primaryURL", logging.CleanURL(primaryURL))

		http.Redirect(w, r, primaryURL, http.StatusPermanentRedirect)
	})
}

var getPrimaryDomainAndPrefixFunc = getPrimaryDomainAndPrefix

func getPrimaryURL(r *http.Request) string {
	primaryDomain, prefix := getPrimaryDomainAndPrefixFunc(r)
	if primaryDomain == "" {
		return ""
	}

	requestHost, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		requestHost = r.Host
	}

	primaryURL, isValid := utils.ParseURL(primaryDomain)
	if !isValid {
		return ""
	}

	// If the requestHost is already the primaryDomain, no need to redirect
	if requestHost == primaryURL.Host {
		return ""
	}

	// Ensure to redirect to the same path requested
	primaryURL.Path = strings.TrimPrefix(
		r.URL.Path,
		strings.TrimSuffix(prefix, "/"),
	)

	return primaryURL.String()
}

func getPrimaryDomainAndPrefix(r *http.Request) (string, string) {
	lookupPath, err := domain.FromRequest(r).GetLookupPath(r)
	if err != nil {
		logging.Logger.ErrorContext(logging.LogRequest(r), "primaryDomain: failed to get lookupPath", "error", err)
		return "", ""
	}

	return lookupPath.PrimaryDomain, lookupPath.Prefix
}
