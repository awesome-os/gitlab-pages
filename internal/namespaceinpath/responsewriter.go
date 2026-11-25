package namespaceinpath

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

// responseWriter is a wrapper around http.ResponseWriter that captures the response
type responseWriter struct {
	http.ResponseWriter
	pagesDomain     string
	authRedirectURI string
	ctx             context.Context
}

// newResponseWriter creates a new responseWriter
func newResponseWriter(ctx context.Context, w http.ResponseWriter, pagesDomain string, authRedirectURI string) *responseWriter {
	return &responseWriter{
		ResponseWriter:  w,
		pagesDomain:     pagesDomain,
		authRedirectURI: authRedirectURI,
		ctx:             ctx,
	}
}

// WriteHeader captures the status code and rewrites the location url to namespace in path
func (w *responseWriter) WriteHeader(statusCode int) {
	header := w.Header()

	if header.Get("Location") != "" {
		parsedLocationURL, err := url.Parse(header.Get("Location"))
		if err == nil && !w.isAuthURL(parsedLocationURL) {
			newURL := &customURL{URL: parsedLocationURL, pagesDomain: w.pagesDomain}

			ctx := w.ctx
			logger := logv2.FromContext(ctx)

			if err = newURL.convertToNamespaceInPath(); err != nil {
				logger.ErrorContext(ctx, "while writing location header, couldn't convert URL",
					slog.String("orig_host", parsedLocationURL.Host),
					slog.String("orig_path", parsedLocationURL.Path),
					slog.String("pages_domain", w.pagesDomain),
					logv2.CorrelationID(header.Get("X-Request-Id")),
					logv2.Error(err),
				)
			} else {
				logger.DebugContext(ctx, "while writing location header, converted URL",
					slog.String("orig_host", parsedLocationURL.Host),
					slog.String("orig_path", parsedLocationURL.Path),
					slog.String("new_location", logging.CleanURL(newURL.String())),
					logv2.CorrelationID(header.Get("X-Request-Id")),
				)
				w.Header().Set("Location", newURL.String())
			}
		}
	}

	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriter) isAuthURL(reqURL *url.URL) bool {
	if w.authRedirectURI == "" {
		return false
	}

	if isAuthRedirectURL(reqURL, w.authRedirectURI) {
		return true
	}

	_, port, _ := net.SplitHostPort(reqURL.Host)

	pagesDomainWithPort := w.pagesDomain
	if port != "" {
		pagesDomainWithPort = w.pagesDomain + ":" + port
	}

	authSegment := ""
	if reqURL.Host == pagesDomainWithPort {
		// if namespace in path
		segments := strings.Split(strings.TrimPrefix(reqURL.Path, "/"), "/")

		if len(segments) == 2 {
			authSegment = segments[1]
		}
	} else if strings.HasSuffix(reqURL.Host, "."+pagesDomainWithPort) {
		// if namespace in host
		segments := strings.Split(strings.TrimPrefix(reqURL.Path, "/"), "/")

		if len(segments) == 1 {
			authSegment = segments[0]
		}
	}

	return authSegment == "auth"
}
