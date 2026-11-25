package httptransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

const (
	// DefaultTTFBTimeout is the timeout used in the meteredRoundTripper
	// when calling http.Transport.RoundTrip. The request will be cancelled
	// if the response takes longer than this.
	DefaultTTFBTimeout = 30 * time.Second
)

var (
	sysPoolOnce = &sync.Once{}
	sysPool     *x509.CertPool

	// only overridden by transport_darwin.go
	loadExtraCerts = func() {}
	// DefaultTransport can be used with http.Client with TLS and certificates
	DefaultTransport = NewTransport()
)

// Transport wraps a RoundTripper so it can be extended and modified outside of this package
type Transport interface {
	http.RoundTripper
	RegisterProtocol(scheme string, rt http.RoundTripper)
}

// NewTransport initializes an http.Transport with a custom dialer that includes TLS Root CAs.
// It sets default connection values such as timeouts and max idle connections.
func NewTransport() *http.Transport {
	return &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var dialer tls.Dialer
			dialer.Config = &tls.Config{
				RootCAs:    pool(),
				MinVersion: tls.VersionTLS12,
			}
			return dialer.DialContext(ctx, network, addr)
		},
		Proxy: http.ProxyFromEnvironment,
		// overrides the DefaultMaxIdleConnsPerHost = 2
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		// Set more timeouts https://gitlab.com/gitlab-org/gitlab-pages/-/issues/495
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 15 * time.Second,
	}
}

// NewTransportWithClientCert creates a new http.Transport with the provided client certificate configuration.
// It sets up the TLS configuration with the provided CA files and client certificates.
// The transport is configured with default connection values such as timeouts and max idle connections.
func NewTransportWithClientCert(clientCfg config.HTTPClientCfg) *http.Transport {
	certPool := pool()

	for _, caFile := range clientCfg.CAFiles {
		cert, err := os.ReadFile(caFile)
		if err == nil {
			certPool.AppendCertsFromPEM(cert)
		} else {
			logging.Logger.ErrorContext(context.Background(), "reading CA file",
				logv2.Error(err),
				slog.String("ca-file", caFile),
			)
		}
	}

	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		MinVersion: tls.VersionTLS12, // set MinVersion to fix gosec: G402
	}

	tlsConfig.MinVersion = clientCfg.MinVersion
	tlsConfig.MaxVersion = clientCfg.MaxVersion

	if clientCfg.Cert != nil {
		tlsConfig.Certificates = []tls.Certificate{*clientCfg.Cert}
	}

	t := NewTransport()
	t.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var dialer tls.Dialer
		dialer.Config = tlsConfig
		return dialer.DialContext(ctx, network, addr)
	}
	t.TLSClientConfig = tlsConfig

	return t
}

// This is here because macOS does not support the SSL_CERT_FILE and
// SSL_CERT_DIR environment variables. We have arranged things to read
// SSL_CERT_FILE and SSL_CERT_DIR  as late as possible to avoid conflicts
// with file descriptor passing at startup.
func pool() *x509.CertPool {
	sysPoolOnce.Do(loadPool)
	return sysPool
}

func loadPool() {
	var err error

	// Always load the system cert pool
	sysPool, err = x509.SystemCertPool()
	if err != nil {
		logging.Logger.ErrorContext(context.Background(), "failed to load system cert pool for http client", logv2.Error(err))
		return
	}

	// Go does not load SSL_CERT_FILE and SSL_CERT_DIR on darwin systems so we need to
	// load them manually in OSX. See https://golang.org/src/crypto/x509/root_unix.go
	loadExtraCerts()
}
