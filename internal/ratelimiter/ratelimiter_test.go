package ratelimiter

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/3th1nk/cidr"
	"github.com/stretchr/testify/require"
)

var (
	now          = "2021-09-13T15:00:00Z"
	validTime, _ = time.Parse(time.RFC3339, now)
)

func mockNow() time.Time {
	return validTime
}

var sharedTestCases = map[string]struct {
	limit     float64
	burstSize int
	reqNum    int
}{
	"one_request_per_second": {
		limit:     1,
		burstSize: 1,
		reqNum:    2,
	},
	"one_request_per_second_but_big_bucket": {
		limit:     1,
		burstSize: 10,
		reqNum:    11,
	},
	"three_req_per_second_bucket_size_one": {
		limit:     3,
		burstSize: 1, // max burst 1 means 1 at a time
		reqNum:    3,
	},
	"10_requests_per_second": {
		limit:     10,
		burstSize: 10,
		reqNum:    11,
	},
}

func TestRequestAllowed(t *testing.T) {
	t.Parallel()

	for tn, tc := range sharedTestCases {
		t.Run(tn, func(t *testing.T) {
			rl := New(
				"rate_limiter",
				WithNow(mockNow),
				WithLimitPerSecond(tc.limit),
				WithBurstSize(tc.burstSize),
			)
			defer rl.Stop()

			for i := 0; i < tc.reqNum; i++ {
				r := requestFor("172.16.123.1", "https://domain.gitlab.io")

				got := rl.requestAllowed(r)
				if i < tc.burstSize {
					require.Truef(t, got, "expected true for request no. %d", i)
				} else {
					// requests should fail after reaching tc.burstSize because mockNow
					// always returns the same time
					require.False(t, got, "expected false for request no. %d", i)
				}
			}
		})
	}
}

func TestBypassRateLimiter(t *testing.T) {
	var limit float64 = 10
	burstSize := 10
	reqNum := 11

	type ipAndShouldBypass struct {
		ip           string
		shouldBypass bool
	}

	testCases := []struct {
		name               string
		bypassCIDRs        []string
		ipsAndShouldBypass []ipAndShouldBypass
	}{
		{
			name:        "single_cidr_within_and_outside",
			bypassCIDRs: []string{"172.16.123.1/24"},
			ipsAndShouldBypass: []ipAndShouldBypass{
				{"172.16.123.1", true},
				{"172.16.123.50", true},
				{"172.16.2.2", false},
				{"10.0.0.1", false},
			},
		},
		{
			name:        "multiple_cidrs_within_and_outside",
			bypassCIDRs: []string{"172.16.123.1/24", "192.168.1.1/24"},
			ipsAndShouldBypass: []ipAndShouldBypass{
				{"192.168.1.1", true},
				{"192.168.1.50", true},
				{"192.168.2.1", false},
				{"172.16.123.50", true},
				{"172.16.2.2", false},
				{"10.0.0.1", false},
			},
		},
		{
			name:        "ipv6_single_cidr_within_and_outside",
			bypassCIDRs: []string{"2001:db8::/32"},
			ipsAndShouldBypass: []ipAndShouldBypass{
				{"2001:db8::1", true},
				{"[2001:db8::1]:80", true},
				{"2001:db8::1234", true},
				{"2001:db9::1", false}},
		},
		{
			name:        "ipv6_multiple_cidrs_within_and_outside",
			bypassCIDRs: []string{"2001:db8::/32", "2001:0db9::/32"},
			ipsAndShouldBypass: []ipAndShouldBypass{
				{"2001:db8::1", true},
				{"[2001:db8::1]:80", true},
				{"2001:db8::1234", true},
				{"2001:0db9::1", true},
				{"2001:0db9::1:1", true},
				{"2001:0dba::1", false}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var bypassCIDRs []cidr.CIDR
			for _, cidrStr := range tc.bypassCIDRs {
				c, err := cidr.Parse(cidrStr)
				require.NoError(t, err)
				bypassCIDRs = append(bypassCIDRs, *c)
			}

			rl := New(
				"rate_limiter",
				WithNow(mockNow),
				WithLimitPerSecond(limit),
				WithBurstSize(burstSize),
				WithBypassCIDRs(bypassCIDRs),
			)
			defer rl.Stop()

			for _, ip := range tc.ipsAndShouldBypass {
				for i := 0; i < reqNum; i++ {
					r := requestFor(ip.ip, "https://domain.gitlab.io")

					got := rl.requestAllowed(r)
					if ip.shouldBypass {
						require.Truef(t, got, "expected true for request no. %d inside bypass CIDR for ip %s", i, ip.ip)
					} else {
						if i < burstSize {
							require.Truef(t, got, "expected true for request no. %d, ip %s", i, ip.ip)
						} else {
							require.Falsef(t, got, "expected false for request no. %d outside burst size for ip %s", i, ip.ip)
						}
					}
				}
			}
		})
	}
}

func TestSingleRateLimiterWithMultipleSourceIPs(t *testing.T) {
	now := time.Now()

	rl := New(
		"rate_limiter",
		WithLimitPerSecond(1),
		WithBurstSize(1),
		WithNow(func() time.Time {
			return now
		}),
	)
	defer rl.Stop()

	testRequest := func(ip string, i int) {
		r := requestFor(ip, "https://domain.gitlab.io")
		got := rl.requestAllowed(r)
		require.Truef(t, got, "expected true for %v request no. %d", ip, i)
	}

	for i := 0; i < 5; i++ {
		testRequest("172.16.123.10", i)
		testRequest("172.16.123.20", i)
		testRequest("172.16.123.30", i)
		now = now.Add(time.Second)
	}
}

func requestFor(remoteAddr, domain string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, domain, nil)
	r.RemoteAddr = remoteAddr
	return r
}
