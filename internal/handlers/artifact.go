package handlers

import (
	"net/http"

	domainCfg "gitlab.com/gitlab-org/gitlab-pages/internal/domain"
)

func ArtifactMiddleware(handler http.Handler, h *Handlers) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		domain := domainCfg.FromRequest(r)

		if h.HandleArtifactRequest(w, r, domain) {
			return
		}

		handler.ServeHTTP(w, r)
	})
}
