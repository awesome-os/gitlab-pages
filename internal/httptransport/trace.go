package httptransport

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http/httptrace"
	"time"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

func (mrt *meteredRoundTripper) newTracer(ctx context.Context, start time.Time) *httptrace.ClientTrace {
	logger := logv2.FromContext(ctx)

	trace := &httptrace.ClientTrace{
		GetConn: func(host string) {
			mrt.httpTraceObserve("httptrace.ClientTrace.GetConn", start)

			logger.Log(ctx, logging.LevelTrace, "httptrace.ClientTrace.GetConn", logv2.HTTPHost(host))
		},
		GotConn: func(connInfo httptrace.GotConnInfo) {
			mrt.httpTraceObserve("httptrace.ClientTrace.GotConn", start)

			logger.Log(ctx, logging.LevelTrace, "httptrace.ClientTrace.GotConn",
				slog.Bool("reused", connInfo.Reused),
				slog.Bool("was_idle", connInfo.WasIdle),
				slog.Int64("idle_time_ms", connInfo.IdleTime.Milliseconds()),
			)
		},
		GotFirstResponseByte: func() {
			mrt.httpTraceObserve("httptrace.ClientTrace.GotFirstResponseByte", start)
		},
		DNSStart: func(d httptrace.DNSStartInfo) {
			mrt.httpTraceObserve("httptrace.ClientTrace.DNSStart", start)
		},
		DNSDone: func(d httptrace.DNSDoneInfo) {
			mrt.httpTraceObserve("httptrace.ClientTrace.DNSDone", start)

			logger.Log(ctx, logging.LevelTrace, "httptrace.ClientTrace.DNSDone", logging.NillableError(d.Err))
		},
		ConnectStart: func(net, addr string) {
			mrt.httpTraceObserve("httptrace.ClientTrace.ConnectStart", start)

			logger.Log(ctx, logging.LevelTrace, "httptrace.ClientTrace.ConnectStart",
				slog.String("network", net),
				slog.String("address", addr),
			)
		},
		ConnectDone: func(net string, addr string, err error) {
			mrt.httpTraceObserve("httptrace.ClientTrace.ConnectDone", start)

			if err != nil {
				logger.ErrorContext(ctx, "httptrace.ClientTrace.ConnectDone",
					slog.String("network", net),
					slog.String("address", addr),
					logv2.Error(err),
				)
			}

			logger.Log(ctx, logging.LevelTrace, "httptrace.ClientTrace.ConnectDone",
				slog.String("network", net),
				slog.String("address", addr),
			)
		},
		TLSHandshakeStart: func() {
			mrt.httpTraceObserve("httptrace.ClientTrace.TLSHandshakeStart", start)
		},
		TLSHandshakeDone: func(connState tls.ConnectionState, err error) {
			mrt.httpTraceObserve("httptrace.ClientTrace.TLSHandshakeDone", start)

			if err != nil {
				logger.ErrorContext(ctx, "httptrace.ClientTrace.TLSHandshakeDone",
					slog.Uint64("version", uint64(connState.Version)),
					slog.Bool("connection_resumed", connState.DidResume),
					logv2.Error(err),
				)
			}

			logger.Log(ctx, logging.LevelTrace, "httptrace.ClientTrace.TLSHandshakeDone",
				slog.Uint64("version", uint64(connState.Version)),
				slog.Bool("connection_resumed", connState.DidResume),
			)
		},
	}

	return trace
}

func (mrt *meteredRoundTripper) httpTraceObserve(label string, start time.Time) {
	mrt.tracer.WithLabelValues(label).
		Observe(time.Since(start).Seconds())
}
