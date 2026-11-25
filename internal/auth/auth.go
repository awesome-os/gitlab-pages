package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"gitlab.com/gitlab-org/labkit/correlation"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"
	"golang.org/x/crypto/hkdf"

	"gitlab.com/gitlab-org/gitlab-pages/internal"
	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/errortracking"
	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/httperrors"
	"gitlab.com/gitlab-org/gitlab-pages/internal/httptransport"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source"
)

// nolint: gosec // auth constants, not credentials
// gosec: G101: Potential hardcoded credentials
const (
	apiURLUserTemplate       = "%s/api/v4/user"
	apiURLProjectTemplate    = "%s/api/v4/projects/%d/pages_access"
	authorizeURLTemplate     = "%s/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s&scope=%s&root_namespace_id=%s"
	authorizePKCESuffix      = "&code_challenge=%s&code_challenge_method=S256"
	tokenURLTemplate         = "%s/oauth/token"
	callbackPath             = "/auth"
	authorizeProxyTemplate   = "%s?domain=%s&root_namespace_id=%s&state=%s"
	authorizeProxyPKCESuffix = "&code_challenge=%s"

	failAuthErrMsg             = "failed to authenticate request"
	fetchAccessTokenErrMsg     = "fetching access token failed"
	queryParameterErrMsg       = "failed to parse domain query parameter"
	saveSessionErrMsg          = "failed to save the session"
	domainQueryParameterErrMsg = "domain query parameter only supports http/https protocol"
	projectPrefix              = "_project_prefix"
	ssoReauthAttemptedKey      = "sso_reauth_attempted"
	stateKey                   = "state"
	stateIssuedAtKey           = "state_issued_at"
	codeVerifierKey            = "code_verifier"
	pkceSessionKey             = "pkce"
	codeChallengeParam         = "code_challenge"
	pkceParam                  = "pkce"
	pkceParamValue             = "1"

	authStateTTL = 10 * time.Minute
)

var (
	errAuthNotConfigured    = errors.New("authentication is not configured")
	errGenerateKeys         = errors.New("could not generate auth keys")
	errGenerateState        = errors.New("could not generate auth state")
	errGenerateCodeVerifier = errors.New("could not generate code verifier")
)

