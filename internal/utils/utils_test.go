package utils

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsDomainURL(t *testing.T) {
	tests := map[string]struct {
		url          string
		expectedBool bool
	}{
		"only_path": {
			url:          "/goto.html",
			expectedBool: false,
		},
		"valid_domain_url": {
			url:          "https://GitLab.com",
			expectedBool: true,
		},
		"schemaless_domain_url_with_special_char": {
			url:          "/\\GitLab.com",
			expectedBool: false,
		},
		"schemaless_domain_url": {
			url:          "//GitLab.com/pages.html",
			expectedBool: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.EqualValues(t, tt.expectedBool, IsDomainURL(tt.url))
		})
	}
}

func TestMatchHost(t *testing.T) {
	tests := map[string]struct {
		originalURL  string
		path         string
		expectedBool bool
		expectedPath string
	}{
		"path_without_domain": {
			originalURL:  "https://GitLab.com/goto.html",
			path:         "/goto.html",
			expectedBool: true,
			expectedPath: "/goto.html",
		},
		"valid_matching_host": {
			originalURL:  "https://GitLab.com/goto.html",
			path:         "https://GitLab.com/goto.html",
			expectedBool: true,
			expectedPath: "/goto.html",
		},
		"different_schema_path": {
			originalURL:  "http://GitLab.com/goto.html",
			path:         "https://GitLab.com/goto.html",
			expectedBool: false,
			expectedPath: "",
		},
		"different_host_path": {
			originalURL:  "https://GitLab.com/goto.html",
			path:         "https://GitLab-test.com/goto.html",
			expectedBool: false,
			expectedPath: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			parsedURL, err := url.Parse(tt.originalURL)
			require.NoError(t, err)

			hostMatches, path := MatchHost(parsedURL, tt.path)
			require.EqualValues(t, tt.expectedBool, hostMatches)
			require.EqualValues(t, tt.expectedPath, path)
		})
	}
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name     string
		urlStr   string
		expected *url.URL
		valid    bool
	}{
		{
			name:     "Empty string",
			urlStr:   "",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Invalid URL",
			urlStr:   "ht@tp://invalid-url",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Missing scheme",
			urlStr:   "www.example.com",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Missing host",
			urlStr:   "http://",
			expected: nil,
			valid:    false,
		},
		{
			name:   "Valid URL",
			urlStr: "http://www.example.com",
			expected: &url.URL{
				Scheme: "http",
				Host:   "www.example.com",
			},
			valid: true,
		},
		{
			name:   "Valid URL with path",
			urlStr: "https://www.example.com/path",
			expected: &url.URL{
				Scheme: "https",
				Host:   "www.example.com",
				Path:   "/path",
			},
			valid: true,
		},
		{
			name:   "Valid URL with query",
			urlStr: "https://www.example.com/path?query=1",
			expected: &url.URL{
				Scheme:   "https",
				Host:     "www.example.com",
				Path:     "/path",
				RawQuery: "query=1",
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, valid := ParseURL(tt.urlStr)
			if tt.valid {
				require.True(t, valid)
				require.NotNil(t, parsedURL)
				require.Equal(t, tt.expected.Scheme, parsedURL.Scheme)
				require.Equal(t, tt.expected.Host, parsedURL.Host)
				require.Equal(t, tt.expected.Path, parsedURL.Path)
				require.Equal(t, tt.expected.RawQuery, parsedURL.RawQuery)
			} else {
				require.False(t, valid)
				require.Nil(t, parsedURL)
			}
		})
	}
}
