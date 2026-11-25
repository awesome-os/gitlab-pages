package namespaceinpath

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertFromNamespaceInPath(t *testing.T) {
	cases := []struct {
		rawURL       string
		pagesDomain  string
		expectedHost string
		expectedPath string
		expectedErr  error
		description  string
	}{
		{
			rawURL:       "https://example.com/namespace/a-path",
			pagesDomain:  "example.com",
			expectedHost: "namespace.example.com",
			expectedPath: "/a-path",
			expectedErr:  nil,
			description:  "Basic conversion from namespace in path to host",
		},
		{
			rawURL:       "https://example.com/",
			pagesDomain:  "example.com",
			expectedHost: "",
			expectedPath: "",
			expectedErr:  errFirstSegmentEmpty,
			description:  "Empty path segment",
		},
		{
			rawURL:       "https://another.com/namespace/a-path",
			pagesDomain:  "example.com",
			expectedHost: "",
			expectedPath: "",
			expectedErr:  errURLNotPagesDomain,
			description:  "URL not matching pages domain",
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			parsedURL, err := url.Parse(c.rawURL)
			require.NoError(t, err)

			customParsedURL := customURL{
				URL:         parsedURL,
				pagesDomain: c.pagesDomain,
			}

			err = customParsedURL.convertFromNamespaceInPath()
			require.ErrorIs(t, err, c.expectedErr)
			if c.expectedErr == nil {
				require.Equal(t, c.expectedHost, customParsedURL.Host)
				require.Equal(t, c.expectedPath, customParsedURL.Path)
			}
		})
	}
}

func TestConvertToNamespaceInPath(t *testing.T) {
	cases := []struct {
		rawURL       string
		pagesDomain  string
		expectedHost string
		expectedPath string
		expectedErr  error
		description  string
	}{
		{
			rawURL:       "https://namespace.example.com/a-path",
			pagesDomain:  "example.com",
			expectedHost: "example.com",
			expectedPath: "/namespace/a-path",
			expectedErr:  nil,
			description:  "Basic conversion from namespace in host to path",
		},
		{
			rawURL:       "https://example.com/a-path",
			pagesDomain:  "example.com",
			expectedHost: "",
			expectedPath: "",
			expectedErr:  errCantExtractNamespace,
			description:  "Cannot extract namespace from host",
		},
		{
			rawURL:       "https://namespace.another.com/a-path",
			pagesDomain:  "example.com",
			expectedHost: "",
			expectedPath: "",
			expectedErr:  errCantExtractNamespace,
			description:  "Host not matching pages domain",
		},
		{
			rawURL:       "https://myexample.com/a-path",
			pagesDomain:  "example.com",
			expectedHost: "",
			expectedPath: "",
			expectedErr:  errCantExtractNamespace,
			description:  "Host ending with the pages domain without a dot boundary",
		},
		{
			rawURL:       "https://myexample.com:8080/a-path",
			pagesDomain:  "example.com",
			expectedHost: "",
			expectedPath: "",
			expectedErr:  errCantExtractNamespace,
			description:  "Host with port ending with the pages domain without a dot boundary",
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			parsedURL, err := url.Parse(c.rawURL)
			require.NoError(t, err)

			customParsedURL := customURL{
				URL:         parsedURL,
				pagesDomain: c.pagesDomain,
			}

			err = customParsedURL.convertToNamespaceInPath()
			require.ErrorIs(t, err, c.expectedErr)
			if c.expectedErr == nil {
				require.Equal(t, c.expectedHost, customParsedURL.Host)
				require.Equal(t, c.expectedPath, customParsedURL.Path)
			}
		})
	}
}

func TestIsPagesDomain(t *testing.T) {
	cases := []struct {
		rawURL      string
		pagesDomain string
		expected    bool
		description string
	}{
		{
			rawURL:      "https://example.com",
			pagesDomain: "example.com",
			expected:    true,
			description: "Matching pages domain",
		},
		{
			rawURL:      "https://another.com",
			pagesDomain: "example.com",
			expected:    false,
			description: "Non-matching pages domain",
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			parsedURL, err := url.Parse(c.rawURL)
			require.NoError(t, err)

			customParsedURL := customURL{
				URL:         parsedURL,
				pagesDomain: c.pagesDomain,
			}

			result := customParsedURL.isPagesDomain()
			require.Equal(t, c.expected, result)
		})
	}
}