// Auth handles authenticating users with GitLab API
type Auth struct {
	pagesDomain          string
	clientID             string
	clientSecret         string
	redirectURI          string
	internalGitlabServer string // used for exchanging OAuth code for token and Accessing API and checking if the user has access to the project
	publicGitlabServer   string // used for redirecting users to gitlab on the start of OAuth workflow
	authSecret           string
	authScope            string
	jwtSigningKey        []byte
	jwtExpiry            time.Duration
	apiClient            *http.Client
	store                sessions.Store
	now                  func() time.Time // allows to stub time.Now() easily in tests
	cookieSessionTimeout time.Duration
	allowNamespaceInPath bool
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// TryAuthenticate tries to authenticate user and fetch access token if request is a callback to /auth?
func (a *Auth) TryAuthenticate(w http.ResponseWriter, r *http.Request, domains source.Source) bool {
	if a == nil {
		return false
	}

	session, err := a.checkSession(w, r)
	if err != nil {
		return true
	}

	// Request is for auth
	if r.URL.Path != callbackPath {
		return false
	}

	logging.Logger.InfoContext(logRequest(r), "Receive OAuth authentication callback")

	if a.handleProxyingAuth(session, w, r, domains) {
		return true
	}

	// If callback is not successful
	errorParam := r.URL.Query().Get("error")
	if errorParam != "" {
		logging.Logger.WarnContext(logRequest(r), "OAuth endpoint returned error", "error", errorParam)

		httperrors.Serve401(w)
		return true
	}

	if verifyCodeAndStateGiven(r) {
		a.checkAuthenticationResponse(session, w, r)
		return true
	}

	return false
}

func (a *Auth) checkAuthenticationResponse(session *hostSession, w http.ResponseWriter, r *http.Request) {
	if !validateState(r, session) {
		// State is NOT ok
		logging.Logger.WarnContext(logRequest(r), "Authentication state did not match expected")

		httperrors.Serve401(w)
		return
	}

	redirectURI, ok := session.Values["uri"].(string)
	if !ok {
		logging.Logger.ErrorContext(logRequest(r), "Can not extract redirect uri from session")
		httperrors.Serve500(w)
		return
	}
	cleanRedirectURI := logging.CleanURL(redirectURI)

	decryptedCode, err := a.DecryptCode(r.URL.Query().Get("code"),
		getRequestDomain(r, session.getNamespaceInPathFromSession()))
	if err != nil {
		logging.Logger.ErrorContext(logRequest(r), "failed to decrypt secure code", "error", err)
		errortracking.CaptureErrWithReqAndStackTrace(err, r)
		httperrors.Serve500(w)
		return
	}

	codeVerifier := sessionCodeVerifier(session, r)

	// Fetch access token with authorization code
	token, err := a.fetchAccessToken(r.Context(), decryptedCode, codeVerifier)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			httperrors.Serve404(w)
			return
		}

		// Fetching token not OK
		logging.Logger.ErrorContext(logRequest(r), fetchAccessTokenErrMsg, "error", err, "redirect_uri", cleanRedirectURI)
		errortracking.CaptureErrWithReqAndStackTrace(err, r, errortracking.WithField("redirect_uri", cleanRedirectURI))

		httperrors.Serve503(w)
		return
	}

	// Store access token
	session.Values["access_token"] = token.AccessToken

	// In final /auth call, updating session path with project prefix.
	// This will prevent leaking restricted and private projects/subgroups pages under the same top level group
	// https://gitlab.com/gitlab-org/gitlab-pages/-/issues/1088
	if feature.ProjectPrefixCookiePath.Enabled() && session.Values[projectPrefix] != nil {
		session.appendPath(session.Values[projectPrefix].(string))

		logging.Logger.InfoContext(logRequest(r), "Appending project prefix in session cookie path",
			"Prefix Path", session.Values[projectPrefix].(string))

		// If project prefix is useful anywhere, we can avoid deleting it from session.
		delete(session.Values, projectPrefix)
	}

	err = session.Save(r, w)
	if err != nil {
		logging.Logger.ErrorContext(logRequest(r), saveSessionErrMsg, "error", err)
		errortracking.CaptureErrWithReqAndStackTrace(err, r)

		httperrors.Serve500(w)
		return
	}

	// Redirect back to requested URI
	logging.Logger.InfoContext(logRequest(r), "Authentication was successful, redirecting user back to requested page",
		"redirect_uri", cleanRedirectURI,
		"pkce", codeVerifier != "")

	http.Redirect(w, r, redirectURI, http.StatusFound)
}

func (a *Auth) domainAllowed(ctx context.Context, name string, domains source.Source) bool {
	isConfigured := (name == a.pagesDomain) || strings.HasSuffix(name, "."+a.pagesDomain)

	if isConfigured {
		return true
	}

	domain, err := domains.GetDomain(ctx, name)

	// domain exists and there is no error
	return (domain != nil && err == nil)
}

