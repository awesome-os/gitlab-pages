package ratelimiter

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	logtest "gitlab.com/gitlab-org/labkit/v2/testing/logtest"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

const (
	remoteAddr = "192.168.1.1"
)

var next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
})

func TestMiddlewareWithDifferentLimits(t *testing.T) {
	for tn, tc := range sharedTestCases {
		t.Run(tn, func(t *testing.T) {
			recorder := loggingtest.Record(t)

			rl := New(
				"rate_limiter",
				WithNow(mockNow),
				WithLimitPerSecond(tc.limit),
				WithBurstSize(tc.burstSize),
				WithCloseConnection(true),
			)
			defer rl.Stop()

			handler := rl.Middleware(next)

			for i := 0; i < tc.reqNum; i++ {
				r := requestFor(remoteAddr, "http://gitlab.com")
				response := testhelpers.PerformRequest(t, handler, r)

				if i < tc.burstSize {
					require.Equal(t, http.StatusNoContent, response.StatusCode, "req: %d failed", i)
				} else {
					// requests should fail after reaching tc.perDomainBurstPerSecond because mockNow
					// always returns the same time
					require.Equal(t, http.StatusTooManyRequests, response.StatusCode, "req: %d failed", i)
					require.Contains(t, response.Body, "Too many requests.")
					require.Equal(t, "close", response.Headers.Get("Connection"), "req: %d connection closed", i)
					assertSourceIPLog(t, recorder)
				}
			}
		})
	}
}

func TestMiddlewareDenyRequestsAfterBurst(t *testing.T) {
	recorder := loggingtest.Record(t)

	blocked, cachedEntries, cacheReqs := newTestMetrics(t)

	rl := New(
		"rate_limiter",
		WithCachedEntriesMetric(cachedEntries),
		WithCachedRequestsMetric(cacheReqs),
		WithBlockedCountMetric(blocked),
		WithNow(mockNow),
		WithLimitPerSecond(1),
		WithBurstSize(1),
		WithCloseConnection(true),
	)
	defer rl.Stop()

	// middleware is evaluated in reverse order
	handler := rl.Middleware(next)

	for i := 0; i < 5; i++ {
		r := requestFor(remoteAddr, "http://gitlab.com")
		response := testhelpers.PerformRequest(t, handler, r)

		if i == 0 {
			require.Equal(t, http.StatusNoContent, response.StatusCode)
			continue
		}

		// burst is 1 and limit is 1 per second, all subsequent requests should fail
		require.Equal(t, http.StatusTooManyRequests, response.StatusCode)
		require.Equal(t, "close", response.Headers.Get("Connection"))
		assertSourceIPLog(t, recorder)
	}

	blockedCount := testutil.ToFloat64(blocked.WithLabelValues("rate_limiter"))
	require.Equal(t, float64(4), blockedCount, "blocked count")
	blocked.Reset()

	cachedCount := testutil.ToFloat64(cachedEntries.WithLabelValues("rate_limiter"))
	require.Equal(t, float64(1), cachedCount, "cached count")
	cachedEntries.Reset()

	cacheReqMiss := testutil.ToFloat64(cacheReqs.WithLabelValues("rate_limiter", "miss"))
	require.Equal(t, float64(1), cacheReqMiss, "miss count")
	cacheReqHit := testutil.ToFloat64(cacheReqs.WithLabelValues("rate_limiter", "hit"))
	require.Equal(t, float64(4), cacheReqHit, "hit count")
	cacheReqs.Reset()
}

