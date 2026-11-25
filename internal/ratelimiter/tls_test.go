package ratelimiter

import (
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/3th1nk/cidr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
)

func newRateLimiter(t *testing.T,
	tlsTestData TLSTestData,
	keyFunc func(*tls.ClientHelloInfo) string,
	bypassCIDRs []string) (*RateLimiter, *prometheus.GaugeVec, *prometheus.GaugeVec, *prometheus.CounterVec) {
	t.Helper()

	var cidrs []cidr.CIDR
	for _, cidrStr := range bypassCIDRs {
		c, err := cidr.Parse(cidrStr)
		require.NoError(t, err)
		cidrs = append(cidrs, *c)
	}

	blocked, cachedEntries, cacheReqs := newTestMetrics(t)
	rl := New("limit_name",
		WithCachedEntriesMetric(cachedEntries),
		WithCachedRequestsMetric(cacheReqs),
		WithBlockedCountMetric(blocked),
		WithNow(stubNow()),
		WithLimitPerSecond(tlsTestData.limitPerSecond),
		WithBurstSize(tlsTestData.burst),
		WithTLSKeyFunc(keyFunc),
		WithBypassCIDRs(cidrs),
	)

	return rl, blocked, cachedEntries, cacheReqs
}

func TestTLSHostnameKey(t *testing.T) {
	info := &tls.ClientHelloInfo{ServerName: "group.gitlab.io"}

	require.Equal(t, "group.gitlab.io", TLSHostnameKey(info))
}

func TestTLSClientIPKey(t *testing.T) {
	tests := []struct {
		addr     string
		expected string
	}{
		{
			"10.1.2.3:1234",
			"10.1.2.3",
		},
		{
			"[2001:db8:3333:4444:5555:6666:7777:8888]:1234",
			"2001:db8:3333:4444::/64",
		},
	}

	for _, tt := range tests {
		addr, err := net.ResolveTCPAddr("tcp", tt.addr)
		require.NoError(t, err)
		conn := stubConn{remoteAddr: addr}
		info := &tls.ClientHelloInfo{Conn: conn}

		require.Equal(t, tt.expected, TLSClientIPKey(info))
	}
}

type TLSTestData struct {
	useHostnameAsKey bool
	limitPerSecond   float64
	burst            int
	successfulReqCnt int
	bypassCIDRs      []string
	shouldBypass     bool
}

var testCases = map[string]TLSTestData{
	"ip_limiter": {
		useHostnameAsKey: false,
		limitPerSecond:   0.1,
		burst:            5,
		successfulReqCnt: 5,
	},
	"hostname_limiter": {
		useHostnameAsKey: true,
		limitPerSecond:   0.1,
		burst:            5,
		successfulReqCnt: 5,
	},
	"disabled": {
		useHostnameAsKey: false,
		limitPerSecond:   0,
		burst:            5,
		successfulReqCnt: 10,
	},
	"slowly_approach_limit": {
		useHostnameAsKey: false,
		limitPerSecond:   0.2,
		burst:            5,
		successfulReqCnt: 6, // 5 * 0.2 gives another 1 request
	},
	"ip_limiter_with_bypass": {
		useHostnameAsKey: false,
		limitPerSecond:   0.1,
		burst:            5,
		successfulReqCnt: 5,
		bypassCIDRs:      []string{"10.1.2.3/24"},
		shouldBypass:     true,
	},
	"hostname_limiter_with_bypass": {
		useHostnameAsKey: true,
		limitPerSecond:   0.1,
		burst:            5,
		successfulReqCnt: 5,
		bypassCIDRs:      []string{"10.1.2.3/24"},
		shouldBypass:     true,
	},
	"disabled_with_bypass": {
		useHostnameAsKey: false,
		limitPerSecond:   0,
		burst:            5,
		successfulReqCnt: 10,
		bypassCIDRs:      []string{"10.1.2.3/24"},
		shouldBypass:     true,
	},
	"slowly_approach_limit_with_bypass": {
		useHostnameAsKey: false,
		limitPerSecond:   0.2,
		burst:            5,
		successfulReqCnt: 6, // 5 * 0.2 gives another 1 request
		bypassCIDRs:      []string{"10.1.2.3/24"},
		shouldBypass:     true,
	},
	"ip_limiter_should_not_bypass": {
		useHostnameAsKey: false,
		limitPerSecond:   0.1,
		burst:            5,
		successfulReqCnt: 5,
		bypassCIDRs:      []string{"20.1.2.3/24"},
		shouldBypass:     false,
	},
	"hostname_limiter_should_not_bypass": {
		useHostnameAsKey: true,
		limitPerSecond:   0.1,
		burst:            5,
		successfulReqCnt: 5,
		bypassCIDRs:      []string{"20.1.2.3/24"},
		shouldBypass:     false,
	},
	"disabled_should_not_bypass": {
		useHostnameAsKey: false,
		limitPerSecond:   0,
		burst:            5,
		successfulReqCnt: 10,
		bypassCIDRs:      []string{"20.1.2.3/24"},
		shouldBypass:     false,
	},
	"slowly_approach_limit_should_not_bypass": {
		useHostnameAsKey: false,
		limitPerSecond:   0.2,
		burst:            5,
		successfulReqCnt: 6, // 5 * 0.2 gives another 1 request
		bypassCIDRs:      []string{"20.1.2.3/24"},
		shouldBypass:     false,
	},
}