// nolint: gocyclo // TODO refactor this function https://gitlab.com/gitlab-org/gitlab-pages/-/issues/813
func (a *Auth) handleProxyingAuth(session *hostSession, w http.ResponseWriter, r *http.Request, domains source.Source) bool {
	// handle auth callback e.g. https://gitlab.io/auth?domain=domain&state=state
	if shouldProxyAuthToGitlab(r) {
		domain := r.URL.Query().Get("domain")
		state := r.URL.Query().Get("state")
		rootNamespaceID := r.URL.Query().Get("root_namespace_id")

		proxyurl, err := url.Parse(domain)
		if err != nil {
			logging.Logger.ErrorContext(logRequest(r), queryParameterErrMsg, "domain_query", domain)
			errortracking.CaptureErrWithReqAndStackTrace(err, r, errortracking.WithField("domain_query", domain))

			httperrors.Serve500(w)
			return true
		}

		cleanDomain := logging.CleanURLValue(proxyurl).String()

		// domain query param can only contain https or http URLs.
		if proxyurl.Scheme != "http" && proxyurl.Scheme != "https" {
			logging.Logger.WarnContext(logRequest(r), domainQueryParameterErrMsg, "domain_query", cleanDomain)

			httperrors.Serve401(w)
			return true
		}

		host, _, err := net.SplitHostPort(proxyurl.Host)
		if err != nil {
			host = proxyurl.Host
		}

		if !a.domainAllowed(r.Context(), host, domains) {
			logging.Logger.WarnContext(logRequest(r), "Domain is not configured",
				"domain_query", cleanDomain,
				"domain_host", host,
			)
			httperrors.Serve401(w)
			return true
		}

		logging.Logger.InfoContext(logRequest(r), "User is authenticating via domain", "domain_query", cleanDomain)

		session.Values["proxy_auth_domain"] = domain

		codeChallenge := url.QueryEscape(r.URL.Query().Get(codeChallengeParam))
		if codeChallenge == "" {
			delete(session.Values, pkceSessionKey)
		} else {
			session.Values[pkceSessionKey] = true
		}

		session.appendPath(r.URL.Path)

		err = session.Save(r, w)
		if err != nil {
			logging.Logger.ErrorContext(logRequest(r), saveSessionErrMsg, "error", err)
			errortracking.CaptureErrWithReqAndStackTrace(err, r)

			httperrors.Serve500(w)
			return true
		}

		url := fmt.Sprintf(authorizeURLTemplate, a.publicGitlabServer, a.clientID, a.redirectURI, state, a.authScope, rootNamespaceID)
		if codeChallenge != "" {
			url += fmt.Sprintf(authorizePKCESuffix, codeChallenge)
		}

		logging.Logger.InfoContext(logRequest(r), "Redirecting user to gitlab for oauth",
			"public_gitlab_server", logging.CleanURL(a.publicGitlabServer),
			"domain_query", cleanDomain,
		)

		http.Redirect(w, r, url, http.StatusFound)

		return true
	}

	// If auth request callback should be proxied to custom domain
	// redirect to originating domain set in the cookie as proxy_auth_domain
	if shouldProxyCallbackToCustomDomain(session) {
		// Get domain started auth process
		proxyDomain := session.Values["proxy_auth_domain"].(string)

		logging.Logger.InfoContext(logRequest(r), "Redirecting auth callback to custom domain",
			"proxy_auth_domain", logging.CleanURL(proxyDomain))

		// Clear proxying from session
		delete(session.Values, "proxy_auth_domain")

		pkceUsed := session.Values[pkceSessionKey] != nil
		delete(session.Values, pkceSessionKey)

		session.appendPath(r.URL.Path)

		err := session.Save(r, w)
		if err != nil {
			logging.Logger.ErrorContext(logRequest(r), saveSessionErrMsg, "error", err)
			errortracking.CaptureErrWithReqAndStackTrace(err, r)

			httperrors.Serve500(w)
			return true
		}

		query := r.URL.Query()

		// prevent https://tools.ietf.org/html/rfc6749#section-10.6 and
		// https://gitlab.com/gitlab-org/gitlab-pages/-/issues/262 by encrypting
		// and signing the OAuth code
		signedCode, err := a.EncryptAndSignCode(proxyDomain, query.Get("code"))
		if err != nil {
			logging.Logger.ErrorContext(logRequest(r), saveSessionErrMsg, "error", err)
			errortracking.CaptureErrWithReqAndStackTrace(err, r)

			httperrors.Serve503(w)
			return true
		}

		// prevent forwarding access token, more context on the security issue
		// https://gitlab.com/gitlab-org/gitlab/-/issues/285244#note_451266051
		query.Del("token")

		query.Del(pkceParam)

		// replace code with signed code
		query.Set("code", signedCode)

		if pkceUsed {
			query.Set(pkceParam, pkceParamValue)
		}

		// Redirect pages to originating domain with code and state to finish
		// authentication process
		http.Redirect(w, r, proxyDomain+r.URL.Path+"?"+query.Encode(), http.StatusFound)
		return true
	}

	return false
}

func getRequestAddress(r *http.Request) string {
	if request.IsHTTPS(r) {
		return "https://" + r.Host + r.RequestURI
	}
	return "http://" + r.Host + r.RequestURI
}

