package acceptance_test

import (
	_ "embed"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitLabAPIMutualTLS(t *testing.T) {
	tests := []struct {
		name           string
		host           string
		path           string
		clientCertPath string
		clientKeyPath  string
		caCertPath     string
		status         int
		expectError    bool
	}{
		{
			name:           "basic request works with GitLab API mutual TLS",
			host:           "group.gitlab-example.com",
			path:           "/index.html",
			clientCertPath: "../../test/testdata/mutualtls/valid/client.crt",
			clientKeyPath:  "../../test/testdata/mutualtls/valid/client.key",
			caCertPath:     "../../test/testdata/mutualtls/valid/ca.crt",
			status:         http.StatusOK,
			expectError:    false,
		},
		{
			name:           "502 when invalid mutual TLS is used",
			host:           "group.gitlab-example.com",
			path:           "/index.html",
			clientCertPath: "../../test/testdata/mutualtls/invalid/client.crt",
			clientKeyPath:  "../../test/testdata/mutualtls/invalid/client.key",
			caCertPath:     "../../test/testdata/mutualtls/invalid/ca.crt",
			status:         http.StatusBadGateway,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			RunPagesProcessWithMutualTLS(t, httpsListener, "", tt.clientCertPath, tt.clientKeyPath, tt.caCertPath)
			rsp, err := GetPageFromListener(t, httpsListener, tt.host, tt.path)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				rsp.Body.Close()
			}
			require.Equal(t, tt.status, rsp.StatusCode)
		})
	}
}
