package internal

import (
	"net/http"
)

// Artifact allows to handle artifact related requests
type Artifact interface {
	TryMakeRequest(w http.ResponseWriter, r *http.Request, token string, responseHandler func(*http.Response) bool) bool
}

// Auth handles the authentication logic
type Auth interface {
	IsAuthSupported() bool
	RequireAuth(w http.ResponseWriter, r *http.Request, domain Domain) bool
	GetTokenIfExists(w http.ResponseWriter, r *http.Request) (string, error)
	CheckResponseForInvalidToken(w http.ResponseWriter, r *http.Request, resp *http.Response) bool
}

type Domain interface {
	GetProjectID(r *http.Request) uint64
	GetProjectPrefix(r *http.Request) string
	ServeNotFoundAuthFailed(w http.ResponseWriter, r *http.Request)
}
