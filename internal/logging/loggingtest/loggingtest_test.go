package loggingtest_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/loggingtest"
)

func TestCaptureWritesLinesWithTheirRequestFields(t *testing.T) {
	buf := loggingtest.Capture(t, slog.LevelInfo)

	r := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)
	logging.Logger.InfoContext(logging.LogRequest(r), "test event")

	require.Contains(t, buf.String(), `"msg":"test event"`)
	require.Contains(t, buf.String(), `"host":"example.com"`)
	require.Contains(t, buf.String(), `"path":"/some/path"`)
}

func TestCaptureDropsLinesBelowTheLevel(t *testing.T) {
	buf := loggingtest.Capture(t, slog.LevelWarn)

	logging.Logger.Info("too quiet")
	logging.Logger.Warn("loud enough")

	require.NotContains(t, buf.String(), "too quiet")
	require.Contains(t, buf.String(), "loud enough")
}

func TestCaptureRestoresTheLoggerWhenTheTestEnds(t *testing.T) {
	before := logging.Logger

	t.Run("capturing", func(t *testing.T) {
		loggingtest.Capture(t, slog.LevelInfo)
		require.NotSame(t, before, logging.Logger)
	})

	require.Same(t, before, logging.Logger)
}

func TestRecordKeepsRecordsWithTheirRequestFields(t *testing.T) {
	recorder := loggingtest.Record(t)

	r := httptest.NewRequest(http.MethodGet, "http://example.com/some/path", nil)
	logging.Logger.InfoContext(logging.LogRequest(r), "test event")

	records := recorder.Records()
	require.Len(t, records, 1)
	require.Equal(t, "test event", records[0].Message)
	require.True(t, slices.ContainsFunc(records[0].Attrs, slog.String("host", "example.com").Equal))
	require.True(t, slices.ContainsFunc(records[0].Attrs, slog.String("path", "/some/path").Equal))
}

func TestSwappingTheLoggerRefusesToRunInAParallelTest(t *testing.T) {
	helpers := map[string]func(*testing.T){
		"Capture": func(t *testing.T) { loggingtest.Capture(t, slog.LevelInfo) },
		"Record":  func(t *testing.T) { loggingtest.Record(t) },
	}

	for name, swapLogger := range helpers {
		t.Run(name, func(t *testing.T) {
			swapLogger(t)

			defer func() { require.Contains(t, fmt.Sprint(recover()), "t.Parallel") }()
			t.Parallel()
		})
	}
}
