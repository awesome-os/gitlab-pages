package acceptance_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type listener struct {
	listener     ListenSpec
	header       http.Header
	clientIP     string
	byPassCIDR   string
	shouldBypass bool
}

var rateLimitedListeners = map[string]listener{
	"http_listener_shouldBypass_false": {
		listener:     httpListener,
		clientIP:     "127.0.0.1",
		byPassCIDR:   "127.1.0.1/24",
		shouldBypass: false,
	},
	"https_listener_shouldBypass_false": {
		listener:     httpsListener,
		clientIP:     "127.0.0.1",
		byPassCIDR:   "127.1.0.1/24",
		shouldBypass: false,
	},
	"proxy_listener_shouldBypass_false": {
		listener: proxyListener,
		header: http.Header{
			"X-Forwarded-For":  []string{"172.16.123.1"},
			"X-Forwarded-Host": []string{"group.gitlab-example.com"},
		},
		clientIP:     "172.16.123.1",
		byPassCIDR:   "172.20.123.1/24",
		shouldBypass: false,
	},
	"proxyv2_listener_shouldBypass_false": {
		listener:     httpsProxyv2Listener,
		clientIP:     "10.1.1.1",
		byPassCIDR:   "10.2.1.1/24",
		shouldBypass: false,
	},
}

var nipRateLimitedListeners = map[string]listener{
	"http_listener_shouldBypass_false": {
		listener:     httpListener,
		clientIP:     "127.0.0.1",
		byPassCIDR:   "127.1.0.1/24",
		shouldBypass: false,
	},
	"https_listener_shouldBypass_false": {
		listener:     httpsListener,
		clientIP:     "127.0.0.1",
		byPassCIDR:   "127.1.0.1/24",
		shouldBypass: false,
	},
	"proxy_listener_shouldBypass_false": {
		listener: proxyListener,
		header: http.Header{
			"X-Forwarded-For":  []string{"172.16.123.1"},
			"X-Forwarded-Host": []string{"gitlab-example.com"},
		},
		clientIP:     "172.16.123.1",
		byPassCIDR:   "172.20.123.1/24",
		shouldBypass: false,
	},
	"proxyv2_listener_shouldBypass_false": {
		listener:     httpsProxyv2Listener,
		clientIP:     "10.1.1.1",
		byPassCIDR:   "10.2.1.1/24",
		shouldBypass: false,
	},
}

func runIPRateLimitsTests(t *testing.T, host, path string, limitedListeners map[string]listener, extraArgs ...processOption) {
	for name, tc := range limitedListeners {
		t.Run(name, func(t *testing.T) {
			rateLimit := 5

			args := []processOption{
				withListeners([]ListenSpec{tc.listener}),
				withExtraArgument("rate-limit-source-ip", fmt.Sprint(rateLimit)),
				withExtraArgument("rate-limit-source-ip-burst", fmt.Sprint(rateLimit)),
			}
			if len(tc.byPassCIDR) > 0 {
				extraArgs = append(extraArgs, withExtraArgument("rate-limit-subnets-allow-list", tc.byPassCIDR))
			}
			logBuf := RunPagesProcess(t, append(args, extraArgs...)...)

			spec := currentListener(tc.listener)
			for i := 0; i < 10; i++ {
				rsp, err := GetPageFromListenerWithHeaders(t, spec, host, path, tc.header)
				require.NoError(t, err)
				require.NoError(t, rsp.Body.Close())

				if tc.shouldBypass {
					require.Equal(t, http.StatusOK, rsp.StatusCode, "cidr should bypass request: %d failed", i)
				} else {
					if i >= rateLimit {
						require.Equal(t, http.StatusTooManyRequests, rsp.StatusCode, host+"/"+path+" request: %d failed", i)
						assertLogFound(t, logBuf, []string{"request hit rate limit", "\"source_ip\":\"" + tc.clientIP + "\""})
					} else {
						require.Equal(t, http.StatusOK, rsp.StatusCode, "request: %d failed", i)
					}
				}
			}
		})
	}
}

func TestIPRateLimits(t *testing.T) {
	runIPRateLimitsTests(t, "group.gitlab-example.com", "project/", rateLimitedListeners)
}

