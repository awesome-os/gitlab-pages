package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"gitlab.com/gitlab-org/labkit/correlation"

	"gitlab.com/gitlab-org/gitlab-pages/internal/source/mock"
)

func TestFetchAccessTokenSaysWhatGitLabAnswered(t *testing.T) {
	tests := map[string]struct {
		respond http.HandlerFunc
		message string
	}{
		"oauth error with description": {
			respond: respondJSONHandler(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"code spent"}`),
			message: "token endpoint returned 400: invalid_grant: code spent",
		},
		"oauth error alone": {
			respond: respondJSONHandler(http.StatusUnauthorized, `{"error":"invalid_client"}`),
			message: "token endpoint returned 401: invalid_client",
		},
		"non-JSON error page": {
			respond: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "<html>500</html>", http.StatusInternalServerError)
			},
			message: "token endpoint returned 500",
		},
		"undecodable success body": {
			respond: respondJSONHandler(http.StatusOK, `{"access_token":`),
			message: "token endpoint returned 200: unexpected EOF",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			apiServer := httptest.NewServer(tc.respond)
			t.Cleanup(apiServer.Close)
			auth := createTestAuth(t, apiServer.URL, "")

			_, err := auth.fetchAccessToken(context.Background(), "code", "verifier")

			require.EqualError(t, err, tc.message)
		})
	}
}

func TestFetchAccessTokenKeepsTheTransportCause(t *testing.T) {
	apiServer := httptest.NewServer(http.NotFoundHandler())
	apiServer.Close()
	auth := createTestAuth(t, apiServer.URL, "")

	_, err := auth.fetchAccessToken(context.Background(), "code", "verifier")

	require.ErrorContains(t, err, "token endpoint: ")
	require.ErrorContains(t, err, "connection refused")
}

func TestCallbackLogsWhatGitLabAnswered(t *testing.T) {
	apiServer := httptest.NewServer(respondJSONHandler(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"code spent"}`))
	t.Cleanup(apiServer.Close)
	auth := createTestAuth(t, apiServer.URL, "")
	logs, r := captureLogs(t, callbackRequest(t, auth))

	result := httptest.NewRecorder()
	require.True(t, auth.TryAuthenticate(result, r, mock.NewMockSource(gomock.NewController(t))))

	require.Equal(t, http.StatusServiceUnavailable, result.Code)
	require.Contains(t, logs.String(), `"error":"token endpoint returned 400: invalid_grant: code spent"`)
}

func TestCallbackFailureLineCarriesTheCorrelationID(t *testing.T) {
	apiServer := httptest.NewServer(respondJSONHandler(http.StatusBadRequest, `{"error":"invalid_grant"}`))
	t.Cleanup(apiServer.Close)
	auth := createTestAuth(t, apiServer.URL, "")
	r := callbackRequest(t, auth)
	r = r.WithContext(correlation.ContextWithCorrelation(r.Context(), "corr-123"))
	logs, r := captureLogs(t, r)

	require.True(t, auth.TryAuthenticate(httptest.NewRecorder(), r, mock.NewMockSource(gomock.NewController(t))))

	require.Contains(t, logs.String(), `"correlation_id":"corr-123"`, "Rails logs the same id, so the two sides of a failure can be joined")
}

func TestCallbackAnswers404WhenTheClientHasGone(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server only notices a vanished client once the body is read.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(apiServer.Close)
	auth := createTestAuth(t, apiServer.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	r := callbackRequest(t, auth).WithContext(ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result := httptest.NewRecorder()
	require.True(t, auth.TryAuthenticate(result, r, mock.NewMockSource(gomock.NewController(t))))

	require.Equal(t, http.StatusNotFound, result.Code, "a client that left is not a failed exchange")
}

func TestTokenRequestCarriesTheCorrelationID(t *testing.T) {
	var got string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Request-ID")
		respondJSONHandler(http.StatusOK, `{"access_token":"abc"}`)(w, r)
	}))
	t.Cleanup(apiServer.Close)
	auth := createTestAuth(t, apiServer.URL, "")

	_, err := auth.fetchAccessToken(correlation.ContextWithCorrelation(context.Background(), "corr-123"), "code", "")

	require.NoError(t, err)
	require.Equal(t, "corr-123", got, "Rails logs the same id, so the two sides of a failure can be joined")
}

// callbackRequest is the second leg of the sign-in: the page's own /auth
// with an encrypted code and the session the first leg left behind.
func callbackRequest(t *testing.T, auth *Auth) *http.Request {
	t.Helper()

	code, err := auth.EncryptAndSignCode("http://example.com", "1")
	require.NoError(t, err)
	r, err := http.NewRequest("GET", "http://example.com/auth?code="+code+"&state=state", nil)
	require.NoError(t, err)
	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"uri":   "http://example.com/private.project/",
		"state": "state",
	})
	return r
}

func respondJSONHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}
