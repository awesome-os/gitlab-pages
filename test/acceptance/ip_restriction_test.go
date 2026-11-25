package acceptance_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
	"gitlab.com/gitlab-org/gitlab-pages/test/gitlabstub"
)

const (
	ipRestrictionHost   = "group.auth.gitlab-example.com"
	ipRestrictionPath   = "/private.project/"
	ipRestrictionBody   = "private"
	allowlistedClientIP = "198.51.100.10"
	blockedClientIP     = "203.0.113.50"
)

// ipRestrictionStub models GitLab group IP restriction on
// /api/v4/projects/:id/pages_access: Rails evaluates the IP in
// X-Forwarded-For and answers 404 when it is not in the group allowlist.
type ipRestrictionStub struct {
	mu           sync.Mutex
	forwardedFor [][]string
}

func (s *ipRestrictionStub) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.forwardedFor = append(s.forwardedFor, r.Header.Values("X-Forwarded-For"))
		s.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer abc" ||
			r.Header.Get("X-Forwarded-For") != allowlistedClientIP {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func (s *ipRestrictionStub) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.forwardedFor = nil
}

func (s *ipRestrictionStub) requests() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.forwardedFor
}

// getPageFromClientIP requests a page through the PROXY protocol v2 listener,
// announcing clientIP as the client address so Pages sees it as r.RemoteAddr.
func getPageFromClientIP(t *testing.T, clientIP, host, path string, header http.Header) (int, string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpsProxyv2Listener.URL(path), nil)
	require.NoError(t, err)
	req.Host = host
	req.Header = header

	client, err := httpsProxyv2Listener.Client(host)
	require.NoError(t, err)
	client.Transport.(*http.Transport).DialContext = httpsProxyv2Listener.proxyV2DialContextFrom(clientIP)

	rsp, err := client.Transport.RoundTrip(req)
	require.NoError(t, err)
	testhelpers.Close(t, rsp.Body)

	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)

	return rsp.StatusCode, string(body)
}

// authenticateSession completes the OAuth flow and returns the session cookie
// holding the access token for host.
func authenticateSession(t *testing.T, spec ListenSpec, host, path string) string {
	t.Helper()

	rsp, err := GetRedirectPage(t, spec, host, path)
	require.NoError(t, err)
	testhelpers.Close(t, rsp.Body)
	require.Equal(t, http.StatusFound, rsp.StatusCode)
	pageCookie := rsp.Header.Get("Set-Cookie")

	proxyURL, err := url.Parse(rsp.Header.Get("Location"))
	require.NoError(t, err)
	state := proxyURL.Query().Get("state")

	rsp, err = GetRedirectPage(t, spec, proxyURL.Host, proxyURL.Path+"?"+proxyURL.RawQuery)
	require.NoError(t, err)
	testhelpers.Close(t, rsp.Body)
	require.Equal(t, http.StatusFound, rsp.StatusCode)
	proxyCookie := rsp.Header.Get("Set-Cookie")

	rsp, err = GetRedirectPageWithCookie(t, spec, proxyURL.Host, "/auth?code=1&state="+state, proxyCookie)
	require.NoError(t, err)
	testhelpers.Close(t, rsp.Body)
	require.Equal(t, http.StatusFound, rsp.StatusCode)

	callbackURL, err := url.Parse(rsp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, host, callbackURL.Host)

	rsp, err = GetRedirectPageWithCookie(t, spec, host, callbackURL.Path+"?"+callbackURL.RawQuery, pageCookie)
	require.NoError(t, err)
	testhelpers.Close(t, rsp.Body)
	require.Equal(t, http.StatusFound, rsp.StatusCode)

	cookie := rsp.Header.Get("Set-Cookie")
	require.NotEmpty(t, cookie)

	return cookie
}