func TestNamespaceInPathEnabled_IPRateLimits(t *testing.T) {
	runIPRateLimitsTests(t,
		"gitlab-example.com",
		"group/project/",
		nipRateLimitedListeners,
		withExtraArgument("namespace-in-path", "true"),
	)
}

var rateLimitedListenersWithBypass = map[string]listener{
	"http_listener_shouldBypass_true": {
		listener:     httpListener,
		clientIP:     "127.0.0.1",
		byPassCIDR:   "127.0.0.1/24",
		shouldBypass: true,
	},
	"https_listener_shouldBypass_true": {
		listener:     httpsListener,
		clientIP:     "127.0.0.1",
		byPassCIDR:   "127.0.0.1/24",
		shouldBypass: true,
	},
	"proxy_listener_shouldBypass_true": {
		listener: proxyListener,
		header: http.Header{
			"X-Forwarded-For":  []string{"172.16.123.1"},
			"X-Forwarded-Host": []string{"group.gitlab-example.com"},
		},
		clientIP:     "172.16.123.1",
		byPassCIDR:   "172.16.123.1/24",
		shouldBypass: true,
	},
	"proxyv2_listener_shouldBypass_true": {
		listener:     httpsProxyv2Listener,
		clientIP:     "10.1.1.1",
		byPassCIDR:   "10.1.1.1/24",
		shouldBypass: true,
	},
}

var nipRateLimitedListenersWithBypass = map[string]listener{
	"http_listener_shouldBypass_true": {
		listener:     httpListener,
		clientIP:     "127.0.0.1",
		byPassCIDR:   "127.0.0.1/24",
		shouldBypass: true,
	},
	"https_listener_shouldBypass_true": {
		listener:     httpsListener,
		clientIP:     "127.0.0.1",
		byPassCIDR:   "127.0.0.1/24",
		shouldBypass: true,
	},
	"proxy_listener_shouldBypass_true": {
		listener: proxyListener,
		header: http.Header{
			"X-Forwarded-For":  []string{"172.16.123.1"},
			"X-Forwarded-Host": []string{"gitlab-example.com"},
		},
		clientIP:     "172.16.123.1",
		byPassCIDR:   "172.16.123.1/24",
		shouldBypass: true,
	},
	"proxyv2_listener_shouldBypass_true": {
		listener:     httpsProxyv2Listener,
		clientIP:     "10.1.1.1",
		byPassCIDR:   "10.1.1.1/24",
		shouldBypass: true,
	},
}

func TestIPRateLimitsWithBypass(t *testing.T) {
	runIPRateLimitsTests(t, "group.gitlab-example.com", "project/", rateLimitedListenersWithBypass)
}

func TestNamespaceInPathEnabled_IPRateLimitsWithBypass(t *testing.T) {
	runIPRateLimitsTests(t,
		"gitlab-example.com",
		"group/project/",
		nipRateLimitedListenersWithBypass,
		withExtraArgument("namespace-in-path", "true"),
	)
}

func runDomainRateLimitsTests(t *testing.T, host, path, otherHost, otherPath string, limitedListeners map[string]listener, extraArgs ...processOption) {
	for name, tc := range limitedListeners {
		t.Run(name, func(t *testing.T) {
			rateLimit := 5

			args := []processOption{
				withListeners([]ListenSpec{tc.listener}),
				withExtraArgument("rate-limit-domain", fmt.Sprint(rateLimit)),
				withExtraArgument("rate-limit-domain-burst", fmt.Sprint(rateLimit)),
			}
			if len(tc.byPassCIDR) > 0 {
				extraArgs = append(extraArgs, withExtraArgument("rate-limit-subnets-allow-list", tc.byPassCIDR))
			}
			logBuf := RunPagesProcess(t, append(args, extraArgs...)...)

			spec := currentListener(tc.listener)
			for i := 0; i < 10; i++ {
				rsp, err := GetPageFromListenerWithHeaders(t, spec, host, path, tc.header)
				require.NoError(t, err)
				require.NoError(t, rsp.Body.Close())

				if tc.shouldBypass {
					require.Equal(t, http.StatusOK, rsp.StatusCode, "cidr should bypass request: %d failed", i)
				} else {
					if i >= rateLimit {
						require.Equal(t, http.StatusTooManyRequests, rsp.StatusCode, "group.gitlab-example.com request: %d failed", i)
						assertLogFound(t, logBuf, []string{"request hit rate limit", "\"source_ip\":\"" + tc.clientIP + "\""})
					} else {
						require.Equal(t, http.StatusOK, rsp.StatusCode, "request: %d failed", i)
					}
				}
			}

			// make sure that requests to other domains are passing
			rsp, err := GetPageFromListener(t, spec, otherHost, otherPath)
			require.NoError(t, err)
			require.NoError(t, rsp.Body.Close())

			require.Equal(t, http.StatusOK, rsp.StatusCode, "request to unrelated domain failed")
		})
	}
}

