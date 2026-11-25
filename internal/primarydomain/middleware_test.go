package primarydomain

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
)

func TestMiddlewareLogsPrimaryURLWithoutQuery(t *testing.T) {
	previousLookup := getPrimaryDomainAndPrefixFunc
	getPrimaryDomainAndPrefixFunc = func(*http.Request) (string, string) {
		return "https://default.example.com?token=primary-secret", ""
	}
	t.Cleanup(func() { getPrimaryDomainAndPrefixFunc = previousLookup })

	buf := loggingtest.Capture(t, slog.LevelInfo)

	middleware := NewMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "http://abc.example.com/path", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, r)

	require.Equal(t, "https://default.example.com/path?token=primary-secret", w.Header().Get("Location"))
	require.NotEmpty(t, buf.String())
	require.NotContains(t, buf.String(), "primary-secret")
	require.Contains(t, buf.String(), `"host":"abc.example.com"`)
}

func TestMiddlewareRedirectsToPrimaryDomain(t *testing.T) {
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewMiddleware(finalHandler)

	tests := []struct {
		name               string
		requestHost        string
		requestPath        string
		primaryDomain      string
		prefix             string
		expectedStatusCode int
		expectedLocation   string
	}{
		{
			name:               "redirect to primary domain",
			requestHost:        "abc.example.com",
			requestPath:        "/path/to/resource",
			primaryDomain:      "https://default.example.com",
			expectedStatusCode: http.StatusPermanentRedirect,
			expectedLocation:   "https://default.example.com/path/to/resource",
		},
		{
			name:               "redirect to primary domain having prefix",
			requestHost:        "abc.example.com",
			requestPath:        "/project/path/to/resource",
			primaryDomain:      "https://default.example.com",
			prefix:             "/project/",
			expectedStatusCode: http.StatusPermanentRedirect,
			expectedLocation:   "https://default.example.com/path/to/resource",
		},
		{
			name:               "no redirect if already on primary domain",
			requestHost:        "default.example.com",
			requestPath:        "/path/to/resource",
			primaryDomain:      "https://default.example.com",
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "no redirect if primaryDomain is empty",
			requestHost:        "abc.example.com",
			requestPath:        "/path/to/resource",
			primaryDomain:      "",
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "invalid primary domain",
			requestHost:        "abc.example.com",
			requestPath:        "/path/to/resource",
			primaryDomain:      "://invalid-url",
			expectedStatusCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getPrimaryDomainAndPrefixFunc = func(r *http.Request) (string, string) {
				return tc.primaryDomain, tc.prefix
			}

			r := httptest.NewRequest(http.MethodGet, "http://"+tc.requestHost+tc.requestPath, nil)
			r.Host = tc.requestHost

			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, r)

			require.Equal(t, tc.expectedStatusCode, rec.Code)

			if tc.expectedStatusCode == http.StatusPermanentRedirect {
				require.Equal(t, tc.expectedLocation, rec.Header().Get("Location"))
			} else {
				require.Empty(t, rec.Header().Get("Location"))
			}
		})
	}
}