func TestKeyFunc(t *testing.T) {
	tt := map[string]struct {
		keyFunc                       KeyFunc
		firstRemoteAddr               string
		firstTarget                   string
		secondRemoteAddr              string
		secondTarget                  string
		expectedSecondCode            int
		expectedCloseConnectionHeader string
	}{
		"rejected_by_ip": {
			keyFunc:                       request.GetIPV4orIPV6PrefixWithoutPort,
			firstRemoteAddr:               "10.0.0.1",
			firstTarget:                   "https://domain.gitlab.io",
			secondRemoteAddr:              "10.0.0.1",
			secondTarget:                  "https://different.gitlab.io",
			expectedSecondCode:            http.StatusTooManyRequests,
			expectedCloseConnectionHeader: "close",
		},
		"rejected_by_ip_with_different_port": {
			keyFunc:                       request.GetIPV4orIPV6PrefixWithoutPort,
			firstRemoteAddr:               "10.0.0.1:41000",
			firstTarget:                   "https://domain.gitlab.io",
			secondRemoteAddr:              "10.0.0.1:41001",
			secondTarget:                  "https://different.gitlab.io",
			expectedSecondCode:            http.StatusTooManyRequests,
			expectedCloseConnectionHeader: "close",
		},
		"rejected_by_domain": {
			keyFunc:                       request.GetHostWithoutPort,
			firstRemoteAddr:               "10.0.0.1",
			firstTarget:                   "https://domain.gitlab.io",
			secondRemoteAddr:              "10.0.0.2",
			secondTarget:                  "https://domain.gitlab.io",
			expectedSecondCode:            http.StatusTooManyRequests,
			expectedCloseConnectionHeader: "close",
		},
		"rejected_by_domain_with_different_protocol": {
			keyFunc:                       request.GetHostWithoutPort,
			firstRemoteAddr:               "10.0.0.1",
			firstTarget:                   "https://domain.gitlab.io",
			secondRemoteAddr:              "10.0.0.2",
			secondTarget:                  "http://domain.gitlab.io",
			expectedSecondCode:            http.StatusTooManyRequests,
			expectedCloseConnectionHeader: "close",
		},
		"domain_limiter_allows_same_ip": {
			keyFunc:                       request.GetHostWithoutPort,
			firstRemoteAddr:               "10.0.0.1",
			firstTarget:                   "https://domain.gitlab.io",
			secondRemoteAddr:              "10.0.0.1",
			secondTarget:                  "https://different.gitlab.io",
			expectedSecondCode:            http.StatusNoContent,
			expectedCloseConnectionHeader: "",
		},
		"ip_limiter_allows_same_domain": {
			keyFunc:                       request.GetIPV4orIPV6PrefixWithoutPort,
			firstRemoteAddr:               "10.0.0.1",
			firstTarget:                   "https://domain.gitlab.io",
			secondRemoteAddr:              "10.0.0.2",
			secondTarget:                  "https://domain.gitlab.io",
			expectedSecondCode:            http.StatusNoContent,
			expectedCloseConnectionHeader: "",
		},
	}

	for name, tc := range tt {
		t.Run(name, func(t *testing.T) {
			rl := New(
				"rate_limiter",
				WithNow(mockNow),
				WithLimitPerSecond(1),
				WithBurstSize(1),
				WithKeyFunc(tc.keyFunc),
				WithCloseConnection(true),
			)
			defer rl.Stop()

			handler := rl.Middleware(next)

			r1 := httptest.NewRequest(http.MethodGet, tc.firstTarget, nil)
			r1.RemoteAddr = tc.firstRemoteAddr

			response1 := testhelpers.PerformRequest(t, handler, r1)
			require.Equal(t, http.StatusNoContent, response1.StatusCode)

			r2 := httptest.NewRequest(http.MethodGet, tc.secondTarget, nil)
			r2.RemoteAddr = tc.secondRemoteAddr
			response2 := testhelpers.PerformRequest(t, handler, r2)
			require.Equal(t, tc.expectedSecondCode, response2.StatusCode)
			require.Equal(t, tc.expectedCloseConnectionHeader, response2.Headers.Get("Connection"))
		})
	}
}

// assertSourceIPLog verifies that the most recent log record contains source_ip == remoteAddr.
// Each rate-limited request produces two log entries; the last one carries source_ip.
func assertSourceIPLog(t *testing.T, recorder *logtest.LogRecorder) {
	t.Helper()

	records := recorder.Records()
	require.NotEmpty(t, records, "expected at least one log entry for rate-limited request")

	// The last record is the "request hit rate limit" entry which includes source_ip.
	lastRecord := records[len(records)-1]
	var sourceIP string
	for _, attr := range lastRecord.Attrs {
		if attr.Key == "source_ip" {
			sourceIP = attr.Value.String()
			break
		}
	}
	require.Equal(t, remoteAddr, sourceIP, "expected source_ip in last log entry")
	require.True(t, slices.ContainsFunc(lastRecord.Attrs, slog.String("host", "gitlab.com").Equal))
}

func newTestMetrics(t *testing.T) (*prometheus.GaugeVec, *prometheus.GaugeVec, *prometheus.CounterVec) {
	t.Helper()

	blockedGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: t.Name(),
		},
		[]string{"limit_name"},
	)

	cachedEntries := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: t.Name(),
	}, []string{"op"})

	cacheReqs := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: t.Name(),
	}, []string{"op", "cache"})

	return blockedGauge, cachedEntries, cacheReqs
}
