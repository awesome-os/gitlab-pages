package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gitlab-org/labkit/v2/fields"
)

// withMapping installs a ServiceMapping with the given variant settings for
// the duration of the test, then restores the previous one.
func withMapping(t *testing.T, schema, dualEmit int) {
	t.Helper()
	prev := Mapping
	Mapping = fields.NewServiceMapping(
		fields.WithSchemaVersion(schema),
		fields.WithDualEmitTarget(dualEmit),
	)
	t.Cleanup(func() { Mapping = prev })
}

// captureAccessLog runs a request through BasicAccessLogger and returns the
// parsed access log line.
func captureAccessLog(t *testing.T, requestURL, referrer string) map[string]any {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := Logger
	Logger = slog.New(slog.NewJSONHandler(buf, nil))
	t.Cleanup(func() { Logger = prev })

	handler := BasicAccessLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "")

	req := httptest.NewRequest(http.MethodGet, requestURL, nil)
	req.Header.Set("Referer", referrer)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	return out
}

func TestAccessLog_DefaultEmitsDurationMs(t *testing.T) {
	withMapping(t, 0, 0)

	out := captureAccessLog(t, "http://example.com/foo", "")

	_, hasMs := out["duration_ms"]
	_, hasS := out[fields.DurationS]
	assert.True(t, hasMs, "duration_ms must be present at schema_version=0")
	assert.False(t, hasS, "duration_s must be absent at schema_version=0 without dual emit")
}

func TestAccessLog_SchemaV1EmitsDurationS(t *testing.T) {
	withMapping(t, 1, 0)

	out := captureAccessLog(t, "http://example.com/foo", "")

	_, hasMs := out["duration_ms"]
	_, hasS := out[fields.DurationS]
	assert.False(t, hasMs, "duration_ms must be absent at schema_version=1")
	assert.True(t, hasS, "duration_s must be present at schema_version=1")
}

func TestAccessLog_DualEmitEmitsBothDurations(t *testing.T) {
	withMapping(t, 0, 1)

	out := captureAccessLog(t, "http://example.com/foo", "")

	_, hasMs := out["duration_ms"]
	_, hasS := out[fields.DurationS]
	assert.True(t, hasMs, "duration_ms must be present with dual emit on")
	assert.True(t, hasS, "duration_s must be present with dual emit on")
}

func TestAccessLog_RemovesSensitiveURLComponents(t *testing.T) {
	withMapping(t, 0, 0)

	out := captureAccessLog(t,
		"/foo%2Fbar?code=code-secret&arbitrary=arbitrary-secret#request-fragment",
		"https://user:password@referrer.example/path?state=state-secret#referrer-fragment",
	)

	assert.Equal(t, "/foo%2Fbar", out[fields.HTTPURI])
	assert.Equal(t, "https://referrer.example/path", out[fields.HTTPReferrer])
}

func TestAccessLog_InvalidReferrerFailsClosed(t *testing.T) {
	withMapping(t, 0, 0)

	out := captureAccessLog(t, "/foo?code=code-secret", "://invalid-referrer-secret")

	assert.Equal(t, "/foo", out[fields.HTTPURI])
	assert.Empty(t, out[fields.HTTPReferrer])
}
