/*
Package tls provides functionality for TLS certificate management and configuration.

This package is responsible for:
- Configuring TLS settings for the application
- Managing certificate retrieval for domains
- Handling client certificate verification
*/
package tls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/ratelimiter"
	"gitlab.com/gitlab-org/gitlab-pages/metrics"
)

var preferredCipherSuites = []uint16{
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_AES_128_GCM_SHA256,
	tls.TLS_AES_256_GCM_SHA384,
	tls.TLS_CHACHA20_POLY1305_SHA256,
}

// error_type values recorded when the wildcard certificate is served as a fallback.
const (
	errorTypeDomainLookupFailed    = "domain_lookup_failed"
	errorTypeCertificateLoadFailed = "certificate_load_failed"
)

// GetCertificateFunc returns the certificate to be used for given domain
type GetCertificateFunc func(*tls.ClientHelloInfo) (*tls.Certificate, error)

// GetConfigFunc returns a tls.Config with populated client
// auth values.
type GetConfigFunc func(*tls.ClientHelloInfo) (*tls.Config, error)

// GetDomainFunc returns a domain for a given hostname
type GetDomainFunc func(context.Context, string) (*domain.Domain, error)

// GetTLSConfig initializes tls.Config based on config flags
// It takes a domain lookup function rather than a certificate function directly
// to improve separation of concerns - the TLS package shouldn't need to know
// how to find domains, only how to get certificates from them
// nolint: unparam // error return is used for certificate and config creation errors
func GetTLSConfig(cfg *config.Config, getDomain GetDomainFunc, getConfigByServerName GetConfigFunc) (*tls.Config, error) {
	wildcardCertificate, err := tls.X509KeyPair(cfg.General.RootCertificate, cfg.General.RootKey)
	if err != nil {
		return nil, err
	}

	getCertificate := func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
		// No SNI (optional per RFC 6066) leaves no hostname to resolve, so
		// serve the wildcard certificate.
		if info.ServerName == "" {
			return &wildcardCertificate, nil
		}

		// Golang calls tls.Config.GetCertificate only if it's set and
		// 1. ServerName != ""
		// 2. Or tls.Config.Certificates is empty array
		// tls.Config.Certificates contain wildcard certificate
		// We want to implement rate limits via GetCertificate, so we need to call it every time
		// So we don't set tls.Config.Certificates, but simulate the behavior of golang:
		// 1. try to get certificate by name
		// 2. if we can't, fallback to default(wildcard) certificate
		d, err := getDomain(context.Background(), info.ServerName)
		if err != nil {
			// Unknown hosts are expected and not worth logging.
			if !errors.Is(err, domain.ErrDomainDoesNotExist) {
				logCertificateError(info, errorTypeDomainLookupFailed, "failed to resolve domain for TLS handshake, serving wildcard certificate", err)
			}
			return &wildcardCertificate, nil
		}

		if d != nil {
			cert, err := d.EnsureCertificate()
			if err != nil {
				// Domains without a custom certificate (e.g. *.gitlab.io) are expected.
				if !errors.Is(err, domain.ErrNoCertificate) {
					logCertificateError(info, errorTypeCertificateLoadFailed, "failed to load custom domain certificate, serving wildcard certificate", err)
				}
				return &wildcardCertificate, nil
			}
			return cert, nil
		}

		return &wildcardCertificate, nil
	}

	TLSDomainRateLimiter := ratelimiter.New(
		"tls_connections_by_domain",
		ratelimiter.WithTLSKeyFunc(ratelimiter.TLSHostnameKey),
		ratelimiter.WithCacheMaxSize(ratelimiter.DefaultDomainCacheSize),
		ratelimiter.WithCachedEntriesMetric(metrics.RateLimitCachedEntries),
		ratelimiter.WithCachedRequestsMetric(metrics.RateLimitCacheRequests),
		ratelimiter.WithBlockedCountMetric(metrics.RateLimitBlockedCount),
		ratelimiter.WithLimitPerSecond(cfg.RateLimit.TLSDomainLimitPerSecond),
		ratelimiter.WithBurstSize(cfg.RateLimit.TLSDomainBurst),
		ratelimiter.WithBypassCIDRs(cfg.RateLimit.RateLimitBypassCIDRs),
	)

	TLSSourceIPRateLimiter := ratelimiter.New(
		"tls_connections_by_source_ip",
		ratelimiter.WithTLSKeyFunc(ratelimiter.TLSClientIPKey),
		ratelimiter.WithCacheMaxSize(ratelimiter.DefaultSourceIPCacheSize),
		ratelimiter.WithCachedEntriesMetric(metrics.RateLimitCachedEntries),
		ratelimiter.WithCachedRequestsMetric(metrics.RateLimitCacheRequests),
		ratelimiter.WithBlockedCountMetric(metrics.RateLimitBlockedCount),
		ratelimiter.WithLimitPerSecond(cfg.RateLimit.TLSSourceIPLimitPerSecond),
		ratelimiter.WithBurstSize(cfg.RateLimit.TLSSourceIPBurst),
		ratelimiter.WithBypassCIDRs(cfg.RateLimit.RateLimitBypassCIDRs),
	)

	getCertificate = TLSDomainRateLimiter.GetCertificateMiddleware(getCertificate)
	getCertificate = TLSSourceIPRateLimiter.GetCertificateMiddleware(getCertificate)

	tlsConfig, err := getTLSConfig(cfg.TLS, getCertificate)
	if err != nil {
		return nil, err
	}

	tlsConfig.GetConfigForClient = func(info *tls.ClientHelloInfo) (*tls.Config, error) {
		return getOptionalConfig(cfg, info, getCertificate, getConfigByServerName)
	}

	return tlsConfig, nil
}

