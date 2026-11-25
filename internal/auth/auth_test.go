package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	dMock "gitlab.com/gitlab-org/gitlab-pages/internal/domain/mock"
	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/mock"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func createTestAuth(t *testing.T, internalServer string, publicServer string) *Auth {
	t.Helper()

	a, err := New(&Options{
		PagesDomain:          "pages.gitlab-example.com",
		StoreSecret:          "something-very-secret",
		ClientID:             "id",
		ClientSecret:         "secret",
		RedirectURI:          "http://pages.gitlab-example.com/auth",
		InternalGitlabServer: internalServer,
		PublicGitlabServer:   publicServer,
		AuthScope:            "scope",
		AuthTimeout:          5 * time.Second,
		CookieSessionTimeout: 10 * time.Minute,
		AllowNamespaceInPath: false,
	})

	require.NoError(t, err)

	return a
}

type domainMock struct {
	projectID       uint64
	projectPrefix   string
	notFoundContent string
}

func (dm *domainMock) GetProjectID(r *http.Request) uint64 {
	return dm.projectID
}

func (dm *domainMock) GetProjectPrefix(r *http.Request) string {
	return dm.projectPrefix
}

func (dm *domainMock) ServeNotFoundAuthFailed(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(dm.notFoundContent))
}

// Gorilla's sessions use request context to save session
// Which makes session sharable between test code and actually manipulating session
// Which leads to negative side effects: we can't test encryption, and cookie params
// like max-age and secure are not being properly set
// To avoid that we use fake request, and set only session cookie without copying context
func setSessionValues(t *testing.T, r *http.Request, auth *Auth, values map[interface{}]interface{}) {
	t.Helper()

	tmpRequest, err := http.NewRequest("GET", "http://"+r.Host, nil)
	if len(r.Header.Get("X-Gitlab-Namespace-In-Path")) > 0 {
		tmpRequest.Header.Set("X-Gitlab-Namespace-In-Path", r.Header.Get("X-Gitlab-Namespace-In-Path"))
	}
	require.NoError(t, err)

	result := httptest.NewRecorder()

	session, _ := auth.getSessionFromStore(tmpRequest)
	session.Values = values
	err = session.Save(tmpRequest, result)
	require.NoError(t, err)

	res := result.Result()
	testhelpers.Close(t, res.Body)

	for _, cookie := range res.Cookies() {
		r.AddCookie(cookie)
	}
}

func TestTryAuthenticate(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()
	reqURL, err := url.Parse("/something/else")
	require.NoError(t, err)
	reqURL.Scheme = request.SchemeHTTPS
	r := &http.Request{URL: reqURL}

	mockCtrl := gomock.NewController(t)

	mockSource := mock.NewMockSource(mockCtrl)
	require.False(t, auth.TryAuthenticate(result, r, mockSource))
}

func TestTryAuthenticateWithError(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()
	reqURL, err := url.Parse("/auth?error=access_denied")
	require.NoError(t, err)

	reqURL.Scheme = request.SchemeHTTPS
	r := &http.Request{URL: reqURL}

	mockCtrl := gomock.NewController(t)

	mockSource := mock.NewMockSource(mockCtrl)
	require.True(t, auth.TryAuthenticate(result, r, mockSource))
	require.Equal(t, http.StatusUnauthorized, result.Code)
}

func captureLogs(t *testing.T, r *http.Request) (*bytes.Buffer, *http.Request) {
	t.Helper()

	buf := loggingtest.Capture(t, slog.LevelInfo)

	return buf, r.WithContext(logv2.WithLogger(r.Context(), logging.Logger))
}

func TestTryAuthenticateLogsCleanedProxyAuthDomain(t *testing.T) {
	auth := createTestAuth(t, "", "")

	r, err := http.NewRequest(http.MethodGet, "https://example.com/auth?code=1&state=state", nil)
	require.NoError(t, err)
	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"state":             "state",
		"proxy_auth_domain": "https://domain.com/?credential=proxy-secret",
	})

	buf, r := captureLogs(t, r)

	mockCtrl := gomock.NewController(t)
	mockSource := mock.NewMockSource(mockCtrl)
	require.True(t, auth.TryAuthenticate(httptest.NewRecorder(), r, mockSource))
	require.Contains(t, buf.String(), "https://domain.com/")
	require.NotContains(t, buf.String(), "proxy-secret")
}

func TestTryAuthenticateLogsOnlyNonSensitiveOAuthQueryValues(t *testing.T) {
	tests := map[string]struct {
		requestURL   string
		secrets      []string
		loggedValue  string
		domainLookup string
	}{
		"oauth error": {
			requestURL:  "https://example.com/auth?error=access_denied&state=state-secret",
			secrets:     []string{"state-secret"},
			loggedValue: "access_denied",
		},
		"proxy domain": {
			requestURL:  "https://example.com/auth?domain=https%3A%2F%2Fpages.gitlab-example.com%2F%3Fcredential%3Ddomain-secret&state=state-secret",
			secrets:     []string{"domain-secret", "state-secret"},
			loggedValue: "https://pages.gitlab-example.com/",
		},
		"proxy domain with unsupported scheme": {
			requestURL:  "https://example.com/auth?domain=ftp%3A%2F%2Fpages.gitlab-example.com%2F%3Fcredential%3Ddomain-secret&state=state-secret",
			secrets:     []string{"domain-secret", "state-secret"},
			loggedValue: "ftp://pages.gitlab-example.com/",
		},
		"unconfigured proxy domain": {
			requestURL:   "https://example.com/auth?domain=https%3A%2F%2Fnot-configured.example.com%2F%3Fcredential%3Ddomain-secret&state=state-secret",
			secrets:      []string{"domain-secret", "state-secret"},
			loggedValue:  "https://not-configured.example.com/",
			domainLookup: "not-configured.example.com",
		},
		"invalid proxy domain": {
			requestURL:  "https://example.com/auth?domain=%3A%2F%2Finvalid-domain&state=state-secret",
			secrets:     []string{"state-secret"},
			loggedValue: "://invalid-domain",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			auth := createTestAuth(t, "", "https://public-gitlab.example.com")
			result := httptest.NewRecorder()
			r, err := http.NewRequest(http.MethodGet, test.requestURL, nil)
			require.NoError(t, err)

			var buf *bytes.Buffer
			buf, r = captureLogs(t, r)

			mockCtrl := gomock.NewController(t)
			mockSource := mock.NewMockSource(mockCtrl)
			if test.domainLookup != "" {
				mockSource.EXPECT().GetDomain(gomock.Any(), test.domainLookup).Return(nil, nil)
			}
			require.True(t, auth.TryAuthenticate(result, r, mockSource))
			require.NotEmpty(t, buf.String())

			for _, secret := range test.secrets {
				require.NotContains(t, buf.String(), secret)
			}
			if test.loggedValue != "" {
				require.Contains(t, buf.String(), test.loggedValue)
			}
		})
	}
}

