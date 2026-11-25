package slowlogs

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	logtest "gitlab.com/gitlab-org/labkit/v2/testing/logtest"
)

func TestMiddlewareLogsSlowRequests(t *testing.T) {
	t.Parallel()
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log, recorder := logtest.NewWithRecorder(nil)
	loggerFunc := func(r *http.Request) context.Context { return r.Context() }

	middleware := NewMiddleware(slowHandler, loggerFunc, 0, log)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	middleware.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "Expected HTTP status OK")

	records := recorder.Records()
	require.NotEmpty(t, records, "Expected a log entry for slow requests")

	lastRecord := records[len(records)-1]
	keys := make(map[string]bool)
	for _, attr := range lastRecord.Attrs {
		keys[attr.Key] = true
	}
	require.True(t, keys["middleware_time_distribution"], "Expected 'middleware_time_distribution' in log entry")
	require.True(t, keys["total_execution_time_ms"], "Expected 'total_execution_time_ms' in log entry")

	var totalExecutionTime int64
	for _, attr := range lastRecord.Attrs {
		if attr.Key == "total_execution_time_ms" {
			totalExecutionTime = attr.Value.Int64()
		}
	}
	assert.GreaterOrEqual(t, totalExecutionTime, int64(0), "Expected 'total_execution_time_ms' to be greater than or equal to 0 milliseconds")
}

func TestMiddlewareDoesNotLogFastRequests(t *testing.T) {
	t.Parallel()
	fastHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log, recorder := logtest.NewWithRecorder(nil)
	loggerFunc := func(r *http.Request) context.Context { return r.Context() }

	middleware := NewMiddleware(fastHandler, loggerFunc, 500*time.Millisecond, log)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	middleware.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, recorder.Records(), "Log entry is not expected")
}

func TestPrepareMiddlewareTimingInfoInMS(t *testing.T) {
	t.Parallel()
	timings := map[string]time.Duration{
		"handler1": 150 * time.Millisecond,
		"handler2": 200 * time.Millisecond,
	}

	ctx := context.WithValue(context.Background(), timingContextKey, &timingInfo{
		MiddlewareTimings: timings,
	})

	timingsInMS := prepareMiddlewareTimingInfoInMS(ctx)

	require.NotNil(t, timingsInMS)
	assert.Equal(t, int64(150), timingsInMS["handler1_ms"])
	assert.Equal(t, int64(200), timingsInMS["handler2_ms"])
}

// TestLogHandlerTimingWithChainedHandlers tests LogHandlerTiming with handler chaining.
func TestLogHandlerTimingWithChainedHandlers(t *testing.T) {
	t.Parallel()
	slowHandler := func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(5 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		})
	}

	anotherSlowHandler := func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			handler.ServeHTTP(w, r)
		})
	}

	log, recorder := logtest.NewWithRecorder(nil)
	loggerFunc := func(r *http.Request) context.Context { return r.Context() }

	chainedHandler := LogHandlerTiming(slowHandler(), "slowHandler")
	chainedHandler = LogHandlerTiming(anotherSlowHandler(chainedHandler), "anotherSlowHandler")
	chainedHandler = NewMiddleware(chainedHandler, loggerFunc, 0, log)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	chainedHandler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	records := recorder.Records()
	require.NotEmpty(t, records)

	lastRecord := records[len(records)-1]

	attrMap := make(map[string]slog.Value)
	for _, attr := range lastRecord.Attrs {
		attrMap[attr.Key] = attr.Value
	}

	require.Contains(t, attrMap, "middleware_time_distribution")
	require.Contains(t, attrMap, "total_execution_time_ms")

	executionTimes := attrMap["middleware_time_distribution"].Any().(map[string]int64)
	handler1Duration, ok := executionTimes["slowHandler_ms"]
	require.True(t, ok)
	handler2Duration, ok := executionTimes["anotherSlowHandler_ms"]
	require.True(t, ok)
	totalExecutionTimeMS := attrMap["total_execution_time_ms"].Int64()

	// Assert that the first handler execution time is approximately 5 milliseconds
	assert.GreaterOrEqual(t, handler1Duration, int64(5))
	assert.Less(t, handler1Duration, int64(20)) // Allow some margin

	// Assert that the second handler execution time is approximately 10 milliseconds
	assert.GreaterOrEqual(t, handler2Duration, int64(10))
	assert.Less(t, handler2Duration, int64(25)) // Allow some margin

	assert.GreaterOrEqual(t, totalExecutionTimeMS, int64(15))
	assert.Less(t, totalExecutionTimeMS, int64(30)) // Allow some margin
}
