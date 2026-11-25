package ratelimiter

import (
	"crypto/tls"
	"errors"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

var ErrTLSRateLimited = errors.New("too many connections, please retry later")

type GetCertificateFunc func(*tls.ClientHelloInfo) (*tls.Certificate, error)

func (rl *RateLimiter) GetCertificateMiddleware(getCertificate GetCertificateFunc) GetCertificateFunc {
	if rl.limitPerSecond <= 0.0 {
		return getCertificate
	}

	return func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if rl.allowed(rl.tlsKeyFunc(info), TLSClientIPKey(info)) {
			return getCertificate(info)
		}

		rl.logRateLimitedTLS(info)

		if rl.blockedCount != nil {
			rl.blockedCount.WithLabelValues(rl.name).Inc()
		}

		return nil, ErrTLSRateLimited
	}
}

func (rl *RateLimiter) logRateLimitedTLS(info *tls.ClientHelloInfo) {
	logging.Logger.Info("TLS connection rate-limited",
		"rate_limiter_name", rl.name,
		"source_ip", TLSClientIPKey(info),
		"req_host", info.ServerName,
		"rate_limiter_limit_per_second", rl.limitPerSecond,
		"rate_limiter_burst_size", rl.burstSize,
	)
}
