package acceptance_test

import (
	"io"
	"mime"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func TestContentNegotiation(t *testing.T) {
	host := "content-negotiation.gitlab-example.com"

	t.Setenv(feature.ContentNegotiation.EnvVariable, "true")
	RunPagesProcess(t, withListeners([]ListenSpec{httpListener}))

	tests := map[string]struct {
		path                string
		accept              string
		expectedStatus      int
		expectedMediaType   string
		expectedBodyContain string
		expectVaryAccept    bool
	}{
		"turtle_extensionless": {
			path:                "/resource",
			accept:              "text/turtle",
			expectedStatus:      http.StatusOK,
			expectedMediaType:   "text/turtle",
			expectedBodyContain: `ex:p "turtle"`,
			expectVaryAccept:    true,
		},
		"jsonld_extensionless": {
			path:                "/resource",
			accept:              "application/ld+json",
			expectedStatus:      http.StatusOK,
			expectedMediaType:   "application/ld+json",
			expectedBodyContain: `"name":"jsonld"`,
			expectVaryAccept:    true,
		},
		"rdf_xml_extensionless": {
			path:                "/resource",
			accept:              "application/rdf+xml",
			expectedStatus:      http.StatusOK,
			expectedMediaType:   "application/rdf+xml",
			expectedBodyContain: "<rdf:RDF",
			expectVaryAccept:    true,
		},
		"quality_value_precedence": {
			path:                "/resource",
			accept:              "text/html;q=0.5, text/turtle;q=0.9",
			expectedStatus:      http.StatusOK,
			expectedMediaType:   "text/turtle",
			expectedBodyContain: `ex:p "turtle"`,
			expectVaryAccept:    true,
		},
		"directory_index_negotiation": {
			path:                "/dir/",
			accept:              "text/turtle",
			expectedStatus:      http.StatusOK,
			expectedMediaType:   "text/turtle",
			expectedBodyContain: `ex:p "dir-turtle"`,
			expectVaryAccept:    true,
		},
		"unsupported_accept_falls_back_to_html": {
			path:                "/resource",
			accept:              "application/xml",
			expectedStatus:      http.StatusOK,
			expectedMediaType:   "text/html",
			expectedBodyContain: "resource html",
			expectVaryAccept:    true,
		},
		"missing_accept_serves_html_with_vary": {
			path:                "/resource",
			accept:              "",
			expectedStatus:      http.StatusOK,
			expectedMediaType:   "text/html",
			expectedBodyContain: "resource html",
			expectVaryAccept:    true,
		},
		"explicit_extension_bypasses_negotiation": {
			path:                "/resource.ttl",
			accept:              "text/html",
			expectedStatus:      http.StatusOK,
			expectedMediaType:   "text/turtle",
			expectedBodyContain: `ex:p "turtle"`,
			expectVaryAccept:    false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			headers := http.Header{}
			if tt.accept != "" {
				headers.Set("Accept", tt.accept)
			}

			rsp, err := GetPageFromListenerWithHeaders(t, httpListener, host, tt.path, headers)
			require.NoError(t, err)
			defer testhelpers.Close(t, rsp.Body)

			body, err := io.ReadAll(rsp.Body)
			require.NoError(t, err)

			require.Equal(t, tt.expectedStatus, rsp.StatusCode)

			mt, _, err := mime.ParseMediaType(rsp.Header.Get("Content-Type"))
			require.NoError(t, err)
			require.Equal(t, tt.expectedMediaType, mt)
			require.Contains(t, string(body), tt.expectedBodyContain)

			if tt.expectVaryAccept {
				require.Contains(t, rsp.Header.Values("Vary"), "Accept")
			} else {
				require.NotContains(t, rsp.Header.Values("Vary"), "Accept")
			}
		})
	}

	t.Run("variant_specific_etag_prevents_false_304", func(t *testing.T) {
		ttlRsp, err := GetPageFromListenerWithHeaders(t, httpListener, host, "/resource", http.Header{
			"Accept": []string{"text/turtle"},
		})
		require.NoError(t, err)
		testhelpers.Close(t, ttlRsp.Body)
		require.Equal(t, http.StatusOK, ttlRsp.StatusCode)

		ttlETag := ttlRsp.Header.Get("ETag")
		require.NotEmpty(t, ttlETag)

		// Matching variant returns 304 Not Modified.
		notModifiedRsp, err := GetPageFromListenerWithHeaders(t, httpListener, host, "/resource", http.Header{
			"Accept":        []string{"text/turtle"},
			"If-None-Match": []string{ttlETag},
		})
		require.NoError(t, err)
		testhelpers.Close(t, notModifiedRsp.Body)
		require.Equal(t, http.StatusNotModified, notModifiedRsp.StatusCode)

		// Different variant with the Turtle ETag returns 200 OK and a distinct ETag.
		jsonldRsp, err := GetPageFromListenerWithHeaders(t, httpListener, host, "/resource", http.Header{
			"Accept":        []string{"application/ld+json"},
			"If-None-Match": []string{ttlETag},
		})
		require.NoError(t, err)
		testhelpers.Close(t, jsonldRsp.Body)
		require.Equal(t, http.StatusOK, jsonldRsp.StatusCode)
		require.NotEqual(t, ttlETag, jsonldRsp.Header.Get("ETag"))
	})
}

func TestContentNegotiationDisabledByDefault(t *testing.T) {
	host := "content-negotiation.gitlab-example.com"

	RunPagesProcess(t,
		withListeners([]ListenSpec{httpListener}),
	)

	rsp, err := GetPageFromListenerWithHeaders(t, httpListener, host, "/resource", http.Header{
		"Accept": []string{"text/turtle"},
	})
	require.NoError(t, err)
	defer testhelpers.Close(t, rsp.Body)

	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rsp.StatusCode)
	mt, _, err := mime.ParseMediaType(rsp.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "text/html", mt)
	require.Contains(t, string(body), "resource html")
	require.NotContains(t, rsp.Header.Values("Vary"), "Accept")
}
