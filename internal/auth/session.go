package auth

import (
	"net/http"
	"strings"

	"github.com/gorilla/sessions"

	"gitlab.com/gitlab-org/gitlab-pages/internal/errortracking"
	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/httperrors"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
)

const (
	sessionHostKey     = "_session_host"
	namespaceInPathKey = "_namespace_in_path"
)

type hostSession struct {
	*sessions.Session
}

func (s *hostSession) Save(r *http.Request, w http.ResponseWriter) error {
	s.Values[sessionHostKey] = r.Host

	return s.Session.Save(r, w)
}

func (s *hostSession) getNamespaceInPathFromSession() string {
	if s.Values[namespaceInPathKey] != nil {
		return s.Values[namespaceInPathKey].(string)
	}
	return ""
}

func (s *hostSession) appendPath(path string) {
	path = strings.Trim(path, "/")
	if feature.ProjectPrefixCookiePath.Enabled() && len(path) > 0 {
		s.Options.Path = strings.TrimSuffix(s.Options.Path, "/") + "/" + path
	}
}

func (a *Auth) getSessionFromStore(r *http.Request) (*hostSession, error) {
	session, err := a.store.Get(r, "gitlab-pages")

	if session != nil {
		namespaceInPath := a.getNamespaceInPath(r)

		// Cookie just for this domain
		session.Options.Path = "/" + namespaceInPath
		session.Options.HttpOnly = true
		session.Options.Secure = request.IsHTTPS(r)
		if !request.IsHTTPS(r) {
			session.Options.SameSite = http.SameSiteDefaultMode
		}
		session.Options.MaxAge = int(a.cookieSessionTimeout.Seconds())

		if session.Values[sessionHostKey] == nil || session.Values[sessionHostKey] != r.Host {
			logging.Logger.InfoContext(logRequest(r), "Resetting session values",
				"Session host", session.Values[sessionHostKey],
				"Request host", r.Host,
				"Namespace in path", namespaceInPath,
			)

			session.Values = make(map[interface{}]interface{})
		}
		if len(namespaceInPath) > 0 {
			session.Values[namespaceInPathKey] = namespaceInPath
		}
	}

	return &hostSession{session}, err
}

func (a *Auth) checkSession(w http.ResponseWriter, r *http.Request) (*hostSession, error) {
	// Create or get session
	session, errsession := a.getSessionFromStore(r)

	if errsession != nil {
		// Save cookie again
		errsave := session.Save(r, w)
		if errsave != nil {
			logging.Logger.ErrorContext(logRequest(r), saveSessionErrMsg, "error", errsave)
			errortracking.CaptureErrWithReqAndStackTrace(errsave, r)
			httperrors.Serve500(w)
			return nil, errsave
		}

		http.Redirect(w, r, getRequestAddress(r), http.StatusFound)
		return nil, errsession
	}

	return session, nil
}
