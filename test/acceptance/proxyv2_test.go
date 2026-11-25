package acceptance_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func TestProxyv2(t *testing.T) {
	// the dummy client IP 10.1.1.1 is set by TestProxyv2Client
	tests := map[string]struct {
		host               string
		urlSuffix          string
		namespaceInPath    bool
		expectedStatusCode int
		expectedContent    string
		expectedLog        []string
	}{
		"basic_proxyv2_request": {
			host:               "group.gitlab-example.com",
			urlSuffix:          "project/",
			namespaceInPath:    false,
			expectedStatusCode: http.StatusOK,
			expectedContent:    "project-subdir\n",
			expectedLog:        []string{"\"host\":\"group.gitlab-example.com\"", "\"remote_ip\":\"10.1.1.1\""},
		},
		"namespace_in_path_enabled_basic_proxyv2_request": {
			host:               "gitlab-example.com",
			urlSuffix:          "group/project/",
			namespaceInPath:    true,
			expectedStatusCode: http.StatusOK,
			expectedContent:    "project-subdir\n",
			expectedLog:        []string{"\"host\":\"group.gitlab-example.com\"", "\"remote_ip\":\"10.1.1.1\""},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			extraArgs := []processOption{withListeners([]ListenSpec{httpsProxyv2Listener})}
			if tt.namespaceInPath {
				extraArgs = append(extraArgs, withExtraArgument("namespace-in-path", "true"))
			}

			logBuf := RunPagesProcess(t, extraArgs...)

			response, err := GetPageFromListener(t, httpsProxyv2Listener, tt.host, tt.urlSuffix)
			require.NoError(t, err)
			testhelpers.Close(t, response.Body)

			require.Equal(t, tt.expectedStatusCode, response.StatusCode)

			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)

			require.Contains(t, string(body), tt.expectedContent, "content mismatch")

			// give the process enough time to write the log message
			require.Eventually(t, func() bool {
				logs := logBuf.String()
				for _, e := range tt.expectedLog {
					if !strings.Contains(logs, e) {
						return false
					}
				}
				return true
			}, time.Second, 10*time.Millisecond, "timed out waiting for expected logs")
		})
	}
}