func getRequestDomain(r *http.Request, namespace string) string {
	requestDomain := r.Host
	// The guard has to test the same string TrimPrefix removes. Testing bare
	// `namespace` also matched hosts where `namespace+"."` is not a prefix, and
	// TrimPrefix then returned r.Host untouched while "/"+namespace was still
	// appended. With namespace_in_path the namespace is the host itself, so that
	// produced https://host/host -- the duplicated domain in the auth parameter.
	if namespace != "" && strings.HasPrefix(r.Host, namespace+".") {
		requestDomain = strings.TrimPrefix(r.Host, namespace+".") + "/" + namespace
	}

	if request.IsHTTPS(r) {
		return "https://" + requestDomain
	}
	return "http://" + requestDomain
}

func shouldProxyAuthToGitlab(r *http.Request) bool {
	return r.URL.Query().Get("domain") != "" && r.URL.Query().Get("state") != ""
}

func shouldProxyCallbackToCustomDomain(session *hostSession) bool {
	return session.Values["proxy_auth_domain"] != nil
}

func validateState(r *http.Request, session *hostSession) bool {
	state := r.URL.Query().Get("state")
	if state == "" {
		// No state param
		return false
	}

	// Check state
	if session.Values[stateKey] == nil || session.Values[stateKey].(string) != state {
		// State does not match
		return false
	}

	// State ok
	return true
}

func verifyCodeAndStateGiven(r *http.Request) bool {
	return r.URL.Query().Get("code") != "" && r.URL.Query().Get("state") != ""
}

func (a *Auth) fetchAccessToken(ctx context.Context, code, codeVerifier string) (tokenResponse, error) {
	token := tokenResponse{}

	// Prepare request
	fetchURL, err := url.Parse(fmt.Sprintf(tokenURLTemplate, a.internalGitlabServer))
	if err != nil {
		return token, err
	}

	content := url.Values{}
	content.Set("client_id", a.clientID)
	content.Set("client_secret", a.clientSecret)
	content.Set("code", code)
	content.Set("grant_type", "authorization_code")
	content.Set("redirect_uri", a.redirectURI)
	if codeVerifier != "" {
		content.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fetchURL.String(), strings.NewReader(content.Encode()))
	if err != nil {
		return token, logging.CleanURLError(err)
	}

	// Request token
	resp, err := a.apiClient.Do(req)
	if err != nil {
		return token, fmt.Errorf("token endpoint: %w", logging.CleanURLError(err))
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return token, tokenEndpointError(resp)
	}

	// Parse response
	err = json.NewDecoder(resp.Body).Decode(&token)
	if err != nil {
		return token, fmt.Errorf("token endpoint returned %d: %w", resp.StatusCode, err)
	}

	return token, nil
}

// tokenEndpointError names the status and, when the body is the OAuth error
// JSON, the error and its description. It never carries the code or a token.
func tokenEndpointError(resp *http.Response) error {
	var errResp errorResponse
	switch {
	case json.NewDecoder(resp.Body).Decode(&errResp) != nil || errResp.Error == "":
		return fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	case errResp.ErrorDescription == "":
		return fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, errResp.Error)
	}
	return fmt.Errorf("token endpoint returned %d: %s: %s", resp.StatusCode, errResp.Error, errResp.ErrorDescription)
}

func (a *Auth) checkSessionIsValid(w http.ResponseWriter, r *http.Request, domain internal.Domain) *hostSession {
	session, err := a.checkSession(w, r)
	if err != nil {
		return nil
	}

	// redirect to /auth?domain=%s&state=%s
	if !isAuthInHeader(r) && a.checkTokenExists(session, w, r, domain) {
		return nil
	}

	return session
}

func (a *Auth) checkTokenExists(session *hostSession, w http.ResponseWriter, r *http.Request, domain internal.Domain) bool {
	// If no access token redirect to OAuth login page
	if session.Values["access_token"] == nil {
		logging.Logger.DebugContext(logRequest(r), "No access token exists, redirecting user to OAuth2 login")

		if err := a.refreshAuthState(session); err != nil {
			logging.Logger.ErrorContext(logRequest(r), "failed to refresh auth state", "error", err)
			errortracking.CaptureErrWithReqAndStackTrace(err, r)

			httperrors.Serve500(w)
			return true
		}

		session.Values["uri"] = getRequestAddress(r)

		// Clear possible proxying
		delete(session.Values, "proxy_auth_domain")

		if feature.ProjectPrefixCookiePath.Enabled() {
			if prefix := requestProjectPrefix(r, domain); len(prefix) > 1 {
				session.Values[projectPrefix] = prefix
			}
			// After successful authentication, user is redirected to /auth url
			// To utilise same session, appended /auth in session path
			session.appendPath("/auth")
		}

		err := session.Save(r, w)
		if err != nil {
			logging.Logger.ErrorContext(logRequest(r), saveSessionErrMsg, "error", err)
			errortracking.CaptureErrWithReqAndStackTrace(err, r)

			httperrors.Serve500(w)
			return true
		}

		// Because the pages domain might be in public suffix list, we have to
		// redirect to pages domain to trigger authorization flow
		http.Redirect(w,
			r,
			a.getProxyAddress(r, session),
			http.StatusFound)

		return true
	}
	return false
}

