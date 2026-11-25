package acceptance_test

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/test/gitlabstub"
)

func TestSlowlogsRecorded(t *testing.T) {
	delay := 10 * time.Millisecond

	logBuf := RunPagesProcess(t,
		withStubOptions(gitlabstub.WithDelay(delay)),
		withListeners([]ListenSpec{httpListener}),
		withExtraArgument("slow-requests-threshold", "5ms"),
	)

	rsp, err := GetPageFromListener(t, httpListener, "group.gitlab-example.com", "/index.html")
	require.NoError(t, err)
	defer rsp.Body.Close()
	_, _ = io.ReadAll(rsp.Body)

	require.Equal(t, http.StatusOK, rsp.StatusCode)

	assertLogFound(t, logBuf, []string{"Request time exceeded",
		"total_execution_time_ms",
		"middleware_time_distribution",
		"other_time_distribution"})
}

func TestSlowlogsRecorderFastRequestNotRecorded(t *testing.T) {
	// No additional delay for GitLab stub
	logBuf := RunPagesProcess(t,
		withListeners([]ListenSpec{httpListener}),
		withExtraArgument("slow-requests-threshold", "10s"), // Very high threshold
	)

	rsp, err := GetPageFromListener(t, httpListener, "group.gitlab-example.com", "/index.html")
	require.NoError(t, err)
	defer rsp.Body.Close()
	_, _ = io.ReadAll(rsp.Body)

	require.Equal(t, http.StatusOK, rsp.StatusCode)

	// Wait a moment to ensure logs would be written if present
	time.Sleep(100 * time.Millisecond)

	// Verify slow logs were not generated
	require.NotContains(t, logBuf.String(), "Request time exceeded")
}
