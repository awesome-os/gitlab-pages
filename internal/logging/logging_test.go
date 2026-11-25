package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gitlab-org/labkit/correlation"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"
	"gitlab.com/gitlab-org/labkit/v2/testing/logtest"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

func TestLogRequest_EnrichesLoggerWithCorrelationID(t *testing.T) {
	const expectedCorrelationID = "test-correlation-123"

	buf := &bytes.Buffer{}
	testLogger := logging.NewLogger(&logv2.Config{Writer: buf})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)
	ctx := correlation.ContextWithCorrelation(req.Context(), expectedCorrelationID)
	ctx = logv2.WithLogger(ctx, testLogger)
	req = req.WithContext(ctx)

	enrichedCtx := logging.LogRequest(req)

	logv2.FromContext(enrichedCtx).InfoContext(enrichedCtx, "test event")

	out := decodeLogLine(t, buf)

	assert.Equal(t, expectedCorrelationID, out["correlation_id"])
	assert.Equal(t, "example.com", out["host"])
	assert.Equal(t, "/some/path", out["path"])
}

func TestLogRequest_DoesNotPanicWhenContextEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	var enrichedCtx context.Context
	require.NotPanics(t, func() {
		enrichedCtx = logging.LogRequest(req)
	})

	require.NotPanics(t, func() {
		_ = logv2.FromContext(enrichedCtx)
	})
}

func TestNewLogger_CarriesRequestAttributesFromLogRequestContext(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logging.NewLogger(&logv2.Config{Writer: buf})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)
	req = req.WithContext(correlation.ContextWithCorrelation(req.Context(), "test-correlation-123"))

	logger.ErrorContext(logging.LogRequest(req), "test event")

	out := decodeLogLine(t, buf)
	assert.Equal(t, "test-correlation-123", out["correlation_id"])
	assert.Equal(t, "example.com", out["host"])
	assert.Equal(t, "/some/path", out["path"])
}

func TestLogRequest_ContextLoggerCarriesEachRequestAttributeOnce(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logging.NewLogger(&logv2.Config{Writer: buf})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)
	req = req.WithContext(logv2.WithLogger(req.Context(), logger))

	ctx := logging.LogRequest(req)
	logv2.FromContext(ctx).InfoContext(ctx, "test event")

	line := buf.String()
	assert.Equal(t, 1, strings.Count(line, `"correlation_id"`), line)
	assert.Equal(t, 1, strings.Count(line, `"host"`), line)
	assert.Equal(t, 1, strings.Count(line, `"path"`), line)
}

func TestNewLogger_DerivedLoggerCarriesRequestAttributes(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logging.NewLogger(&logv2.Config{Writer: buf}).With("component", "test")

	req := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)

	logger.ErrorContext(logging.LogRequest(req), "test event")

	out := decodeLogLine(t, buf)
	assert.Equal(t, "test", out["component"])
	assert.Equal(t, "example.com", out["host"])
	assert.Equal(t, "/some/path", out["path"])
}

func TestNewLogger_AddsNoRequestAttributesWithoutLogRequest(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logging.NewLogger(&logv2.Config{Writer: buf})

	logger.ErrorContext(context.Background(), "test event")

	out := decodeLogLine(t, buf)
	assert.NotContains(t, out, "correlation_id")
	assert.NotContains(t, out, "host")
	assert.NotContains(t, out, "path")
}

func decodeLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out), buf.String())

	return out
}

func BenchmarkLogger(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)
	labkit := logv2.NewWithConfig(&logv2.Config{Writer: io.Discard})
	pages := logging.NewLogger(&logv2.Config{Writer: io.Discard})

	b.Run("labkit/background", func(b *testing.B) { benchLog(context.Background(), b, labkit) })
	b.Run("pages/background", func(b *testing.B) { benchLog(context.Background(), b, pages) })
	b.Run("pages/log_request", func(b *testing.B) { benchLog(logging.LogRequest(req), b, pages) })
}

func benchLog(ctx context.Context, b *testing.B, logger *slog.Logger) {
	b.ReportAllocs()
	for b.Loop() {
		logger.ErrorContext(ctx, "test event", "error", "boom")
	}
}

func BenchmarkLogRequest(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)

	b.ReportAllocs()
	for b.Loop() {
		_ = logging.LogRequest(req)
	}
}

func TestWithRequestAttrs_AddsRequestAttributesToALoggerNewLoggerCannotBuild(t *testing.T) {
	recording, recorder := logtest.NewWithRecorder(nil)
	logger := logging.WithRequestAttrs(recording)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)
	logger.InfoContext(logging.LogRequest(req), "test event")

	records := recorder.Records()
	require.Len(t, records, 1)
	require.True(t, slices.ContainsFunc(records[0].Attrs, slog.String("host", "example.com").Equal))
	require.True(t, slices.ContainsFunc(records[0].Attrs, slog.String("path", "/some/path").Equal))
}

func TestWithRequestAttrs_AddsEachAttributeOnceWhenAppliedTwice(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logging.WithRequestAttrs(logging.NewLogger(&logv2.Config{Writer: buf}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)
	logger.InfoContext(logging.LogRequest(req), "test event")

	require.Equal(t, 1, strings.Count(buf.String(), `"host"`))
}