func (a *Auth) refreshAuthState(session *hostSession) error {
	if a.authStateIsFresh(session) {
		return nil
	}

	state := securecookie.GenerateRandomKey(16)
	if state == nil {
		return errGenerateState
	}

	session.Values[stateKey] = base64.URLEncoding.EncodeToString(state)
	session.Values[stateIssuedAtKey] = a.now().Unix()

	codeVerifier := securecookie.GenerateRandomKey(32)
	if codeVerifier == nil {
		return errGenerateCodeVerifier
	}

	session.Values[codeVerifierKey] = base64.RawURLEncoding.EncodeToString(codeVerifier)

	return nil
}

func (a *Auth) authStateIsFresh(session *hostSession) bool {
	if _, ok := session.Values[stateKey].(string); !ok {
		return false
	}

	if _, ok := session.Values[codeVerifierKey].(string); !ok {
		return false
	}

	issuedAt, ok := session.Values[stateIssuedAtKey].(int64)
	if !ok {
		return false
	}

	return a.now().Sub(time.Unix(issuedAt, 0)) < authStateTTL
}

func codeChallengeFor(codeVerifier string) string {
	challenge := sha256.Sum256([]byte(codeVerifier))

	return base64.RawURLEncoding.EncodeToString(challenge[:])
}

func sessionCodeVerifier(session *hostSession, r *http.Request) string {
	if r.URL.Query().Get(pkceParam) != pkceParamValue {
		return ""
	}

	codeVerifier, _ := session.Values[codeVerifierKey].(string)

	return codeVerifier
}

func (a *Auth) getProxyAddress(r *http.Request, session *hostSession) string {
	proxyAddress := fmt.Sprintf(authorizeProxyTemplate,
		a.redirectURI,
		getRequestDomain(r, session.getNamespaceInPathFromSession()),
		getRootNamespaceID(r),
		session.Values[stateKey].(string))

	return proxyAddress + fmt.Sprintf(authorizeProxyPKCESuffix, codeChallengeFor(session.Values[codeVerifierKey].(string)))
}

func getRootNamespaceID(r *http.Request) string {
	id, err := domain.FromRequest(r).GetRootNamespaceID()
	if err != nil {
		logging.Logger.ErrorContext(logging.LogRequest(r), "Failed to get RootNamespaceID", "error", err)
		return ""
	}

	if id > 0 {
		return strconv.Itoa(id)
	}

	// Keep lookupPath.RootNamespaceID to support old rails response, remove in 18.6.
	lookupPath, err := domain.FromRequest(r).GetLookupPath(r)
	if err != nil {
		logging.Logger.ErrorContext(logging.LogRequest(r), "RootNamespaceID: failed to get lookupPath", "error", err)
		return ""
	}

	if lookupPath.RootNamespaceID > 0 {
		return strconv.Itoa(lookupPath.RootNamespaceID)
	}

	return ""
}

func destroySession(session *hostSession, w http.ResponseWriter, r *http.Request) {
	logging.Logger.DebugContext(logRequest(r), "Destroying session")

	// Invalidate access token and redirect back for refreshing and re-authenticating
	delete(session.Values, "access_token")
	err := session.Save(r, w)
	if err != nil {
		logging.Logger.ErrorContext(logRequest(r), saveSessionErrMsg, "error", err)
		errortracking.CaptureErrWithReqAndStackTrace(err, r)

		httperrors.Serve500(w)
		return
	}

	http.Redirect(w, r, getRequestAddress(r), http.StatusFound)
}

// IsAuthSupported checks if pages is running with the authentication support
func (a *Auth) IsAuthSupported() bool {
	return a != nil
}

