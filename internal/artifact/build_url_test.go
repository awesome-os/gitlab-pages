package artifact_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/artifact"
	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
)

func FuzzBuildURLStaysWithinTopLevelGroup(f *testing.F) {
	const (
		pagesDomain = "gitlab.io"
		suffix      = "." + pagesDomain
		server      = "https://gitlab.com/api/v4"
		serverHost  = "gitlab.com"
		serverPath  = "/api/v4"
	)

	seeds := []struct {
		host string
		path string
	}{
		{"group.gitlab.io", "/-/project/-/jobs/1/artifacts/200.html"},
		{"group.gitlab.io", "/-/subgroup/project/-/jobs/1/artifacts/path/to/file.txt"},
		{"group.gitlab.io", "/-/../victim/project/-/jobs/1/artifacts/secrets.txt"},
		{"group.gitlab.io", "/-/subgroup/../../victim/project/-/jobs/1/artifacts/secrets.txt"},
		{"group.gitlab.io", "/-/project/-/jobs/1/artifacts/../../../../user"},
		{"group.gitlab.io", "/-/project/-/jobs/1/artifacts/..%2F..%2Fuser"},
		{".gitlab.io", "/-/victim/project/-/jobs/1/artifacts/secrets.txt"},
		{"...gitlab.io", "/-/victim/project/-/jobs/1/artifacts/secrets.txt"},
		{"group.gitlab.io", "/-/project/-/jobs/1/artifacts/file?private_token=x"},
		{"group.gitlab.io", "/index.html"},
	}

	for _, seed := range seeds {
		f.Add(seed.host, seed.path)
	}

	a := artifact.New(server, 1, pagesDomain, config.HTTPClientCfg{})

	f.Fuzz(func(t *testing.T, host, requestPath string) {
		u, ok := a.BuildURL(host, requestPath)
		if !ok {
			return
		}

		require.Equal(t, serverHost, u.Host)
		require.Equal(t, "https", u.Scheme)
		require.Empty(t, u.User)

		topGroup := host[:len(host)-len(suffix)]
		prefix := serverPath + "/projects/" + url.PathEscape(topGroup) + "%2F"
		require.True(t, strings.HasPrefix(u.RequestURI(), prefix),
			"request URI %q does not stay within the top-level group %q", u.RequestURI(), topGroup)

		for _, segment := range strings.Split(u.EscapedPath(), "/") {
			require.NotEqual(t, ".", segment)
			require.NotEqual(t, "..", segment)
		}
	})
}
