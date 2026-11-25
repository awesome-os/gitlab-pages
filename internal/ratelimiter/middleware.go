package ratelimiter

import (
	"net/http"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/httperrors"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
)

const (
	headerGitLabRealIP    = "GitLab-Real-IP"
	headerXForwardedFor   = "X-Forwarded-For"
	headerXForwardedProto = "X-Forwarded-Proto"
)

// Middleware returns middleware for rate-limiting clients
func (rl *RateLimiter) Middleware(handler http.Handler) http.Handler {
	if rl.limitPerSecond <= 0.0 {
		return handler
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rl.requestAllowed(r) {
			handler.ServeHTTP(w, r)
			return
		}

		if rl.blockedCount != nil {
			rl.blockedCount.WithLabelValues(rl.name).Inc()
		}

		if rl.closeConnection {
			logging.Logger.InfoContext(logging.LogRequest(r), "request hit rate limit, adding Connection close header in the response.")
			w.Header().Set("Connection", "close")
		}

		rl.logRateLimitedRequest(r)

		httperrors.Serve429(w)
	})
}

func (rl *RateLimiter) logRateLimitedRequest(r *http.Request) {
	// TODO: change to Debug with https://gitlab.com/gitlab-org/gitlab-pages/-/issues/629
	logging.Logger.InfoContext(logging.LogRequest(r), "request hit rate limit",
		"rate_limiter_name", rl.name,
		"scheme", r.URL.Scheme,
		logv2.RemoteAddr(r.RemoteAddr),
		"source_ip", request.GetIPV4orIPV6PrefixWithoutPort(r),
		"x_forwarded_proto", r.Header.Get(headerXForwardedProto),
		"x_forwarded_for", r.Header.Get(headerXForwardedFor),
		"gitlab_real_ip", r.Header.Get(headerGitLabRealIP),
		"rate_limiter_limit_per_second", rl.limitPerSecond,
		"rate_limiter_burst_size", rl.burstSize,
		"close_connection", rl.closeConnection,
	)
}
