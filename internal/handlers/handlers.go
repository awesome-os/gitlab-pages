package handlers

import (
	"net/http"

	"gitlab.com/gitlab-org/gitlab-pages/internal"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

// Handlers take care of handling specific requests
type Handlers struct {
	Auth     internal.Auth
	Artifact internal.Artifact
}

// New when provided the arguments defined herein, returns a pointer to an
// Handlers that is used to handle requests.
func New(auth internal.Auth, artifact internal.Artifact) *Handlers {
	return &Handlers{
		Auth:     auth,
		Artifact: artifact,
	}
}

func (h *Handlers) checkIfLoginRequiredOrInvalidToken(w http.ResponseWriter, r *http.Request, token string, domain internal.Domain) func(*http.Response) bool {
	return func(resp *http.Response) bool {
		// API will return 403 if the project does not have public pipelines (public_builds flag)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
			if token == "" {
				if !h.Auth.IsAuthSupported() {
					// Auth is not supported, probably means no access or does not exist but we cannot try with auth
					return false
				}

				logging.Logger.DebugContext(logging.LogRequest(r), "Artifact API response without token, try with authentication",
					"status_code", resp.StatusCode)

				// Authenticate user
				if h.Auth.RequireAuth(w, r, domain) {
					return true
				}
			} else {
				logging.Logger.DebugContext(logging.LogRequest(r), "Artifact API response with authentication",
					"status_code", resp.StatusCode)
			}
		}

		if h.Auth.CheckResponseForInvalidToken(w, r, resp) {
			return true
		}

		return false
	}
}

// HandleArtifactRequest handles all artifact related requests, will return true if request was handled here
func (h *Handlers) HandleArtifactRequest(w http.ResponseWriter, r *http.Request, domain internal.Domain) bool {
	// In the event a host is prefixed with the artifact prefix an artifact
	// value is created, and an attempt to proxy the request is made

	// Always try to add token to the request if it exists
	token, err := h.Auth.GetTokenIfExists(w, r)
	if err != nil {
		return true
	}

	//nolint: bodyclose // false positive
	// a.checkIfLoginRequiredOrInvalidToken returns a response.Body, closing this body is responsibility
	// of the TryMakeRequest implementation
	return h.Artifact.TryMakeRequest(w, r, token, h.checkIfLoginRequiredOrInvalidToken(w, r, token, domain))
}
