package httptransport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func TestMeteredRoundTripperLogsSafeRequestAndResponseData(t *testing.T) {
	buf := loggingtest.Capture(t, logging.LevelTrace)

	req, err := http.NewRequest(http.MethodGet, "https://user:password@example.com/archive.zip?token=request-secret#fragment-secret", nil)
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode:    http.StatusPartialContent,
		ContentLength: 42,
		Header: http.Header{
			headerAcceptRanges: {"bytes"},
			headerContentRange: {"bytes 0-41/42"},
			headerContentType:  {"application/zip"},
			headerRequestID:    {"upstream-request-id"},
			"Location":         {"https://example.com?token=location-secret"},
			"Set-Cookie":       {"session=cookie-secret"},
			"X-Custom-Token":   {"header-secret"},
		},
	}

	mrt := &meteredRoundTripper{name: "zip_vfs"}
	mrt.logResponse(req, resp)

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.Equal(t, "https://example.com/archive.zip", record["req_url"])
	require.Equal(t, map[string]any{
		headerAcceptRanges:  []any{"bytes"},
		headerContentLength: []any{"42"},
		headerContentRange:  []any{"bytes 0-41/42"},
		headerContentType:   []any{"application/zip"},
		headerRequestID:     []any{"upstream-request-id"},
	}, record["res_headers"])
	require.NotContains(t, buf.String(), "secret")
}

func TestReconfigureMeteredRoundTripper(t *testing.T) {
	histVec, counterVec := newTestMetrics(t)
	transport := NewTransport()
	transport.RegisterProtocol("file", http.NewFileTransport(http.Dir(".")))

	mrt := NewMeteredRoundTripper(transport, t.Name(), nil, histVec, counterVec, time.Millisecond)

	r := httptest.NewRequest("GET", "file:///testdata/file.html", nil)

	res, err := mrt.RoundTrip(r)
	require.NoError(t, err)
	testhelpers.Close(t, res.Body)

	require.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	require.Equal(t, "httptransport/testdata/file.html\n", string(body))

	// make sure counter still works
	statusCode := strconv.Itoa(res.StatusCode)
	counterCount := testutil.ToFloat64(counterVec.WithLabelValues(statusCode))
	require.Equal(t, float64(1), counterCount, statusCode)
}
