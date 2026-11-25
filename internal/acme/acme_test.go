package acme_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/acme"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
)

const (
	baseURL      = "http://example.com"
	indexURL     = baseURL + "/index.html"
	challengeURL = baseURL + "/.well-known/acme-challenge/token"
)

func TestAcmeRedirectLogOmitsChallengeQuery(t *testing.T) {
	gitlabURL, err := url.Parse("https://gitlab.example.com")
	require.NoError(t, err)

	buf := loggingtest.Capture(t, slog.LevelDebug)

	r := httptest.NewRequest(http.MethodGet, challengeURL, nil)
	w := httptest.NewRecorder()
	require.True(t, acme.ServeAcmeChallenges(w, r, func(http.ResponseWriter, *http.Request) bool { return false }, gitlabURL))

	require.Contains(t, w.Header().Get("Location"), "token=token")
	require.NotEmpty(t, buf.String())
	require.NotContains(t, buf.String(), "token=token")
	require.Contains(t, buf.String(), `"path":"/.well-known/acme-challenge/token"`)
}

func TestAcmeMiddleware(t *testing.T) {
	u, err := url.Parse("https://gitlab.example.com")
	require.NoError(t, err)

	testCases := []struct {
		name           string
		f              acme.FallbackStrategy
		path           string
		expectedStatus int
	}{
		{
			name:           "not an acme request",
			path:           indexURL,
			expectedStatus: http.StatusAccepted,
		},
		{
			name: "acme challenge redirect to gitlab if missing",
			f: func(w http.ResponseWriter, r *http.Request) bool {
				return false
			},
			path:           challengeURL,
			expectedStatus: http.StatusTemporaryRedirect,
		},
		{
			name: "acme challenge served from disk if present",
			f: func(w http.ResponseWriter, r *http.Request) bool {
				w.WriteHeader(http.StatusOK)
				return true
			},
			path:           challengeURL,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !acme.ServeAcmeChallenges(w, r, tc.f, u) {
					w.WriteHeader(http.StatusAccepted)
				}
			})

			require.HTTPStatusCode(t, h.ServeHTTP, http.MethodGet, tc.path, nil, tc.expectedStatus)
		})
	}
}