// checkAuthentication checks if user is authenticated and has access to the project
// will return contentServed = false when authFailed = true
func (a *Auth) checkAuthentication(w http.ResponseWriter, r *http.Request, domain internal.Domain) bool {
	logging.Logger.DebugContext(logRequest(r), "Authenticate request")

	if a == nil {
		logging.Logger.ErrorContext(logRequest(r), errAuthNotConfigured.Error())
		errortracking.CaptureErrWithReqAndStackTrace(errAuthNotConfigured, r)

		httperrors.Serve500(w)
		return true
	}

	session := a.checkSessionIsValid(w, r, domain)
	if session == nil {
		return true
	}

	projectID := domain.GetProjectID(r)
	req, err := a.buildAuthRequest(r, projectID, session)
	if err != nil {
		handleAuthError(r, err, failAuthErrMsg)
		httperrors.Serve500(w)
		return true
	}

	resp, err := a.apiClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			httperrors.Serve404(w)
		} else {
			handleAuthError(r, err, "Failed to retrieve info with token")
			domain.ServeNotFoundAuthFailed(w, r)
		}
		return true
	}
	defer resp.Body.Close()

	if checkResponseForInvalidToken(resp, session, w, r, domain) {
		return true
	}

	if resp.StatusCode == http.StatusOK {
		clearReauthMarkerOnSuccess(session, r, w, domain)
		return false
	}

	// A 403 with a cookie session means an expired SAML SSO session; re-auth
	// instead of serving a 404. Scoped to the page-serving path: the artifact
	// path treats a 403-with-token as "no public pipelines".
	if shouldReauthenticateOnForbidden(resp, session, r) {
		return reauthenticateExpiredSession(session, w, r, domain)
	}

	return serveUnexpectedAuthResponse(resp, w, r, domain)
}

// serveUnexpectedAuthResponse logs an unexpected pages_access status, captures
// it for error tracking, and serves the not-found page. It always returns true
// to signal that the response has been handled.
func serveUnexpectedAuthResponse(resp *http.Response, w http.ResponseWriter, r *http.Request, domain internal.Domain) bool {
	err := fmt.Errorf("unexpected response fetching access token status: %d", resp.StatusCode)
	logging.Logger.ErrorContext(logRequest(r), "Unexpected response fetching access token",
		logv2.Error(err),
		logv2.HTTPStatusCode(resp.StatusCode),
	)
	errortracking.CaptureErrWithReqAndStackTrace(err, r)
	domain.ServeNotFoundAuthFailed(w, r)
	return true
}

// buildAuthURL constructs the API URL based on project ID.
func (a *Auth) buildAuthURL(projectID uint64) string {
	if projectID > 0 {
		return fmt.Sprintf(apiURLProjectTemplate, a.internalGitlabServer, projectID)
	}
	return fmt.Sprintf(apiURLUserTemplate, a.internalGitlabServer)
}

// buildAuthRequest creates an authenticated HTTP request.
func (a *Auth) buildAuthRequest(r *http.Request, projectID uint64, session *hostSession) (*http.Request, error) {
	authURL := a.buildAuthURL(projectID)
	req, err := http.NewRequestWithContext(r.Context(), "GET", authURL, nil)
	if err != nil {
		return nil, err
	}
	if isAuthInHeader(r) {
		logging.Logger.InfoContext(logRequest(r), "Private pages accessed using authorization header")
		req.Header.Add("Authorization", r.Header.Get("Authorization"))
	} else {
		req.Header.Add("Authorization", "Bearer "+session.Values["access_token"].(string))
	}

	// Forward the client's real IP so the GitLab API can evaluate
	// group IP allowlists correctly.
	if ip := request.GetRemoteAddrWithoutPort(r); ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}

	return req, nil
}

// handleAuthError logs the error and captures it in error tracking.
func handleAuthError(r *http.Request, err error, message string) {
	cleanedErr := logging.CleanURLError(err)
	logging.Logger.ErrorContext(logRequest(r), message, "error", cleanedErr)
	errortracking.CaptureErrWithReqAndStackTrace(cleanedErr, r)
}

