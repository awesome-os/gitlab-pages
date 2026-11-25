// Package loggingtest swaps logging.Logger for the length of a test. Its
// loggers add the request fields of a logging.LogRequest context, as the
// application logger does.
//
// The logger is one global, so a test that swaps it cannot call t.Parallel.
// Its subtests can, and then share the swapped logger with each other. Call a
// helper once, from the test or benchmark goroutine, not from b.RunParallel.
package loggingtest

import (
	"bytes"
	"log/slog"
	"testing"

	logv2 "gitlab.com/gitlab-org/labkit/v2/log"
	"gitlab.com/gitlab-org/labkit/v2/testing/logtest"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

// Capture makes logging.Logger write lines at level and above to the returned
// buffer, as JSON, until the test ends.
func Capture(t testing.TB, level slog.Level) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	swap(t, logging.NewLogger(&logv2.Config{Writer: buf, LogLevel: &level}))

	return buf
}

// Record makes logging.Logger keep every record in the returned recorder until
// the test ends.
func Record(t testing.TB) *logtest.LogRecorder {
	t.Helper()

	logger, recorder := logtest.NewWithRecorder(nil)
	swap(t, logging.WithRequestAttrs(logger))

	return recorder
}

func swap(t testing.TB, logger *slog.Logger) {
	t.Helper()

	// logging.Logger is one global, so parallel tests would share it. Setenv
	// makes the testing package refuse t.Parallel in a test that swaps it.
	t.Setenv("GITLAB_PAGES_LOGGINGTEST", "1")

	previous := logging.Logger
	logging.Logger = logger
	t.Cleanup(func() { logging.Logger = previous })
}
