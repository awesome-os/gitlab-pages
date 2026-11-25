package urilimiter

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab/client"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func TestNewMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	})

	tests := map[string]struct {
		limit          int
		url            string
		emptyHost      bool
		expectedStatus int
	}{
		"with_disabled_middleware": {
			limit:          0,
			url:            "/index.html",
			expectedStatus: http.StatusOK,
		},
		"with_limit_set_to_request_length": {
			limit:          17,
			url:            "/index.html?q=a#b",
			expectedStatus: http.StatusOK,
		},
		"with_uri_length_exceeding_the_limit": {
			limit:          17,
			url:            "/index1.html?q=a#b",
			expectedStatus: http.StatusRequestURITooLong,
		},
		"with_uri_length_exceeding_the_limit_with_query": {
			limit:          17,
			url:            "/index.html?q=aa#b",
			expectedStatus: http.StatusRequestURITooLong,
		},
		"with_uri_length_exceeding_the_limit_with_fragment": {
			limit:          17,
			url:            "/index.html?q=a#bb",
			expectedStatus: http.StatusRequestURITooLong,
		},
		"with_hostname_too_long": {
			limit:          1024, // This limit is not referred here
			url:            "http://" + strings.Repeat("a", client.FQDNMaxLength+1) + ".com/index.html",
			expectedStatus: http.StatusNotFound,
		},
		"with_hostname_label_too_long": {
			limit:          1024, // This limit is not referred here
			url:            "http://" + strings.Repeat("a", client.DNSLabelMaxLength+1) + ".com/index.html",
			expectedStatus: http.StatusNotFound,
		},
		"with_invalid_hostname": {
			limit:          1024, // This limit is not referred here
			url:            "http://example!.com/index.html",
			expectedStatus: http.StatusNotFound,
		},
		// An HTTP/1.0 request may omit the Host header entirely.
		"with_empty_hostname": {
			limit:          1024, // This limit is not referred here
			url:            "/index.html",
			emptyHost:      true,
			expectedStatus: http.StatusNotFound,
		},
	}

	for tn, tt := range tests {
		t.Run(tn, func(t *testing.T) {
			middleware := NewMiddleware(handler, tt.limit)

			ww := httptest.NewRecorder()
			rr := httptest.NewRequest(http.MethodGet, tt.url, nil)
			if tt.emptyHost {
				rr.Host = ""
			}

			middleware.ServeHTTP(ww, rr)

			res := ww.Result()
			testhelpers.Close(t, res.Body)

			require.Equal(t, tt.expectedStatus, res.StatusCode)
			if tt.expectedStatus == http.StatusOK {
				b, err := io.ReadAll(res.Body)
				require.NoError(t, err)

				require.Equal(t, "hello", string(b))
			}
		})
	}
}
