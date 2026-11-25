package logging_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gitlab-org/labkit/correlation"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
)

func TestWithLoggerMiddleware_InjectsLoggerIntoContext(t *testing.T) {
	var capturedLogger *slog.Logger

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLogger = logv2.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := logging.WithLoggerMiddleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.NotNil(t, capturedLogger, "logger must be present in request context")
	assert.Same(t, logging.Logger, capturedLogger, "injected logger must be the global Logger")
}

func TestWithLoggerMiddleware_DoesNotPanicWithoutPriorLogger(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = logv2.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := logging.WithLoggerMiddleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})
}

func TestWithLoggerMiddleware_PropagatesCorrelationIDThroughLogCalls(t *testing.T) {
	const expectedCorrelationID = "e2e-correlation-456"

	buf := loggingtest.Capture(t, slog.LevelInfo)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enrichedCtx := logging.LogRequest(r)
		logv2.FromContext(enrichedCtx).InfoContext(enrichedCtx, "handler event")
		w.WriteHeader(http.StatusOK)
	})

	handler := correlation.InjectCorrelationID(
		logging.WithLoggerMiddleware(innerHandler),
		correlation.WithPropagation(),
	)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/a/b", nil)
	req.Header.Set("X-Request-Id", expectedCorrelationID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	out := decodeLogLine(t, buf)
	assert.Equal(t, expectedCorrelationID, out["correlation_id"])
	assert.Equal(t, "handler event", out["msg"])
	assert.Equal(t, "/a/b", out["path"])
	assert.Equal(t, "example.com", out["host"])
}