// TestIPRestrictionAccessControl asserts that group IP restriction enforced by
// the pages_access API applies to access-controlled Pages: Pages forwards the
// client address it sees (r.RemoteAddr, here set through PROXY protocol) in
// X-Forwarded-For, serves the private content only when the API allows that
// address, and ignores forwarding headers sent by the client.
func TestIPRestrictionAccessControl(t *testing.T) {
	stub := &ipRestrictionStub{}

	RunPagesProcess(t,
		withListeners([]ListenSpec{httpsProxyv2Listener}),
		withArguments([]string{"-config=" + defaultAuthConfig(t)}),
		withStubOptions(gitlabstub.WithPagesAccessHandler(stub.handler())),
	)

	sessionCookie := authenticateSession(t, httpsProxyv2Listener, ipRestrictionHost, ipRestrictionPath)

	spoofedHeaders := http.Header{
		"X-Forwarded-For": []string{allowlistedClientIP},
		"X-Real-Ip":       []string{allowlistedClientIP},
		"Forwarded":       []string{"for=" + allowlistedClientIP},
	}

	tests := map[string]struct {
		clientIP       string
		session        bool
		clientHeaders  http.Header
		expectedStatus int
	}{
		"session_allowlisted_ip_is_served": {
			clientIP:       allowlistedClientIP,
			session:        true,
			expectedStatus: http.StatusOK,
		},
		"session_blocked_ip_is_not_found": {
			clientIP:       blockedClientIP,
			session:        true,
			expectedStatus: http.StatusNotFound,
		},
		"session_blocked_ip_cannot_spoof_forwarded_headers": {
			clientIP:       blockedClientIP,
			session:        true,
			clientHeaders:  spoofedHeaders,
			expectedStatus: http.StatusNotFound,
		},
		"authorization_header_allowlisted_ip_is_served": {
			clientIP:       allowlistedClientIP,
			expectedStatus: http.StatusOK,
		},
		"authorization_header_blocked_ip_is_not_found": {
			clientIP:       blockedClientIP,
			expectedStatus: http.StatusNotFound,
		},
		"authorization_header_blocked_ip_cannot_spoof_forwarded_headers": {
			clientIP:       blockedClientIP,
			clientHeaders:  spoofedHeaders,
			expectedStatus: http.StatusNotFound,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			stub.reset()

			header := http.Header{}
			for k, v := range tt.clientHeaders {
				header[k] = v
			}

			if tt.session {
				header.Set("Cookie", sessionCookie)
			} else {
				header.Set("Authorization", "Bearer abc")
			}

			status, body := getPageFromClientIP(t, tt.clientIP, ipRestrictionHost, ipRestrictionPath, header)

			require.Equal(t, [][]string{{tt.clientIP}}, stub.requests(),
				"pages_access must be called once with X-Forwarded-For set to the client address only")
			require.Equal(t, tt.expectedStatus, status)

			if tt.expectedStatus == http.StatusOK {
				require.Equal(t, ipRestrictionBody, body)
			} else {
				require.NotEqual(t, ipRestrictionBody, body, "private content must not be served")
			}
		})
	}
}

// TestIPRestrictionIgnoresClientForwardedHeaders asserts that on listeners
// without a trusted reverse proxy in front, the X-Forwarded-For sent to
// pages_access is the TCP peer address, never a header sent by the client.
func TestIPRestrictionIgnoresClientForwardedHeaders(t *testing.T) {
	stub := &ipRestrictionStub{}

	RunPagesProcess(t,
		withListeners([]ListenSpec{httpListener, httpsListener}),
		withArguments([]string{"-config=" + defaultAuthConfig(t)}),
		withStubOptions(gitlabstub.WithPagesAccessHandler(stub.handler())),
	)

	for _, spec := range []ListenSpec{httpListener, httpsListener} {
		t.Run(spec.Type, func(t *testing.T) {
			stub.reset()

			rsp, err := GetPageFromListenerWithHeaders(t, spec, ipRestrictionHost, ipRestrictionPath, http.Header{
				"Authorization":   []string{"Bearer abc"},
				"X-Forwarded-For": []string{allowlistedClientIP},
				"X-Real-Ip":       []string{allowlistedClientIP},
				"Forwarded":       []string{"for=" + allowlistedClientIP},
			})
			require.NoError(t, err)
			testhelpers.Close(t, rsp.Body)

			require.Equal(t, [][]string{{spec.Host}}, stub.requests())
			require.Equal(t, http.StatusNotFound, rsp.StatusCode)
		})
	}
}