// logCertificateError records that the wildcard certificate is served because
// the domain certificate could not be resolved.
func logCertificateError(info *tls.ClientHelloInfo, errorType, msg string, err error) {
	metrics.TLSCertificateErrors.WithLabelValues(errorType).Inc()

	attrs := []slog.Attr{
		logv2.HTTPHost(info.ServerName),
		logv2.ErrorType(errorType),
		logv2.Error(err),
		logv2.RemoteAddr(info.Conn.RemoteAddr().String()),
	}

	logging.Logger.LogAttrs(info.Context(), slog.LevelWarn, msg, attrs...)
}

func getOptionalConfig(cfg *config.Config, info *tls.ClientHelloInfo, getCertificate GetCertificateFunc, getConfigByServerName GetConfigFunc) (*tls.Config, error) {
	customConfig, err := getConfigByServerName(info)

	if customConfig != nil || err != nil {
		customConfig.GetCertificate = getCertificate
		return customConfig, err
	}

	if cfg.TLS.ClientAuth == tls.NoClientCert {
		return nil, nil
	}

	for _, i := range cfg.TLS.ClientAuthDomains {
		if i != info.ServerName {
			continue
		}
		tlsConfig, err := getTLSConfig(cfg.TLS, getCertificate)
		if err != nil {
			return nil, err
		}
		tlsConfig.ClientAuth = cfg.TLS.ClientAuth
		return tlsConfig, nil
	}

	return nil, nil
}

func getTLSConfig(cfgTLS config.TLS, getCertificateByServerName GetCertificateFunc) (*tls.Config, error) {
	// set MinVersion to fix gosec: G402
	tlsConfig := &tls.Config{GetCertificate: getCertificateByServerName, MinVersion: tls.VersionTLS12}

	// GoLang 1.23 no longer supports insecure ciphers.
	// Therefore, starting from GitLab 17.10, the insecure-ciphers flag is ignored.
	tlsConfig.CipherSuites = preferredCipherSuites

	tlsConfig.MinVersion = cfgTLS.MinVersion
	tlsConfig.MaxVersion = cfgTLS.MaxVersion

	if len(cfgTLS.ClientAuthDomains) == 0 {
		tlsConfig.ClientAuth = cfgTLS.ClientAuth
	}

	if cfgTLS.ClientAuth > tls.RequestClientCert {
		caCert, err := os.ReadFile(cfgTLS.ClientCert)
		if err != nil {
			return nil, err
		}
		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(caCert)
		tlsConfig.ClientCAs = certPool
	}

	return tlsConfig, nil
}

// GetConfigForClient returns a TLS config for a client based on the server name
// This handles client certificate authentication when required by the domain
func GetConfigForClient(info *tls.ClientHelloInfo, getDomain GetDomainFunc) (*tls.Config, error) {
	if info.ServerName == "" {
		return nil, nil
	}
	domain, err := getDomain(info.Context(), info.ServerName)
	if err != nil {
		return nil, fmt.Errorf("error getting domain for client config %s: %w", info.ServerName, err)
	}

	if domain == nil {
		return nil, nil
	}

	certPool, err := domain.EnsureClientCertPool()
	if err != nil {
		return nil, fmt.Errorf("error ensuring client cert pool for %s: %w", info.ServerName, err)
	}

	if certPool == nil {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientCAs:  certPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
	}

	return tlsConfig, nil
}
