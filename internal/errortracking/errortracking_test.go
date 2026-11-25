package errortracking

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestForErrorTrackingContainsOnlySafeRequestData(t *testing.T) {
	requestURL, err := url.Parse("https://user:password@example.com/auth?code=code-secret&state=state-secret&domain=domain-secret&arbitrary=arbitrary-secret#fragment-secret")
	require.NoError(t, err)

	r := &http.Request{
		Method:           http.MethodPost,
		URL:              requestURL,
		Host:             "example.com",
		Header:           http.Header{"Authorization": {"Bearer authorization-secret"}, "Cookie": {"gitlab-pages=cookie-secret"}, "X-Custom-Token-Value": {"header-secret"}},
		Body:             io.NopCloser(strings.NewReader("body-secret")),
		RequestURI:       requestURL.RequestURI(),
		TransferEncoding: []string{"chunked"},
		Trailer:          http.Header{"X-Trailer": {"trailer-secret"}},
		Form:             url.Values{"form": {"form-secret"}},
		PostForm:         url.Values{"post-form": {"post-form-secret"}},
	}

	got := requestForErrorTracking(r)

	require.Equal(t, http.MethodPost, got.Method)
	require.Equal(t, "example.com", got.Host)
	require.Equal(t, "https://example.com/auth", got.URL.String())
	require.Empty(t, got.Header)
	require.Nil(t, got.Body)
	require.Empty(t, got.RequestURI)
	require.Empty(t, got.TransferEncoding)
	require.Empty(t, got.Trailer)
	require.Empty(t, got.Form)
	require.Empty(t, got.PostForm)
	require.Nil(t, got.MultipartForm)

	require.Equal(t, "code-secret", r.URL.Query().Get("code"))
	require.Equal(t, "Bearer authorization-secret", r.Header.Get("Authorization"))
	require.NotNil(t, r.Body)
}