func TestGetCertificateMiddleware(t *testing.T) {
	expectedCert := &tls.Certificate{}
	expectedErr := errors.New("expected error")

	getCertificate := func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return expectedCert, expectedErr
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			recorder := loggingtest.Record(t)

			keyFunc := TLSClientIPKey
			if tt.useHostnameAsKey {
				keyFunc = TLSHostnameKey
			}

			rl, blocked, cachedEntries, cacheReqs := newRateLimiter(t, tt, keyFunc, tt.bypassCIDRs)
			defer rl.Stop()

			middlewareGetCert := rl.GetCertificateMiddleware(getCertificate)

			addr, err := net.ResolveTCPAddr("tcp", "10.1.2.3:12345")
			require.NoError(t, err)
			conn := stubConn{remoteAddr: addr}
			info := &tls.ClientHelloInfo{Conn: conn, ServerName: "group.gitlab.io"}

			for i := 0; i < tt.successfulReqCnt; i++ {
				cert, err := middlewareGetCert(info)
				require.Equal(t, expectedCert, cert)
				require.Equal(t, expectedErr, err)
			}

			// When rate-limiter disabled altogether
			if tt.limitPerSecond <= 0 {
				return
			}

			cert, err := middlewareGetCert(info)
			if tt.shouldBypass {
				require.Equal(t, expectedCert, cert)
				require.Equal(t, expectedErr, err)
			} else {
				require.Nil(t, cert)
				require.Equal(t, err, ErrTLSRateLimited)

				records := recorder.Records()
				require.NotEmpty(t, records, "expected at least one log entry for rate-limited TLS connection")
				lastRecord := records[len(records)-1]
				require.Equal(t, "TLS connection rate-limited", lastRecord.Message)

				attrMap := make(map[string]any)
				for _, attr := range lastRecord.Attrs {
					attrMap[attr.Key] = attr.Value.Any()
				}
				require.Equal(t, "limit_name", attrMap["rate_limiter_name"])
				require.Equal(t, "10.1.2.3", attrMap["source_ip"])
				require.Equal(t, "group.gitlab.io", attrMap["req_host"])
				require.InDelta(t, tt.limitPerSecond, attrMap["rate_limiter_limit_per_second"], 0.0001)
				require.Equal(t, int64(tt.burst), attrMap["rate_limiter_burst_size"])
			}

			// make another request with different key and expect success
			if tt.useHostnameAsKey {
				info.ServerName = "another-group.gitlab.io"
			} else {
				addr, err := net.ResolveTCPAddr("tcp", "10.10.20.30:12345")
				require.NoError(t, err)
				conn = stubConn{remoteAddr: addr}
				info.Conn = conn
			}

			cert, err = middlewareGetCert(info)
			require.Equal(t, expectedCert, cert)
			require.Equal(t, expectedErr, err)

			blockedCount := testutil.ToFloat64(blocked.WithLabelValues("limit_name"))
			if tt.shouldBypass {
				require.Equal(t, float64(0), blockedCount, "blocked count")
			} else {
				require.Equal(t, float64(1), blockedCount, "blocked count")
			}

			cachedCount := testutil.ToFloat64(cachedEntries.WithLabelValues("limit_name"))
			require.Equal(t, float64(2), cachedCount, "cached count") // 1 for first key + 1 for different one

			cacheReqMiss := testutil.ToFloat64(cacheReqs.WithLabelValues("limit_name", "miss"))
			require.Equal(t, float64(2), cacheReqMiss, "miss count") // 1 for first key + 1 for different one
			cacheReqHit := testutil.ToFloat64(cacheReqs.WithLabelValues("limit_name", "hit"))
			require.Equal(t, float64(tt.successfulReqCnt), cacheReqHit, "hit count")
		})
	}
}

func stubNow() func() time.Time {
	now := time.Now()
	return func() time.Time {
		now = now.Add(time.Second)

		return now
	}
}
