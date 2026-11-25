package slowlogs

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// LoggerFunc returns a context enriched with per-request fields that will be
// attached to any slow-log line emitted for that request.
type LoggerFunc func(r *http.Request) context.Context

// NewMiddleware returns middleware which logs timing info if downstream time is more than 0.5 seconds
func NewMiddleware(handler http.Handler, logger LoggerFunc, slowRequestThreshold time.Duration, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// update the request context with timing info
		ctx := withTimingInfo(r.Context())
		r = r.WithContext(ctx)

		handler.ServeHTTP(w, r)

		totalDownstreamTime := time.Since(start)
		if totalDownstreamTime >= slowRequestThreshold {
			logCtx := logger(r)
			log.InfoContext(logCtx, "Request time exceeded 0.5 seconds",
				"total_execution_time_ms", totalDownstreamTime.Milliseconds(),
				"middleware_time_distribution", prepareMiddlewareTimingInfoInMS(r.Context()),
				"other_time_distribution", prepareOtherTimingInfoInMS(r.Context()),
			)
		}
	})
}

// prepareMiddlewareTimingInfoLog return timing info as map of string and duration in milliseconds.
func prepareMiddlewareTimingInfoInMS(ctx context.Context) map[string]int64 {
	info := ctx.Value(timingContextKey).(*timingInfo)

	return prepareTimingInfoInMS(info.MiddlewareTimings)
}

// prepareOtherTimingInfoLog return timing info as map of string and duration in milliseconds.
func prepareOtherTimingInfoInMS(ctx context.Context) map[string]int64 {
	info := ctx.Value(timingContextKey).(*timingInfo)

	return prepareTimingInfoInMS(info.OtherTimings)
}

func prepareTimingInfoInMS(timingDurations map[string]time.Duration) map[string]int64 {
	timingsInMillis := make(map[string]int64)
	for key, duration := range timingDurations {
		timingsInMillis[key+"_ms"] = duration.Milliseconds()
	}
	return timingsInMillis
}

// LogHandlerTiming calculates handler execution time excluding downstream handler execution time
func LogHandlerTiming(handler http.Handler, handlerName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startLogTime := time.Now()
		defer calculateMiddlewareExecutionTime(r.Context(), startLogTime, handlerName)
		handler.ServeHTTP(w, r)
	})
}

func calculateMiddlewareExecutionTime(ctx context.Context, startTime time.Time, handlerName string) {
	info := getTimingInfo(ctx)
	spentTime := time.Since(startTime)
	info.MiddlewareTimings[handlerName] = spentTime - info.LastDuration
	info.LastDuration = spentTime
}
