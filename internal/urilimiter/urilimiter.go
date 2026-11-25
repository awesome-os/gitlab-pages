package urilimiter

import (
	"net/http"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/httperrors"
	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/client"
)

func NewMiddleware(handler http.Handler, limit int) http.Handler {
	if limit == 0 {
		return handler
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.RequestURI) > limit {
			httperrors.Serve414(w)

			return
		}

		if err := client.ValidateHostName(request.GetHostWithoutPort(r)); err != nil {
			client.LogInvalidHostname(r.Context(), err, logv2.HTTPHost(r.Host))
			httperrors.Serve404(w)

			return
		}

		handler.ServeHTTP(w, r)
	})
}