// CheckAuthenticationWithoutProject checks if user is authenticated and has a valid token
func (a *Auth) CheckAuthenticationWithoutProject(w http.ResponseWriter, r *http.Request, domain internal.Domain) bool {
	if a == nil {
		// No auth supported
		return false
	}

	return a.checkAuthentication(w, r, domain)
}

// GetTokenIfExists returns the token if it exists
func (a *Auth) GetTokenIfExists(w http.ResponseWriter, r *http.Request) (string, error) {
	if a == nil {
		return "", nil
	}

	session, err := a.checkSession(w, r)
	if err != nil {
		return "", errors.New("error retrieving the session")
	}

	if session.Values["access_token"] != nil {
		return session.Values["access_token"].(string), nil
	}

	return "", nil
}

// RequireAuth will trigger authentication flow if no token exists
func (a *Auth) RequireAuth(w http.ResponseWriter, r *http.Request, domain internal.Domain) bool {
	return a.checkSessionIsValid(w, r, domain) == nil
}

// CheckResponseForInvalidToken checks response for invalid token and destroys session if it was invalid
func (a *Auth) CheckResponseForInvalidToken(w http.ResponseWriter, r *http.Request,
	resp *http.Response,
) bool {
	if a == nil {
		// No auth supported
		return false
	}

	session, err := a.checkSession(w, r)
	if err != nil {
		return true
	}

	if checkResponseForInvalidToken(resp, session, w, r, nil) {
		return true
	}

	return false
}

func (a *Auth) getNamespaceInPath(r *http.Request) string {
	if !a.allowNamespaceInPath {
		return ""
	}

	namespaceInPath := r.Header.Get("X-Gitlab-Namespace-In-Path")
	if namespaceInPath == "" || !strings.HasPrefix(r.Host, namespaceInPath+"."+a.pagesDomain) {
		return ""
	}

	return namespaceInPath
}

func setSessionProjectPrefixPath(session *hostSession, r *http.Request, d internal.Domain) {
	if !feature.ProjectPrefixCookiePath.Enabled() || d == nil {
		return
	}
	if ns := session.getNamespaceInPathFromSession(); len(ns) > 0 {
		session.Options.Path = "/" + ns
	}
	session.appendPath(requestProjectPrefix(r, d))
}

func checkResponseForInvalidToken(resp *http.Response, session *hostSession, w http.ResponseWriter, r *http.Request, d internal.Domain) bool {
	if resp.StatusCode == http.StatusUnauthorized {
		errResp := errorResponse{}

		// Parse response
		defer resp.Body.Close()
		err := json.NewDecoder(resp.Body).Decode(&errResp)
		if err != nil {
			errortracking.CaptureErrWithReqAndStackTrace(err, r)
			return false
		}

		if isAuthInHeader(r) {
			logging.Logger.WarnContext(logRequest(r), "Authorization token provided in request header was invalid")
			httperrors.Serve404(w)
			return true
		} else if errResp.Error == "invalid_token" {
			// Token is invalid
			logging.Logger.WarnContext(logRequest(r), "Access token was invalid, destroying session")

			setSessionProjectPrefixPath(session, r, d)
			destroySession(session, w, r)
			return true
		}
	}

	return false
}

// clearReauthMarkerOnSuccess clears the re-auth marker so a future SSO expiry can recover again.
func clearReauthMarkerOnSuccess(session *hostSession, r *http.Request, w http.ResponseWriter, d internal.Domain) {
	if session.Values[ssoReauthAttemptedKey] == nil {
		return
	}

	delete(session.Values, ssoReauthAttemptedKey)
	setSessionProjectPrefixPath(session, r, d)
	if err := session.Save(r, w); err != nil {
		logging.Logger.ErrorContext(logRequest(r), saveSessionErrMsg, logv2.Error(err))
		errortracking.CaptureErrWithReqAndStackTrace(err, r)
	}
}

// shouldReauthenticateOnForbidden returns true for a 403 with a cookie-based
// token and no prior re-auth attempt.
func shouldReauthenticateOnForbidden(resp *http.Response, session *hostSession, r *http.Request) bool {
	return resp.StatusCode == http.StatusForbidden &&
		!isAuthInHeader(r) &&
		session.Values["access_token"] != nil &&
		session.Values[ssoReauthAttemptedKey] == nil
}

