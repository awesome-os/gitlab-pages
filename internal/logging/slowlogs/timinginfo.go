package slowlogs

import (
	"context"
	"time"
)

// timingInfo holds the execution time for each handler.
type timingInfo struct {
	MiddlewareTimings map[string]time.Duration
	OtherTimings      map[string]time.Duration
	LastDuration      time.Duration
}

// newTimingInfo creates new instance of timingInfo
func newTimingInfo() *timingInfo {
	return &timingInfo{
		MiddlewareTimings: make(map[string]time.Duration),
		OtherTimings:      make(map[string]time.Duration),
	}
}

// contextKey is used to store the timingInfo in the context.
type contextKey string

const timingContextKey contextKey = "timingInfoKey"

// Recorder records the method execution time. This should be used with defer
func Recorder(ctx context.Context, functionName string) func() {
	startTime := time.Now()
	return func() {
		recordOtherTiming(ctx, functionName, time.Since(startTime))
	}
}

// withTimingInfo adds a timingInfo struct to the context.
func withTimingInfo(ctx context.Context) context.Context {
	return context.WithValue(ctx, timingContextKey, newTimingInfo())
}

// getTimingInfo retrieves the timingInfo from the context.
func getTimingInfo(ctx context.Context) *timingInfo {
	info, ok := ctx.Value(timingContextKey).(*timingInfo)
	if !ok {
		return newTimingInfo()
	}

	return info
}

// recordOtherTiming records the timingInfo.OtherTimings duration into the context.
func recordOtherTiming(ctx context.Context, handlerName string, duration time.Duration) {
	info := getTimingInfo(ctx)
	if info != nil {
		info.OtherTimings[handlerName] = duration
	}
}
