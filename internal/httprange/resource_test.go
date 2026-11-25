package httprange

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type failingTransport struct {
	err error
}

func (t failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

func urlValue(url string) atomic.Value {
	v := atomic.Value{}
	v.Store(url)
	return v
}

func TestNewResourceCleansTransportErrorURL(t *testing.T) {
	client := &http.Client{Transport: failingTransport{err: errors.New("request failed")}}

	_, err := NewResource(context.Background(), "https://user:password@example.com/archive.zip?token=token-secret#fragment-secret", client)

	require.EqualError(t, err, `Get "https://example.com/archive.zip": request failed`)
}

func TestNewResource(t *testing.T) {
	resource := &Resource{
		url:          urlValue("/some/resource"),
		ETag:         "etag",
		LastModified: "Wed, 21 Oct 2015 07:28:00 GMT",
		Size:         1,
	}

	tests := map[string]struct {
		url            string
		status         int
		contentRange   string
		want           *Resource
		expectedErrMsg string
	}{
		"status_ok": {
			url:    "/some/resource",
			status: http.StatusOK,
			want:   resource,
		},
		"status_partial_content_success": {
			url:          "/some/resource",
			status:       http.StatusPartialContent,
			contentRange: "bytes 200-1000/67589",
			want: &Resource{
				url:          urlValue("/some/resource"),
				ETag:         "etag",
				LastModified: "Wed, 21 Oct 2015 07:28:00 GMT",
				Size:         67589,
			},
		},
		"status_partial_content_invalid_content_range": {
			url:            "/some/resource",
			status:         http.StatusPartialContent,
			contentRange:   "invalid",
			expectedErrMsg: "invalid `Content-Range`:",
			want:           resource,
		},
		"status_partial_content_content_range_not_a_number": {
			url:            "/some/resource",
			status:         http.StatusPartialContent,
			contentRange:   "bytes 200-1000/notanumber",
			expectedErrMsg: "invalid `Content-Range`:",
			want:           resource,
		},
		"StatusRequestedRangeNotSatisfiable": {
			url:            "/some/resource",
			status:         http.StatusRequestedRangeNotSatisfiable,
			expectedErrMsg: ErrRangeRequestsNotSupported.Error(),
			want:           resource,
		},
		"not_found": {
			url:            "/some/resource",
			status:         http.StatusNotFound,
			expectedErrMsg: ErrNotFound.Error(),
			want:           resource,
		},
		"invalid_url": {
			url:            "/%",
			expectedErrMsg: "invalid URL escape",
			want:           resource,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", tt.want.ETag)
				w.Header().Set("Last-Modified", tt.want.LastModified)
				w.Header().Set("Content-Range", tt.contentRange)
				w.WriteHeader(tt.status)
				w.Write([]byte("1"))
			}))
			defer testServer.Close()

			got, err := NewResource(context.Background(), testServer.URL+tt.url, testClient)
			if tt.expectedErrMsg != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErrMsg)
				return
			}

			require.NoError(t, err)
			require.Contains(t, got.URL(), tt.want.URL())
			require.Equal(t, tt.want.LastModified, got.LastModified)
			require.Equal(t, tt.want.ETag, got.ETag)
			require.Equal(t, tt.want.Size, got.Size)
		})
	}
}
