package logging

import (
	"net/http"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"
)

// WithLoggerMiddleware stores the global Logger in the request context so that
// downstream handlers and helpers can retrieve it via logv2.FromContext.
func WithLoggerMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := logv2.WithLogger(r.Context(), Logger)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
