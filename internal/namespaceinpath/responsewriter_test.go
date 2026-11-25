package namespaceinpath

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"
)

func TestCustomResponseWriter_WriteHeader(t *testing.T) {
	pagesDomain := "example.com"
	authRedirectURI := "http://example.com/projects/auth"

	cases := []struct {
		name             string
		locationHeader   string
		expectedLocation string
		statusCode       int
	}{
		{
			name:             "Valid location header conversion",
			locationHeader:   "https://namespace.example.com/a-path",
			expectedLocation: "https://example.com/namespace/a-path",
			statusCode:       http.StatusFound,
		},
		{
			name:             "Location header is an auth URL",
			locationHeader:   authRedirectURI,
			expectedLocation: authRedirectURI,
			statusCode:       http.StatusFound,
		},
		{
			name:             "Location header is empty",
			locationHeader:   "",
			expectedLocation: "",
			statusCode:       http.StatusFound,
		},
		{
			name:             "Custom domain location header conversion",
			locationHeader:   "https://namespace.another.com/a-path",
			expectedLocation: "https://namespace.another.com/a-path",
			statusCode:       http.StatusFound,
		},
		{
			name:             "Custom domain ending with the pages domain is not converted",
			locationHeader:   "https://myexample.com/a-path",
			expectedLocation: "https://myexample.com/a-path",
			statusCode:       http.StatusFound,
		},
		{
			name:             "Valid location header with no schema conversion",
			locationHeader:   "//namespace.example.com/a-path",
			expectedLocation: "//example.com/namespace/a-path",
			statusCode:       http.StatusFound,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writer := newResponseWriter(context.Background(), recorder, pagesDomain, authRedirectURI)

			// Set the Location header
			if c.locationHeader != "" {
				writer.Header().Set("Location", c.locationHeader)
			}

			// Write the header with the provided status code
			writer.WriteHeader(c.statusCode)

			// Assert the Location header
			require.Equal(t, c.expectedLocation, writer.Header().Get("Location"))
		})
	}
}

func TestCustomResponseWriterLogsLocationWithoutQuery(t *testing.T) {
	buf := &bytes.Buffer{}
	level := slog.LevelDebug
	logger := logv2.NewWithConfig(&logv2.Config{Writer: buf, LogLevel: &level})
	ctx := logv2.WithLogger(context.Background(), logger)
	recorder := httptest.NewRecorder()
	writer := newResponseWriter(ctx, recorder, "example.com", "")
	writer.Header().Set("Location", "https://namespace.example.com/a-path?token=location-secret#fragment-secret")

	writer.WriteHeader(http.StatusFound)

	require.Equal(t, "https://example.com/namespace/a-path?token=location-secret#fragment-secret", writer.Header().Get("Location"))
	require.NotEmpty(t, buf.String())
	require.NotContains(t, buf.String(), "location-secret")
	require.NotContains(t, buf.String(), "fragment-secret")
}

func TestCustomResponseWriter_IsAuthURL(t *testing.T) {
	pagesDomain := "example.com"
	authRedirectURI := "http://example.com/projects/auth"

	cases := []struct {
		rawURL         string
		expectedResult bool
	}{
		{
			rawURL:         authRedirectURI,
			expectedResult: true,
		},
		{
			rawURL:         "http://example.com/namespace/auth",
			expectedResult: true,
		},
		{
			rawURL:         "http://example.com/not-auth",
			expectedResult: false,
		},
		{
			rawURL:         "http://example.com/",
			expectedResult: false,
		},
		{
			rawURL:         "http://namespace.example.com/auth",
			expectedResult: true,
		},
		{
			rawURL:         "http://namespace.example.com/not-auth",
			expectedResult: false,
		},
		{
			rawURL:         "http://another.com/auth",
			expectedResult: false,
		},
		{
			rawURL:         "http://another.com/namespace/auth",
			expectedResult: false,
		},
		{
			rawURL:         "http://myexample.com/auth",
			expectedResult: false,
		},
	}

	for _, c := range cases {
		t.Run(c.rawURL, func(t *testing.T) {
			writer := newResponseWriter(context.Background(), httptest.NewRecorder(), pagesDomain, authRedirectURI)

			parsedURL, err := url.Parse(c.rawURL)
			require.NoError(t, err)

			result := writer.isAuthURL(parsedURL)
			require.Equal(t, c.expectedResult, result)
		})
	}
}