func TestDomainRateLimits(t *testing.T) {
	runDomainRateLimitsTests(t,
		"group.gitlab-example.com",
		"project/",
		"CapitalGroup.gitlab-example.com",
		"project/",
		rateLimitedListeners,
	)
}

func TestNamespaceInPathEnabled_DomainRateLimits(t *testing.T) {
	runDomainRateLimitsTests(t,
		"gitlab-example.com",
		"group/project/",
		"gitlab-example.com",
		"CapitalGroup/project/",
		nipRateLimitedListeners,
		withExtraArgument("namespace-in-path", "true"),
	)
}

func TestDomainRateLimitsWithBypass(t *testing.T) {
	runDomainRateLimitsTests(t,
		"group.gitlab-example.com",
		"project/",
		"CapitalGroup.gitlab-example.com",
		"project/",
		rateLimitedListenersWithBypass,
	)
}

func TestNamespaceInPathEnabled_DomainRateLimitsWithBypass(t *testing.T) {
	runDomainRateLimitsTests(t,
		"gitlab-example.com",
		"group/project/",
		"gitlab-example.com",
		"CapitalGroup/project/",
		nipRateLimitedListenersWithBypass,
		withExtraArgument("namespace-in-path", "true"),
	)
}

type tlsListener struct {
	spec         ListenSpec
	domainLimit  bool
	sourceIP     string
	byPassCIDR   string
	shouldBypass bool
}

var tlsRateLimitedListeners = map[string]tlsListener{
	"https_with_domain_limit": {
		spec:        httpsListener,
		domainLimit: true,
		sourceIP:    "127.0.0.1",
	},
	"https_with_ip_limit": {
		spec:     httpsListener,
		sourceIP: "127.0.0.1",
	},
	"proxyv2_with_domain_limit": {
		spec:        httpsProxyv2Listener,
		domainLimit: true,
		sourceIP:    "10.1.1.1",
	},
	"proxyv2_with_ip_limit": {
		spec:     httpsProxyv2Listener,
		sourceIP: "10.1.1.1",
	},
}

func runTLSRateLimitsTests(t *testing.T, host, path string, limitedListeners map[string]tlsListener, extraArgs ...processOption) {
	for name, tt := range limitedListeners {
		t.Run(name, func(t *testing.T) {
			rateLimit := 5

			options := []processOption{
				withListeners([]ListenSpec{tt.spec}),
				withExtraArgument("metrics-address", ":"+metricsPort),
			}
			options = append(options, extraArgs...)

			limitName := "tls_connections_by_source_ip"

			if tt.domainLimit {
				options = append(options,
					withExtraArgument("rate-limit-tls-domain", fmt.Sprint(rateLimit)),
					withExtraArgument("rate-limit-tls-domain-burst", fmt.Sprint(rateLimit)))

				limitName = "tls_connections_by_domain"
			} else {
				options = append(options,
					withExtraArgument("rate-limit-tls-source-ip", fmt.Sprint(rateLimit)),
					withExtraArgument("rate-limit-tls-source-ip-burst", fmt.Sprint(rateLimit)))
			}

			if len(tt.byPassCIDR) > 0 {
				options = append(options, withExtraArgument("rate-limit-subnets-allow-list", tt.byPassCIDR))
			}

			logBuf := RunPagesProcess(t, options...)

			spec := currentListener(tt.spec)

			// when we start the process we make 1 requests to verify that process is up
			// it gets counted in the rate limit for IP, but host is different
			if !tt.domainLimit {
				rateLimit--
			}

			for i := 0; i < 10; i++ {
				rsp, err := makeTLSRequest(t, spec, "https://"+host+path)

				if tt.shouldBypass {
					require.NoError(t, err, "request: %d failed", i)
					require.NoError(t, rsp.Body.Close())
					require.Equal(t, http.StatusOK, rsp.StatusCode, "cidr should bypass request: %d failed", i)
				} else {
					if i >= rateLimit {
						assertLogFound(t, logBuf, []string{
							"TLS connection rate-limited",
							`"req_host":"` + host + `"`,
							fmt.Sprintf("\"source_ip\":\"%s\"", tt.sourceIP)})

						require.Error(t, err)
						require.Contains(t, err.Error(), "remote error: tls: internal error")
					} else {
						require.NoError(t, err, "request: %d failed", i)
						require.NoError(t, rsp.Body.Close())
						require.Equal(t, http.StatusOK, rsp.StatusCode, "request: %d failed", i)
					}
				}
			}

			expectedMetric := ""
			if tt.shouldBypass {
				// when we start the process we make 1 requests to verify that process is up
				// it gets counted as miss as host is different.
				extraMissCount := 0
				if tt.domainLimit {
					extraMissCount = 1
				}
				expectedMetric = fmt.Sprintf(
					"gitlab_pages_rate_limit_cache_requests{cache=\"hit\",op=\"%s\"} %v",
					limitName, 10-extraMissCount)
			} else {
				expectedMetric = fmt.Sprintf(
					"gitlab_pages_rate_limit_blocked_count{limit_name=\"%s\"} %v",
					limitName, 10-rateLimit)
			}

			RequireMetricEqual(t, "127.0.0.1:"+metricsPort, expectedMetric)
		})
	}
}