func TestTryAuthenticateDoesNotLogRedirectURIQuery(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"access_token":"abc"}`)
	}))
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")
	code, err := auth.EncryptAndSignCode("https://example.com", "1")
	require.NoError(t, err)

	r, err := http.NewRequest(http.MethodGet, "https://example.com/auth?code="+code+"&state=state-secret", nil)
	require.NoError(t, err)
	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"uri":   "https://pages.gitlab-example.com/project/?credential=redirect-secret",
		"state": "state-secret",
	})

	buf, r := captureLogs(t, r)

	result := httptest.NewRecorder()
	mockCtrl := gomock.NewController(t)
	mockSource := mock.NewMockSource(mockCtrl)
	require.True(t, auth.TryAuthenticate(result, r, mockSource))
	require.Equal(t, "https://pages.gitlab-example.com/project/?credential=redirect-secret", result.Header().Get("Location"))
	require.NotContains(t, buf.String(), "redirect-secret")
	require.NotContains(t, buf.String(), "state-secret")
}

func TestTryAuthenticateWithCodeButInvalidState(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com/auth?code=1&state=invalid", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"state": "state",
	})

	mockCtrl := gomock.NewController(t)

	mockSource := mock.NewMockSource(mockCtrl)
	require.True(t, auth.TryAuthenticate(result, r, mockSource))
	require.Equal(t, http.StatusUnauthorized, result.Code)
}

func TestTryAuthenticateRemoveTokenFromRedirect(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com/auth?code=1&state=state&token=secret", nil)
	require.Equal(t, r.URL.Query().Get("token"), "secret", "token is present before redirecting")
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"state":             "state",
		"proxy_auth_domain": "https://domain.com",
	})

	mockCtrl := gomock.NewController(t)

	mockSource := mock.NewMockSource(mockCtrl)
	require.True(t, auth.TryAuthenticate(result, r, mockSource))
	require.Equal(t, http.StatusFound, result.Code)

	require.Contains(t, result.Header().Get("Set-Cookie"), "Path=/auth;")

	redirect, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)

	require.Empty(t, redirect.Query().Get("token"), "token is gone after redirecting")
}

func TestTryAuthenticateWithDomainAndState(t *testing.T) {
	auth := createTestAuth(t, "", "public-gitlab.example.com")
	result := httptest.NewRecorder()
	reqURL, err := url.Parse("/auth?domain=https%3A%2F%2Fpages.gitlab-example.com&state=state&root_namespace_id=1")
	require.NoError(t, err)
	r := &http.Request{URL: reqURL}

	mockCtrl := gomock.NewController(t)

	mockSource := mock.NewMockSource(mockCtrl)
	require.True(t, auth.TryAuthenticate(result, r, mockSource))
	require.Equal(t, http.StatusFound, result.Code)
	require.Contains(t, result.Header().Get("Set-Cookie"), "Path=/auth;")
	redirect, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)

	require.Equal(t, "/public-gitlab.example.com/oauth/authorize?client_id=id&redirect_uri=http://pages.gitlab-example.com/auth&response_type=code&state=state&scope=scope&root_namespace_id=1", redirect.String())
}

func TestCheckAuthenticationWhenStateIsAlreadySet(t *testing.T) {
	auth := createTestAuth(t, "", "")
	now := time.Unix(1700000000, 0)
	auth.now = func() time.Time { return now }

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com/", nil)
	require.NoError(t, err)

	domain1 := getDomainWithMockResolver(t, "example.com", 1)
	r = domain.ReqWithDomain(r, domain1)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		stateKey:         "given_state",
		stateIssuedAtKey: now.Unix(),
		codeVerifierKey:  "given_verifier",
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})
	require.True(t, contentServed)

	session, _ := auth.getSessionFromStore(r)
	require.Equal(t, "given_state", session.Values[stateKey], "did not reuse the pre-set state")
	require.Equal(t, "given_verifier", session.Values[codeVerifierKey], "did not reuse the pre-set code verifier")
}

func TestTryAuthenticateWithNonHttpDomainAndState(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com/auth?domain=mailto://example.com?body=TESTBODY&state=state", nil)
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)

	mockSource := mock.NewMockSource(mockCtrl)
	require.True(t, auth.TryAuthenticate(result, r, mockSource))
	require.Equal(t, http.StatusUnauthorized, result.Code)
}

func TestProjectPrefixInSessionValues(t *testing.T) {
	t.Setenv(feature.ProjectPrefixCookiePath.EnvVariable, "true")
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com/test", nil)
	require.NoError(t, err)

	domain1 := getDomainWithMockResolver(t, "example.com", 1)
	r = domain.ReqWithDomain(r, domain1)

	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000, projectPrefix: "/test/"})
	require.True(t, contentServed)

	cookieString := result.Header().Get("Set-Cookie")
	require.Contains(t, cookieString, "Path=/auth;", "did not set cookie Path=/auth;")

	session, _ := auth.getSessionFromStore(r)
	require.Equal(t, "/test/", session.Values["_project_prefix"], "did not set project prefix in session values")
}

// Project prefixes are resolved case-insensitively, but browsers match cookie
// paths case-sensitively, so the prefix stored for the cookie path must use
// the letter case of the request path.
func TestProjectPrefixInSessionValuesUsesRequestCase(t *testing.T) {
	t.Setenv(feature.ProjectPrefixCookiePath.EnvVariable, "true")

	tests := map[string]struct {
		path           string
		projectPrefix  string
		expectedPrefix any
	}{
		"same case": {
			path:           "/project/index.html",
			projectPrefix:  "/project/",
			expectedPrefix: "/project/",
		},
		"different case": {
			path:           "/Project/index.html",
			projectPrefix:  "/project/",
			expectedPrefix: "/Project/",
		},
		"different case without trailing slash": {
			path:           "/PROJECT",
			projectPrefix:  "/project/",
			expectedPrefix: "/PROJECT/",
		},
		"different case in a subgroup": {
			path:           "/SubGroup/Project/",
			projectPrefix:  "/subgroup/project/",
			expectedPrefix: "/SubGroup/Project/",
		},
		"lowercase request for a mixed-case prefix": {
			path:           "/mytestproject/index.html",
			projectPrefix:  "/MyTestProject/",
			expectedPrefix: "/mytestproject/",
		},
		"request path longer than the prefix segment": {
			path:           "/Project.1/",
			projectPrefix:  "/project/",
			expectedPrefix: "/project/",
		},
		"request path shorter than the prefix": {
			path:           "/Proj",
			projectPrefix:  "/project/",
			expectedPrefix: "/project/",
		},
		"root prefix": {
			path:           "/Index.html",
			projectPrefix:  "/",
			expectedPrefix: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			auth := createTestAuth(t, "", "")

			result := httptest.NewRecorder()

			r, err := http.NewRequest("GET", "https://example.com"+tc.path, nil)
			require.NoError(t, err)

			domain1 := getDomainWithMockResolver(t, "example.com", 1)
			r = domain.ReqWithDomain(r, domain1)

			contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000, projectPrefix: tc.projectPrefix})
			require.True(t, contentServed)

			session, _ := auth.getSessionFromStore(r)
			require.Equal(t, tc.expectedPrefix, session.Values["_project_prefix"])
		})
	}
}

func testTryAuthenticateWithCodeAndState(t *testing.T, https bool, projectPrefix string) *httptest.ResponseRecorder {
	t.Helper()

	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			require.Equal(t, "POST", r.Method)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "{\"access_token\":\"abc\"}")
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	host := "http://example.com"
	if https {
		host = "https://example.com"
	}

	code, err := auth.EncryptAndSignCode(host, "1")
	require.NoError(t, err)

	r, err := http.NewRequest("GET", host+"/auth?code="+code+"&state=state", nil)
	require.NoError(t, err)
	if https {
		r.URL.Scheme = request.SchemeHTTPS
	} else {
		r.URL.Scheme = request.SchemeHTTP
	}

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"uri":             "https://pages.gitlab-example.com/project/",
		"state":           "state",
		"_project_prefix": projectPrefix,
	})

	result := httptest.NewRecorder()

	mockCtrl := gomock.NewController(t)

	mockSource := mock.NewMockSource(mockCtrl)
	require.True(t, auth.TryAuthenticate(result, r, mockSource))

	res := result.Result()
	testhelpers.Close(t, res.Body)

	require.Equal(t, http.StatusFound, result.Code)
	require.Equal(t, "https://pages.gitlab-example.com/project/", result.Header().Get("Location"))
	require.Equal(t, 600, res.Cookies()[0].MaxAge)
	require.Equal(t, https, res.Cookies()[0].Secure)

	return result
}

func TestTryAuthenticateWithCodeAndStateOverHTTP(t *testing.T) {
	testTryAuthenticateWithCodeAndState(t, false, "/")
}

func TestTryAuthenticateWithCodeAndStateOverHTTPS(t *testing.T) {
	testTryAuthenticateWithCodeAndState(t, true, "/")
}

func TestTryAuthenticateOverHTTPSWithProjectPrefixCookiePathEnabled(t *testing.T) {
	t.Setenv(feature.ProjectPrefixCookiePath.EnvVariable, "true")
	prefix := "/project"
	result := testTryAuthenticateWithCodeAndState(t, true, prefix)

	cookieString := result.Header().Get("Set-Cookie")
	require.Contains(t, cookieString, "Path="+prefix+";", "did not set Cookie Path="+prefix+";")
}

func TestTryAuthenticateOverHTTPSWithProjectPrefixCookiePathDisabled(t *testing.T) {
	t.Setenv(feature.ProjectPrefixCookiePath.EnvVariable, "false")
	prefix := "/project"
	result := testTryAuthenticateWithCodeAndState(t, true, prefix)

	cookieString := result.Header().Get("Set-Cookie")
	require.Contains(t, cookieString, "Path=/;", "did not set Cookie Path=/;")
}

func TestCheckAuthenticationWhenAccess(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()
	r, err := http.NewRequest("Get", "https://example.com/", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{"access_token": "abc"})
	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})
	require.False(t, contentServed)

	// notFoundContent wasn't served so the default response from checkAuthentication should be 200
	require.Equal(t, http.StatusOK, result.Code)
}

func TestCheckAuthenticationSetsXForwardedFor(t *testing.T) {
	var receivedXFF string

	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			receivedXFF = r.Header.Get("X-Forwarded-For")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	t.Cleanup(apiServer.Close)

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "https://example.com/", nil)
	require.NoError(t, err)
	r.RemoteAddr = "203.0.113.50:12345"

	setSessionValues(t, r, auth, map[interface{}]interface{}{"access_token": "abc"})
	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})
	require.False(t, contentServed)
	require.Equal(t, http.StatusOK, result.Code)
	require.Equal(t, "203.0.113.50", receivedXFF, "X-Forwarded-For should contain the client's real IP")
}

// TestCheckAuthenticationIPRestriction models GitLab group IP restriction:
// Rails answers pages_access with 404 when the IP in X-Forwarded-For is not in
// the group allowlist. Pages must then serve its not-found page instead of the
// private content, on both the session and the Authorization header paths,
// and must forward r.RemoteAddr rather than a client supplied header.
func TestCheckAuthenticationIPRestriction(t *testing.T) {
	const (
		allowlistedIP = "198.51.100.10"
		blockedIP     = "203.0.113.50"
		notFound      = "IP restricted 404"
	)

	tests := map[string]struct {
		remoteAddr        string
		authHeader        string
		clientHeaders     http.Header
		expectedForwarded string
		expectedStatus    int
	}{
		"session_allowlisted_ip": {
			remoteAddr:        allowlistedIP + ":12345",
			expectedForwarded: allowlistedIP,
			expectedStatus:    http.StatusOK,
		},
		"session_blocked_ip": {
			remoteAddr:        blockedIP + ":12345",
			expectedForwarded: blockedIP,
			expectedStatus:    http.StatusNotFound,
		},
		"header_allowlisted_ip": {
			remoteAddr:        allowlistedIP + ":12345",
			authHeader:        "Bearer pat",
			expectedForwarded: allowlistedIP,
			expectedStatus:    http.StatusOK,
		},
		"header_blocked_ip": {
			remoteAddr:        blockedIP + ":12345",
			authHeader:        "Bearer pat",
			expectedForwarded: blockedIP,
			expectedStatus:    http.StatusNotFound,
		},
		"session_ipv6_blocked_ip": {
			remoteAddr:        "[2001:db8::1]:443",
			expectedForwarded: "2001:db8::1",
			expectedStatus:    http.StatusNotFound,
		},
		"session_blocked_ip_spoofing_allowlisted_ip": {
			remoteAddr:        blockedIP + ":12345",
			clientHeaders:     http.Header{"X-Forwarded-For": []string{allowlistedIP}},
			expectedForwarded: blockedIP,
			expectedStatus:    http.StatusNotFound,
		},
		"header_blocked_ip_spoofing_allowlisted_ip": {
			remoteAddr: blockedIP + ":12345",
			authHeader: "Bearer pat",
			clientHeaders: http.Header{
				"X-Forwarded-For": []string{allowlistedIP + ", " + blockedIP},
				"X-Real-Ip":       []string{allowlistedIP},
				"Forwarded":       []string{"for=" + allowlistedIP},
			},
			expectedForwarded: blockedIP,
			expectedStatus:    http.StatusNotFound,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv(feature.AuthorizationHeader.EnvVariable, "true")

			expectedAuthorization := "Bearer abc"
			if tt.authHeader != "" {
				expectedAuthorization = tt.authHeader
			}

			var receivedXFF []string
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v4/projects/1000/pages_access" {
					w.WriteHeader(http.StatusTeapot)
					return
				}

				receivedXFF = r.Header.Values("X-Forwarded-For")

				if r.Header.Get("Authorization") != expectedAuthorization ||
					r.Header.Get("X-Forwarded-For") != allowlistedIP {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(apiServer.Close)

			auth := createTestAuth(t, apiServer.URL, "")

			r, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
			require.NoError(t, err)
			r.RemoteAddr = tt.remoteAddr
			for k, v := range tt.clientHeaders {
				r.Header[k] = v
			}

			if tt.authHeader != "" {
				r.Header.Set("Authorization", tt.authHeader)
			} else {
				setSessionValues(t, r, auth, map[interface{}]interface{}{"access_token": "abc"})
			}

			result := httptest.NewRecorder()
			contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000, notFoundContent: notFound})

			require.Equal(t, []string{tt.expectedForwarded}, receivedXFF)
			require.Equal(t, tt.expectedStatus, result.Code)

			if tt.expectedStatus == http.StatusOK {
				require.False(t, contentServed, "private content must be served by the caller")
				require.Empty(t, result.Body.String())
			} else {
				require.True(t, contentServed, "not-found page must be served instead of private content")
				require.Equal(t, notFound, result.Body.String())
			}
		})
	}
}

func TestCheckAuthenticationWhenContextCanceled(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(apiServer.Close)

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()
	r, err := http.NewRequest("Get", "https://example.com/", nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(r.Context())
	r = r.WithContext(ctx)
	setSessionValues(t, r, auth, map[interface{}]interface{}{"access_token": "abc"})

	// cancel context explicitly and expect not found
	cancel()
	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})
	require.True(t, contentServed)
	require.Equal(t, http.StatusNotFound, result.Code)
}

func TestCheckAuthenticationWhenNoAccess(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	w := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com/auth?code=1&state=state", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	contentServed := auth.checkAuthentication(w, r, &domainMock{projectID: 1000, notFoundContent: "Generic 404"})
	require.True(t, contentServed)
	res := w.Result()
	testhelpers.Close(t, res.Body)

	require.Equal(t, http.StatusNotFound, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, string(body), "Generic 404")
}

func TestCheckAuthenticationWithSessionFromDifferentHost(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()
	r, err := http.NewRequest("Get", "https://different.com/", nil)
	require.NoError(t, err)
	setSessionValues(t, r, auth, map[interface{}]interface{}{"access_token": "abc"})

	r, err = http.NewRequest("Get", "https://example.com/", nil)
	require.NoError(t, err)
	domain1 := getDomainWithMockResolver(t, "example.com", 0) // 0 is treated as blank
	r = domain.ReqWithDomain(r, domain1)
	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})
	require.True(t, contentServed)

	// should redirect to auth
	require.Equal(t, http.StatusFound, result.Code)
	redirectURL, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "pages.gitlab-example.com", redirectURL.Host)
	require.Equal(t, "/auth", redirectURL.Path)
	require.Equal(t, "https://example.com", redirectURL.Query().Get("domain"))
	require.Equal(t, "", redirectURL.Query().Get("root_namespace_id"))
}

func TestShouldReauthenticateOnForbidden(t *testing.T) {
	auth := createTestAuth(t, "", "")

	newSession := func(t *testing.T, values map[interface{}]interface{}) *hostSession {
		t.Helper()
		r, err := http.NewRequest("GET", "https://example.com", nil)
		require.NoError(t, err)
		session, err := auth.getSessionFromStore(r)
		require.NoError(t, err)
		session.Values = values
		return session
	}

	tests := map[string]struct {
		statusCode    int
		setAuthHeader bool
		sessionValues map[interface{}]interface{}
		expected      bool
	}{
		"403 with cookie token and no marker reauthenticates": {
			statusCode:    http.StatusForbidden,
			sessionValues: map[interface{}]interface{}{"access_token": "abc"},
			expected:      true,
		},
		"non-403 status does not reauthenticate": {
			statusCode:    http.StatusNotFound,
			sessionValues: map[interface{}]interface{}{"access_token": "abc"},
			expected:      false,
		},
		"non-403 status with marker set does not reauthenticate": {
			statusCode:    http.StatusNotFound,
			sessionValues: map[interface{}]interface{}{"access_token": "abc", "sso_reauth_attempted": true},
			expected:      false,
		},
		"403 without session token does not reauthenticate": {
			statusCode:    http.StatusForbidden,
			sessionValues: map[interface{}]interface{}{},
			expected:      false,
		},
		"403 without session token but with marker set does not reauthenticate": {
			statusCode:    http.StatusForbidden,
			sessionValues: map[interface{}]interface{}{"sso_reauth_attempted": true},
			expected:      false,
		},
		"403 with marker already set does not reauthenticate": {
			statusCode:    http.StatusForbidden,
			sessionValues: map[interface{}]interface{}{"access_token": "abc", "sso_reauth_attempted": true},
			expected:      false,
		},
		"403 with header auth does not reauthenticate": {
			statusCode:    http.StatusForbidden,
			setAuthHeader: true,
			sessionValues: map[interface{}]interface{}{"access_token": "abc"},
			expected:      false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if tc.setAuthHeader {
				t.Setenv(feature.AuthorizationHeader.EnvVariable, "true")
			}

			r, err := http.NewRequest("GET", "https://example.com", nil)
			require.NoError(t, err)
			if tc.setAuthHeader {
				r.Header.Set("Authorization", "Bearer abc")
			}

			session := newSession(t, tc.sessionValues)
			resp := &http.Response{StatusCode: tc.statusCode}

			require.Equal(t, tc.expected, shouldReauthenticateOnForbidden(resp, session, r))
		})
	}
}

func TestReauthenticateExpiredSessionSetsProjectPrefixCookiePath(t *testing.T) {
	t.Setenv(feature.ProjectPrefixCookiePath.EnvVariable, "true")

	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("GET", "https://example.com/subgroup/project/page.html", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{
		projectID:     1000,
		projectPrefix: "/subgroup/project/",
	})
	require.True(t, contentServed)
	require.Equal(t, http.StatusFound, result.Code)

	cookieHeader := result.Header().Get("Set-Cookie")
	require.Contains(t, cookieHeader, "Path=/subgroup/project",
		"marker cookie must use the project-prefix path to overwrite the stale cookie")
}

// The stale cookie was set with the letter case of the request path, so the
// marker cookie must use it too, or the browser keeps both cookies.
func TestReauthenticateExpiredSessionUsesRequestCaseForCookiePath(t *testing.T) {
	t.Setenv(feature.ProjectPrefixCookiePath.EnvVariable, "true")

	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("GET", "https://example.com/SubGroup/Project/page.html", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{
		projectID:     1000,
		projectPrefix: "/subgroup/project/",
	})
	require.True(t, contentServed)
	require.Equal(t, http.StatusFound, result.Code)

	res := result.Result()
	testhelpers.Close(t, res.Body)

	cookies := res.Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "/SubGroup/Project", cookies[0].Path)
}

func TestCheckAuthenticationWhenForbiddenAndSessionExpiredReauthenticates(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})
	require.True(t, contentServed)
	require.Equal(t, http.StatusFound, result.Code)
}

func TestCheckAuthenticationWhenForbiddenAfterReauthAttemptServesNotFound(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token":         "abc",
		"sso_reauth_attempted": true,
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000, notFoundContent: "Generic 404"})
	require.True(t, contentServed)
	res := result.Result()
	testhelpers.Close(t, res.Body)

	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestCheckAuthenticationWhenForbiddenWithHeaderTokenDoesNotReauthenticate(t *testing.T) {
	t.Setenv(feature.AuthorizationHeader.EnvVariable, "true")

	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com", nil)
	require.NoError(t, err)
	r.Header.Set("Authorization", "Bearer abc")

	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000, notFoundContent: "Generic 404"})
	require.True(t, contentServed)
	res := result.Result()
	testhelpers.Close(t, res.Body)

	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestCheckAuthenticationWhenForbiddenWithoutSessionTokenDoesNotReauthenticate(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com", nil)
	require.NoError(t, err)

	session, err := auth.checkSession(result, r)
	require.NoError(t, err)
	require.False(t, shouldReauthenticateOnForbidden(&http.Response{StatusCode: http.StatusForbidden}, session, r))
}

func TestCheckAuthenticationClearsReauthMarkerOnSuccess(t *testing.T) {
	t.Setenv(feature.ProjectPrefixCookiePath.EnvVariable, "true")

	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("GET", "https://example.com/SubGroup/Project/page.html", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token":         "abc",
		"sso_reauth_attempted": true,
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{
		projectID:     1000,
		projectPrefix: "/SubGroup/Project/",
	})
	require.False(t, contentServed)

	res := result.Result()
	testhelpers.Close(t, res.Body)

	cookies := res.Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "/SubGroup/Project", cookies[0].Path)
}

func TestCheckAuthenticationWhenInvalidToken(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "{\"error\":\"invalid_token\"}")
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})
	require.True(t, contentServed)
	require.Equal(t, http.StatusFound, result.Code)
}

func TestCheckAuthenticationWhenInvalidTokenSetsProjectPrefixCookiePath(t *testing.T) {
	t.Setenv(feature.ProjectPrefixCookiePath.EnvVariable, "true")

	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "{\"error\":\"invalid_token\"}")
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "https://example.com/SubGroup/Project/", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{
		projectID:     1000,
		projectPrefix: "/SubGroup/Project/",
	})
	require.True(t, contentServed)
	require.Equal(t, http.StatusFound, result.Code)

	res := result.Result()
	testhelpers.Close(t, res.Body)

	cookies := res.Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "/SubGroup/Project", cookies[0].Path)
}

func testCheckAuthenticationWhenValidAndInvalidAuthorizationHeader(t *testing.T, tests []AuthHeader) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/1000/pages_access":
			authHeader := r.Header.Get("Authorization")
			if authHeader == "Bearer abc" {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusUnauthorized) // Handle invalid token case
			}
		default:
			t.Logf("Unexpected r.URL.Path: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(apiServer.Close)

	auth := createTestAuth(t, apiServer.URL, "")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest("GET", "https://example.com", nil)
			require.NoError(t, err)
			if tc.authHeader != "" {
				r.Header.Set("Authorization", tc.authHeader)
			}

			domain1 := getDomainWithMockResolver(t, "example.com", 1)
			r = domain.ReqWithDomain(r, domain1)

			result := httptest.NewRecorder()

			contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})

			require.Equal(t, tc.expectContent, contentServed)
			require.Equal(t, tc.expectedCode, result.Code)
			if tc.expectedCode == http.StatusFound {
				redirectURL, err := url.Parse(result.Header().Get("Location"))
				require.NoError(t, err)
				require.Equal(t, "pages.gitlab-example.com", redirectURL.Host)
				require.Equal(t, "/auth", redirectURL.Path)
				require.Equal(t, "https://example.com", redirectURL.Query().Get("domain"))
				require.Equal(t, "1", redirectURL.Query().Get("root_namespace_id"))
			}
		})
	}
}

type AuthHeader struct {
	name          string
	authHeader    string
	expectedCode  int
	expectContent bool
}

func TestCheckAuthenticationWhenValidAndInvalidAuthorizationHeaderFFEnabled(t *testing.T) {
	t.Setenv(feature.AuthorizationHeader.EnvVariable, "true")
	tests := []AuthHeader{
		{"Valid Token", "Bearer abc", http.StatusOK, false},
		{"Invalid Token", "Bearer invalid", http.StatusNotFound, true},
		{"Missing Token", "", http.StatusFound, true},
	}

	testCheckAuthenticationWhenValidAndInvalidAuthorizationHeader(t, tests)
}

func TestCheckAuthenticationWhenValidAndInvalidAuthorizationHeaderFFDisabled(t *testing.T) {
	t.Setenv(feature.AuthorizationHeader.EnvVariable, "false")
	tests := []AuthHeader{
		{"Valid Token", "Bearer abc", http.StatusFound, true},
		{"Invalid Token", "Bearer invalid", http.StatusFound, true},
		{"Missing Token", "", http.StatusFound, true},
	}

	testCheckAuthenticationWhenValidAndInvalidAuthorizationHeader(t, tests)
}

func TestCheckAuthenticationWithoutProject(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/user":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com/auth?code=1&state=state", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	contentServed := auth.CheckAuthenticationWithoutProject(result, r, &domainMock{projectID: 0})
	require.False(t, contentServed)
	require.Equal(t, http.StatusOK, result.Code)
}

func TestCheckAuthenticationWithoutProjectWhenInvalidToken(t *testing.T) {
	apiServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/user":
			require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "{\"error\":\"invalid_token\"}")
		default:
			t.Logf("Unexpected r.URL.RawPath: %q", r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	apiServer.Start()
	defer apiServer.Close()

	auth := createTestAuth(t, apiServer.URL, "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com/", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	contentServed := auth.CheckAuthenticationWithoutProject(result, r, &domainMock{projectID: 0})
	require.True(t, contentServed)
	require.Equal(t, http.StatusFound, result.Code)
}

func TestGenerateKeys(t *testing.T) {
	keys, err := generateKeys("something-very-secret", 3)
	require.NoError(t, err)
	require.Len(t, keys, 3)

	require.NotEqual(t, fmt.Sprint(string(keys[0])), fmt.Sprint(string(keys[1])))
	require.NotEqual(t, fmt.Sprint(string(keys[0])), fmt.Sprint(string(keys[2])))
	require.NotEqual(t, fmt.Sprint(string(keys[1])), fmt.Sprint(string(keys[2])))

	require.Len(t, keys[0], 32)
	require.Len(t, keys[1], 32)
	require.Len(t, keys[2], 32)
}

func TestGetTokenIfExistsWhenTokenExists(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"access_token": "abc",
	})

	token, err := auth.GetTokenIfExists(result, r)
	require.NoError(t, err)
	require.Equal(t, "abc", token)
}

func TestGetTokenIfExistsWhenTokenDoesNotExist(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://example.com", nil)
	require.NoError(t, err)

	setSessionValues(t, r, auth, map[interface{}]interface{}{})

	token, err := auth.GetTokenIfExists(result, r)
	require.Equal(t, "", token)
	require.NoError(t, err)
}

func TestCheckResponseForInvalidTokenWhenInvalidToken(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()
	reqURL, err := url.Parse("http://pages.gitlab-example.com/test")
	require.NoError(t, err)
	r := &http.Request{URL: reqURL, Host: "pages.gitlab-example.com", RequestURI: "/test"}

	resp := &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(bytes.NewReader([]byte("{\"error\":\"invalid_token\"}")))}
	testhelpers.Close(t, resp.Body)

	require.True(t, auth.CheckResponseForInvalidToken(result, r, resp))
	res := result.Result()
	testhelpers.Close(t, res.Body)
	require.Equal(t, http.StatusFound, res.StatusCode)
	require.Equal(t, "http://pages.gitlab-example.com/test", result.Header().Get("Location"))
}

func TestCheckResponseForInvalidTokenWhenNotInvalidToken(t *testing.T) {
	auth := createTestAuth(t, "", "")

	result := httptest.NewRecorder()
	reqURL, err := url.Parse("/something")
	require.NoError(t, err)
	r := &http.Request{URL: reqURL}

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte("ok")))}

	require.False(t, auth.CheckResponseForInvalidToken(result, r, resp))
}

func TestDomainAllowed(t *testing.T) {
	auth := createTestAuth(t, "", "")
	mockCtrl := gomock.NewController(t)
	mockSource := mock.NewMockSource(mockCtrl)

	testCases := []struct {
		name     string
		expected bool
	}{
		{
			name:     "pages.unrelated-site.com",
			expected: false,
		},
		{
			name:     "prepended-pages.gitlab-example.com",
			expected: false,
		},
		{
			name:     "pages.gitlab-example.com.unrelated-site.com",
			expected: false,
		},
		{
			name:     "pages.gitlab-example.com",
			expected: true,
		},
		{
			name:     "subdomain.pages.gitlab-example.com",
			expected: true,
		},
		{
			name:     "multi.sub.domain.pages.gitlab-example.com",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			if !tc.expected {
				mockSource.EXPECT().GetDomain(ctx, tc.name).Return(nil, nil)
			}

			actual := auth.domainAllowed(ctx, tc.name, mockSource)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func testNamespaceInPath(t *testing.T, allowNamespaceInPath bool, namespace string, expectedPath string, msg string) {
	auth := createTestAuth(t, "", "")
	auth.allowNamespaceInPath = allowNamespaceInPath

	result := httptest.NewRecorder()

	r, err := http.NewRequest("Get", "https://namespace.pages.gitlab-example.com", nil)
	r.Header.Set("X-Gitlab-Namespace-In-Path", namespace)
	require.NoError(t, err)

	domain1 := getDomainWithMockResolver(t, "namespace.pages.gitlab-example.com", 1)
	r = domain.ReqWithDomain(r, domain1)

	// pre-set an state
	setSessionValues(t, r, auth, map[interface{}]interface{}{
		"state":            "given_state",
		namespaceInPathKey: strings.TrimPrefix(expectedPath, "/"),
	})

	contentServed := auth.checkAuthentication(result, r, &domainMock{projectID: 1000})
	require.True(t, contentServed)

	// check if the state was re-used instead of re-created
	session, _ := auth.getSessionFromStore(r)
	require.Equal(t, expectedPath, session.Options.Path, msg)
	require.Equal(t, strings.TrimPrefix(expectedPath, "/"), session.Values[namespaceInPathKey], msg)
}

func TestValidSessionPathWhenNamespaceInPathIsDisabled(t *testing.T) {
	testNamespaceInPath(t,
		false,
		"namespace",
		"/",
		"did not set path as '/' in session options")
}

func TestValidSessionPathWhenNamespaceInPathIsEnabled(t *testing.T) {
	testNamespaceInPath(t,
		true,
		"namespace",
		"/namespace",
		"did not set namespace path in session options")
}

func TestForgedNamespaceWhenNamespaceInPathIsEnabled(t *testing.T) {
	testNamespaceInPath(t,
		true,
		"namespace-forged",
		"/",
		"did not set path as '/' in session options")
}

func getDomainWithMockResolver(t *testing.T, domainName string, namespaceID int) *domain.Domain {
	mockCtrl := gomock.NewController(t)
	mockResolver := dMock.NewMockResolver(mockCtrl)

	mockResolver.EXPECT().
		Resolve(gomock.Any()).
		AnyTimes().
		Return(&serving.Request{
			Serving:    nil,
			LookupPath: &serving.LookupPath{RootNamespaceID: namespaceID},
			SubPath:    "",
		}, nil)

	return domain.New(domainName, "", "", "", namespaceID, mockResolver)
}

func TestAuthStateTTLMatchesOAuthAuthorizationCodeExpiry(t *testing.T) {
	require.Equal(t, 10*time.Minute, authStateTTL)
}

func newTestSession(t *testing.T, auth *Auth, values map[interface{}]interface{}) *hostSession {
	t.Helper()

	r, err := http.NewRequest("GET", "https://example.com/", nil)
	require.NoError(t, err)

	session, err := auth.getSessionFromStore(r)
	require.NoError(t, err)

	for k, v := range values {
		session.Values[k] = v
	}

	return session
}

func TestAuthStateIsReusedAfterASessionRoundTrip(t *testing.T) {
	auth := createTestAuth(t, "", "")
	issuedAt := time.Unix(1700000000, 0)
	auth.now = func() time.Time { return issuedAt }

	r, err := http.NewRequest("GET", "https://example.com/", nil)
	require.NoError(t, err)

	session := newTestSession(t, auth, nil)
	require.NoError(t, auth.refreshAuthState(session))

	state := session.Values[stateKey]
	codeVerifier := session.Values[codeVerifierKey]

	result := httptest.NewRecorder()
	require.NoError(t, session.Save(r, result))

	restored := sessionFromResponse(t, auth, r, result)

	auth.now = func() time.Time { return issuedAt.Add(authStateTTL - time.Second) }
	require.NoError(t, auth.refreshAuthState(restored))

	require.Equal(t, state, restored.Values[stateKey])
	require.Equal(t, codeVerifier, restored.Values[codeVerifierKey])
}

func TestRefreshAuthState(t *testing.T) {
	now := time.Unix(1700000000, 0)

	t.Run("generates a state and a code verifier", func(t *testing.T) {
		auth := createTestAuth(t, "", "")
		auth.now = func() time.Time { return now }

		session := newTestSession(t, auth, nil)
		require.NoError(t, auth.refreshAuthState(session))

		require.NotEmpty(t, session.Values[stateKey])
		require.Equal(t, now.Unix(), session.Values[stateIssuedAtKey])

		codeVerifier, ok := session.Values[codeVerifierKey].(string)
		require.True(t, ok)
		require.Len(t, codeVerifier, 43)
		require.NotContains(t, codeVerifier, "=")
	})

	t.Run("keeps the state and code verifier while they are fresh", func(t *testing.T) {
		auth := createTestAuth(t, "", "")
		auth.now = func() time.Time { return now.Add(authStateTTL - time.Second) }

		session := newTestSession(t, auth, map[interface{}]interface{}{
			stateKey:         "given_state",
			stateIssuedAtKey: now.Unix(),
			codeVerifierKey:  "given_verifier",
		})
		require.NoError(t, auth.refreshAuthState(session))

		require.Equal(t, "given_state", session.Values[stateKey])
		require.Equal(t, "given_verifier", session.Values[codeVerifierKey])
	})

	t.Run("rotates the state and code verifier once they expire", func(t *testing.T) {
		auth := createTestAuth(t, "", "")
		auth.now = func() time.Time { return now.Add(authStateTTL) }

		session := newTestSession(t, auth, map[interface{}]interface{}{
			stateKey:         "given_state",
			stateIssuedAtKey: now.Unix(),
			codeVerifierKey:  "given_verifier",
		})
		require.NoError(t, auth.refreshAuthState(session))

		require.NotEqual(t, "given_state", session.Values[stateKey])
		require.NotEqual(t, "given_verifier", session.Values[codeVerifierKey])
		require.Equal(t, now.Add(authStateTTL).Unix(), session.Values[stateIssuedAtKey])
	})

	t.Run("rotates a state that has no code verifier", func(t *testing.T) {
		auth := createTestAuth(t, "", "")
		auth.now = func() time.Time { return now }

		session := newTestSession(t, auth, map[interface{}]interface{}{
			stateKey:         "given_state",
			stateIssuedAtKey: now.Unix(),
		})
		require.NoError(t, auth.refreshAuthState(session))

		require.NotEqual(t, "given_state", session.Values[stateKey])
		require.NotEmpty(t, session.Values[codeVerifierKey])
	})
}

func TestGetProxyAddress(t *testing.T) {
	auth := createTestAuth(t, "", "")

	r, err := http.NewRequest("GET", "https://example.com/project/", nil)
	require.NoError(t, err)
	r = domain.ReqWithDomain(r, getDomainWithMockResolver(t, "example.com", 1))

	session := newTestSession(t, auth, map[interface{}]interface{}{
		stateKey:        "given_state",
		codeVerifierKey: "ks02i3jdikdo2k0dkfodf3m39rjfjsdk0wk349rj3jrhf",
	})

	proxyAddress, err := url.Parse(auth.getProxyAddress(r, session))
	require.NoError(t, err)
	require.Equal(t, "2i0WFA-0AerkjQm4X4oDEhqA17QIAKNjXpagHBXmO_U", proxyAddress.Query().Get("code_challenge"))
}

func startAuthFlow(t *testing.T, auth *Auth, sessionValues map[interface{}]interface{}) (string, *hostSession) {
	t.Helper()

	r, err := http.NewRequest("GET", "https://example.com/", nil)
	require.NoError(t, err)
	r = domain.ReqWithDomain(r, getDomainWithMockResolver(t, "example.com", 1))

	setSessionValues(t, r, auth, sessionValues)

	result := httptest.NewRecorder()
	require.True(t, auth.checkAuthentication(result, r, &domainMock{projectID: 1000}))
	require.Equal(t, http.StatusFound, result.Code)

	proxyAddress, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)

	return proxyAddress.Query().Get("state"), sessionFromResponse(t, auth, r, result)
}

func TestConcurrentRequestsAfterAuthStateExpiryRotateIndependently(t *testing.T) {
	auth := createTestAuth(t, "", "")
	issuedAt := time.Unix(1700000000, 0)
	auth.now = func() time.Time { return issuedAt.Add(authStateTTL) }

	expiredSession := func() map[interface{}]interface{} {
		return map[interface{}]interface{}{
			stateKey:         "given_state",
			stateIssuedAtKey: issuedAt.Unix(),
			codeVerifierKey:  "given_verifier",
		}
	}

	firstState, _ := startAuthFlow(t, auth, expiredSession())
	secondState, secondSession := startAuthFlow(t, auth, expiredSession())

	require.NotEqual(t, firstState, secondState)

	callback, err := http.NewRequest("GET", "https://example.com/auth?code=1&state="+url.QueryEscape(firstState), nil)
	require.NoError(t, err)

	require.False(t, validateState(callback, secondSession))
}

func TestTryAuthenticateForwardsCodeChallengeToGitLab(t *testing.T) {
	tests := map[string]struct {
		codeChallenge string
		expectedQuery string
		expectedPKCE  bool
	}{
		"without a code challenge": {
			codeChallenge: "",
			expectedQuery: "",
			expectedPKCE:  false,
		},
		"with a code challenge": {
			codeChallenge: "2i0WFA-0AerkjQm4X4oDEhqA17QIAKNjXpagHBXmO_U",
			expectedQuery: "&code_challenge=2i0WFA-0AerkjQm4X4oDEhqA17QIAKNjXpagHBXmO_U&code_challenge_method=S256",
			expectedPKCE:  true,
		},
		"with a code challenge containing query separators": {
			codeChallenge: "challenge&redirect_uri=https://evil.com",
			expectedQuery: "&code_challenge=challenge%26redirect_uri%3Dhttps%3A%2F%2Fevil.com&code_challenge_method=S256",
			expectedPKCE:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			auth := createTestAuth(t, "", "public-gitlab.example.com")
			result := httptest.NewRecorder()

			query := url.Values{}
			query.Set("domain", "https://pages.gitlab-example.com")
			query.Set("state", "state")
			query.Set("root_namespace_id", "1")
			if tt.codeChallenge != "" {
				query.Set("code_challenge", tt.codeChallenge)
			}

			r, err := http.NewRequest("GET", "https://pages.gitlab-example.com/auth?"+query.Encode(), nil)
			require.NoError(t, err)

			mockCtrl := gomock.NewController(t)
			mockSource := mock.NewMockSource(mockCtrl)

			require.True(t, auth.TryAuthenticate(result, r, mockSource))
			require.Equal(t, http.StatusFound, result.Code)

			expectedLocation := "/public-gitlab.example.com/oauth/authorize?client_id=id&redirect_uri=http://pages.gitlab-example.com/auth" +
				"&response_type=code&state=state&scope=scope&root_namespace_id=1" + tt.expectedQuery
			require.Equal(t, expectedLocation, result.Header().Get("Location"))

			session := sessionFromResponse(t, auth, r, result)
			require.Equal(t, tt.expectedPKCE, session.Values[pkceSessionKey] != nil)
		})
	}
}

func sessionFromResponse(t *testing.T, auth *Auth, r *http.Request, result *httptest.ResponseRecorder) *hostSession {
	t.Helper()

	res := result.Result()
	testhelpers.Close(t, res.Body)

	next, err := http.NewRequest("GET", "https://"+r.Host+"/auth", nil)
	require.NoError(t, err)

	for _, cookie := range res.Cookies() {
		next.AddCookie(cookie)
	}

	session, err := auth.getSessionFromStore(next)
	require.NoError(t, err)

	return session
}

func TestTryAuthenticateMarksCallbackRedirectAsPKCE(t *testing.T) {
	tests := map[string]struct {
		callbackQuery string
		sessionValues map[interface{}]interface{}
		expectedPKCE  string
	}{
		"when the authorization request carried a code challenge": {
			callbackQuery: "code=1&state=state",
			sessionValues: map[interface{}]interface{}{
				stateKey:            "state",
				"proxy_auth_domain": "https://domain.com",
				pkceSessionKey:      true,
			},
			expectedPKCE: "1",
		},
		"when the authorization request carried no code challenge": {
			callbackQuery: "code=1&state=state",
			sessionValues: map[interface{}]interface{}{
				stateKey:            "state",
				"proxy_auth_domain": "https://domain.com",
			},
			expectedPKCE: "",
		},
		"when the callback carries a marker the proxy did not record": {
			callbackQuery: "code=1&state=state&pkce=1",
			sessionValues: map[interface{}]interface{}{
				stateKey:            "state",
				"proxy_auth_domain": "https://domain.com",
			},
			expectedPKCE: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			auth := createTestAuth(t, "", "")
			result := httptest.NewRecorder()

			r, err := http.NewRequest("GET", "https://pages.gitlab-example.com/auth?"+tt.callbackQuery, nil)
			require.NoError(t, err)

			setSessionValues(t, r, auth, tt.sessionValues)

			mockCtrl := gomock.NewController(t)
			mockSource := mock.NewMockSource(mockCtrl)

			require.True(t, auth.TryAuthenticate(result, r, mockSource))
			require.Equal(t, http.StatusFound, result.Code)

			redirect, err := url.Parse(result.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, tt.expectedPKCE, redirect.Query().Get(pkceParam))

			session := sessionFromResponse(t, auth, r, result)
			require.Nil(t, session.Values[pkceSessionKey])
		})
	}
}

func TestTryAuthenticateSendsCodeVerifierToTokenEndpoint(t *testing.T) {
	tests := map[string]struct {
		pkceMarker       string
		sessionVerifier  string
		expectedVerifier string
	}{
		"when the callback is marked and the session holds a verifier": {
			pkceMarker:       "1",
			sessionVerifier:  "given_verifier",
			expectedVerifier: "given_verifier",
		},
		"when the callback is not marked": {
			pkceMarker:       "",
			sessionVerifier:  "given_verifier",
			expectedVerifier: "",
		},
		"when the callback marker is not the expected value": {
			pkceMarker:       "0",
			sessionVerifier:  "given_verifier",
			expectedVerifier: "",
		},
		"when the session holds no verifier": {
			pkceMarker:       "1",
			sessionVerifier:  "",
			expectedVerifier: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var tokenRequestForm url.Values

			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/oauth/token", r.URL.Path)

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)

				tokenRequestForm, err = url.ParseQuery(string(body))
				require.NoError(t, err)

				fmt.Fprint(w, "{\"access_token\":\"abc\"}")
			}))
			defer apiServer.Close()

			auth := createTestAuth(t, apiServer.URL, "")

			code, err := auth.EncryptAndSignCode("https://example.com", "1")
			require.NoError(t, err)

			query := url.Values{}
			query.Set("code", code)
			query.Set("state", "state")
			if tt.pkceMarker != "" {
				query.Set(pkceParam, tt.pkceMarker)
			}

			r, err := http.NewRequest("GET", "https://example.com/auth?"+query.Encode(), nil)
			require.NoError(t, err)
			r.URL.Scheme = request.SchemeHTTPS

			sessionValues := map[interface{}]interface{}{
				"uri":    "https://example.com/project/",
				stateKey: "state",
			}
			if tt.sessionVerifier != "" {
				sessionValues[codeVerifierKey] = tt.sessionVerifier
			}

			setSessionValues(t, r, auth, sessionValues)

			result := httptest.NewRecorder()

			mockCtrl := gomock.NewController(t)
			mockSource := mock.NewMockSource(mockCtrl)

			require.True(t, auth.TryAuthenticate(result, r, mockSource))
			require.Equal(t, http.StatusFound, result.Code)

			require.Equal(t, tt.expectedVerifier, tokenRequestForm.Get("code_verifier"))
		})
	}
}
