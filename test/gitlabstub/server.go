package gitlabstub

import (
	"net/http/httptest"
	"os"

	"github.com/gorilla/mux"
)

func NewUnstartedServer(opts ...Option) (*httptest.Server, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	conf := &config{
		pagesRoot: wd,
	}

	for _, so := range opts {
		so(conf)
	}

	if conf.pagesHandler == nil {
		conf.pagesHandler = defaultAPIHandler(conf.delay, conf.pagesRoot)
	}

	router := mux.NewRouter()

	router.HandleFunc("/api/v4/internal/pages", conf.pagesHandler)

	if conf.tokenHandler == nil {
		conf.tokenHandler = defaultAuthHandler()
	}
	router.HandleFunc("/oauth/token", conf.tokenHandler)

	userHandler := defaultUserHandler()
	router.HandleFunc("/api/v4/user", userHandler)

	if conf.pagesAccessHandler == nil {
		conf.pagesAccessHandler = handleAccessControlRequests
	}
	router.HandleFunc("/api/v4/projects/{project_id:[0-9]+}/pages_access", conf.pagesAccessHandler)

	router.PathPrefix("/").HandlerFunc(handleAccessControlArtifactRequests)

	s := httptest.NewUnstartedServer(router)
	s.TLS = conf.tlsConfig

	return s, nil
}