func TestTLSRateLimits(t *testing.T) {
	runTLSRateLimitsTests(t, "group.gitlab-example.com", "/project", tlsRateLimitedListeners)
}

func TestNamespaceInPathEnabled_TLSRateLimits(t *testing.T) {
	// domain_limit will apply at instance level (i.e. pages domain)
	runTLSRateLimitsTests(t,
		"gitlab-example.com",
		"/group/project",
		tlsRateLimitedListeners,
		withExtraArgument("namespace-in-path", "true"))
}

var tlsRateLimitedListenersWithBypass = map[string]tlsListener{
	"https_with_domain_limit": {
		spec:         httpsListener,
		domainLimit:  true,
		sourceIP:     "127.0.0.1",
		byPassCIDR:   "127.0.0.1/24",
		shouldBypass: true,
	},
	"https_with_ip_limit": {
		spec:         httpsListener,
		sourceIP:     "127.0.0.1",
		byPassCIDR:   "127.0.0.1/24",
		shouldBypass: true,
	},
	"proxyv2_with_domain_limit": {
		spec:         httpsProxyv2Listener,
		domainLimit:  true,
		sourceIP:     "10.1.1.1",
		byPassCIDR:   "10.1.1.1/24",
		shouldBypass: true,
	},
	"proxyv2_with_ip_limit": {
		spec:         httpsProxyv2Listener,
		sourceIP:     "10.1.1.1",
		byPassCIDR:   "10.1.1.1/24",
		shouldBypass: true,
	},
}

func TestTLSRateLimitsWithBypass(t *testing.T) {
	runTLSRateLimitsTests(t, "group.gitlab-example.com", "/project", tlsRateLimitedListenersWithBypass)
}

func TestNamespaceInPathEnabled_TLSRateLimitsWithBypass(t *testing.T) {
	// domain_limit will apply at instance level (i.e. pages domain)
	runTLSRateLimitsTests(t,
		"gitlab-example.com",
		"/group/project",
		tlsRateLimitedListenersWithBypass,
		withExtraArgument("namespace-in-path", "true"))
}

func makeTLSRequest(t *testing.T, spec ListenSpec, url string) (*http.Response, error) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	require.NoError(t, err)

	client, err := spec.Client(req.Host)
	require.NoError(t, err)

	return client.Do(req)
}

func assertLogFound(t *testing.T, logBuf *LogCaptureBuffer, expectedLogs []string) {
	t.Helper()

	// give the process enough time to write the log message
	require.Eventually(t, func() bool {
		logs := logBuf.String()
		for _, e := range expectedLogs {
			if !strings.Contains(logs, e) {
				return false
			}
		}
		return true
	}, 100*time.Millisecond, 10*time.Millisecond, "timed out waiting for expected logs")
}