// reauthenticateExpiredSession marks that a re-auth was attempted and destroys
// the session. The cookie path must match the original auth cookie so the
// marker overwrites the stale one in the browser.
func reauthenticateExpiredSession(session *hostSession, w http.ResponseWriter, r *http.Request, d internal.Domain) bool {
	logging.Logger.WarnContext(logRequest(r),
		"Access forbidden, likely an expired SSO session; destroying session and re-authenticating")

	session.Values[ssoReauthAttemptedKey] = true
	setSessionProjectPrefixPath(session, r, d)

	destroySession(session, w, r)
	return true
}

// requestProjectPrefix returns the project prefix of the domain written in the
// letter case of the request path. Project prefixes are resolved
// case-insensitively, but browsers match cookie paths case-sensitively, so a
// cookie path taken from the API prefix is never sent back for a request path
// that uses different letter case. The prefix is returned unchanged when the
// request path does not start with it.
func requestProjectPrefix(r *http.Request, d internal.Domain) string {
	prefix := d.GetProjectPrefix(r)
	trimmed := strings.TrimSuffix(prefix, "/")

	if len(r.URL.Path) < len(trimmed) {
		return prefix
	}

	requested, rest := r.URL.Path[:len(trimmed)], r.URL.Path[len(trimmed):]
	if !strings.EqualFold(requested, trimmed) || (rest != "" && rest[0] != '/') {
		return prefix
	}

	return requested + prefix[len(trimmed):]
}

func isAuthInHeader(r *http.Request) bool {
	if !feature.AuthorizationHeader.Enabled() {
		return false
	}
	token := strings.TrimSpace(r.Header.Get("Authorization"))
	return token != ""
}

func logRequest(r *http.Request) context.Context {
	return logging.LogRequest(r)
}

// generateKeys derives count hkdf keys from a secret, ensuring the key is
// the same for the same secret used across multiple instances
func generateKeys(secret string, count int) ([][]byte, error) {
	keys := make([][]byte, count)
	hkdfReader := hkdf.New(sha256.New, []byte(secret), []byte{}, []byte("PAGES_SIGNING_AND_ENCRYPTION_KEY"))

	for i := 0; i < count; i++ {
		key := make([]byte, 32)
		if _, err := io.ReadFull(hkdfReader, key); err != nil {
			return nil, err
		}

		keys[i] = key
	}

	if len(keys) < count {
		return nil, errGenerateKeys
	}

	return keys, nil
}

// Options carry required auth parameters used to populate Auth struct
type Options struct {
	PagesDomain          string
	StoreSecret          string
	ClientID             string
	ClientSecret         string
	RedirectURI          string
	InternalGitlabServer string
	PublicGitlabServer   string
	AuthScope            string
	AuthTimeout          time.Duration
	CookieSessionTimeout time.Duration
	AllowNamespaceInPath bool
	ClientCfg            config.HTTPClientCfg
}

// New when authentication supported this will be used to create authentication handler
func New(options *Options) (*Auth, error) {
	// generate 3 keys, 2 for the cookie store and 1 for JWT signing
	keys, err := generateKeys(options.StoreSecret, 3)
	if err != nil {
		return nil, err
	}
	httpTransport := httptransport.NewTransportWithClientCert(options.ClientCfg)

	return &Auth{
		pagesDomain:          options.PagesDomain,
		clientID:             options.ClientID,
		clientSecret:         options.ClientSecret,
		redirectURI:          options.RedirectURI,
		internalGitlabServer: strings.TrimRight(options.InternalGitlabServer, "/"),
		publicGitlabServer:   strings.TrimRight(options.PublicGitlabServer, "/"),
		apiClient: &http.Client{
			Timeout: options.AuthTimeout,
			// Rails logs the same id, so a failure here joins the request that caused it.
			Transport: correlation.NewInstrumentedRoundTripper(httpTransport,
				correlation.WithClientName("gitlab-pages-auth")),
		},
		store:                sessions.NewCookieStore(keys[0], keys[1]),
		authSecret:           options.StoreSecret,
		authScope:            options.AuthScope,
		jwtSigningKey:        keys[2],
		jwtExpiry:            time.Minute,
		now:                  time.Now,
		cookieSessionTimeout: options.CookieSessionTimeout,
		allowNamespaceInPath: options.AllowNamespaceInPath,
	}, nil
}
