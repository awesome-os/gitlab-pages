package gitlabstub

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"time"
)

type config struct {
	pagesHandler       http.HandlerFunc
	tokenHandler       http.HandlerFunc
	pagesAccessHandler http.HandlerFunc
	pagesRoot          string
	delay              time.Duration
	tlsConfig          *tls.Config
}

type Option func(*config)

func defaultTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
}

func WithPagesHandler(ph http.HandlerFunc) Option {
	return func(sc *config) {
		sc.pagesHandler = ph
	}
}

func WithTokenHandler(th http.HandlerFunc) Option {
	return func(sc *config) {
		sc.tokenHandler = th
	}
}

// WithPagesAccessHandler replaces the default handler for
// /api/v4/projects/:id/pages_access.
func WithPagesAccessHandler(ph http.HandlerFunc) Option {
	return func(sc *config) {
		sc.pagesAccessHandler = ph
	}
}

func WithPagesRoot(pagesRoot string) Option {
	return func(sc *config) {
		sc.pagesRoot = pagesRoot
	}
}

func WithDelay(delay time.Duration) Option {
	return func(sc *config) {
		sc.delay = delay
	}
}

func WithCertificate(cert tls.Certificate) Option {
	return func(sc *config) {
		if sc.tlsConfig == nil {
			sc.tlsConfig = defaultTLSConfig()
		}
		sc.tlsConfig.Certificates = append(sc.tlsConfig.Certificates, cert)
	}
}

func WithMutualTLS(caCert *x509.Certificate) Option {
	return func(sc *config) {
		if sc.tlsConfig == nil {
			sc.tlsConfig = defaultTLSConfig()
		}

		sc.tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		sc.tlsConfig.ClientCAs = x509.NewCertPool()
		sc.tlsConfig.ClientCAs.AddCert(caCert)
	}
}
