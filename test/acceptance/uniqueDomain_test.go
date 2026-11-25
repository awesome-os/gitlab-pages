package acceptance_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func TestRedirectToUniqueDomain(t *testing.T) {
	RunPagesProcess(t)

	tests := []struct {
		name          string
		requestDomain string
		requestPath   string
		redirectURL   string
		httpStatus    int
	}{
		{
			name:          "when project has unique domain",
			requestDomain: "group.unique-url.gitlab-example.com",
			requestPath:   "with-unique-url/",
			redirectURL:   "https://unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com/",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when requesting implicit index.html",
			requestDomain: "group.unique-url.gitlab-example.com",
			requestPath:   "with-unique-url",
			redirectURL:   "https://unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when project is nested",
			requestDomain: "group.unique-url.gitlab-example.com",
			requestPath:   "subgroup1/subgroup2/with-unique-url/subdir/index.html",
			redirectURL:   "https://unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com/subdir/index.html",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when serving with a port",
			requestDomain: "group.unique-url.gitlab-example.com:8080",
			requestPath:   "with-unique-url/",
			redirectURL:   "https://unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com:8080/",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when serving a path with a port",
			requestDomain: "group.unique-url.gitlab-example.com:8080",
			requestPath:   "with-unique-url/subdir/index.html",
			redirectURL:   "https://unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com:8080/subdir/index.html",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when already serving the unique domain",
			requestDomain: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when already serving the unique domain with a port",
			requestDomain: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com:8080",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when already serving the unique domain with path",
			requestDomain: "unique-url-group-unique-url-a1b2c3d4e5f6.gitlab-example.com",
			requestPath:   "subdir/index.html",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when project does not have unique domain with a path",
			requestDomain: "group.unique-url.gitlab-example.com",
			requestPath:   "without-unique-url/subdir/index.html",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when project does not exist",
			requestDomain: "group.unique-url.gitlab-example.com",
			requestPath:   "inexisting-project/",
			httpStatus:    http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rsp, err := GetRedirectPage(t, httpsListener, test.requestDomain, test.requestPath)
			require.NoError(t, err)
			testhelpers.Close(t, rsp.Body)

			require.Equal(t, test.httpStatus, rsp.StatusCode)
			require.Equal(t, test.redirectURL, rsp.Header.Get("Location"))
		})
	}
}

func TestNamespaceInPathEnabled_RedirectToUniqueDomain(t *testing.T) {
	RunPagesProcess(t, withExtraArgument("namespace-in-path", "true"))

	tests := []struct {
		name          string
		requestDomain string
		requestPath   string
		redirectURL   string
		httpStatus    int
	}{
		{
			name:          "when project has unique domain",
			requestDomain: "gitlab-example.com",
			requestPath:   "group.unique-url/with-unique-url/",
			redirectURL:   "https://gitlab-example.com/unique-url-group-unique-url-a1b2c3d4e5f6/",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when requesting implicit index.html",
			requestDomain: "gitlab-example.com",
			requestPath:   "group.unique-url/with-unique-url",
			redirectURL:   "https://gitlab-example.com/unique-url-group-unique-url-a1b2c3d4e5f6/",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when project is nested",
			requestDomain: "gitlab-example.com",
			requestPath:   "group.unique-url/subgroup1/subgroup2/with-unique-url/subdir/index.html",
			redirectURL:   "https://gitlab-example.com/unique-url-group-unique-url-a1b2c3d4e5f6/subdir/index.html",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when serving with a port",
			requestDomain: "gitlab-example.com:8080",
			requestPath:   "group.unique-url/with-unique-url/",
			redirectURL:   "https://gitlab-example.com:8080/unique-url-group-unique-url-a1b2c3d4e5f6/",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when serving a path with a port",
			requestDomain: "gitlab-example.com:8080",
			requestPath:   "group.unique-url/with-unique-url/subdir/index.html",
			redirectURL:   "https://gitlab-example.com:8080/unique-url-group-unique-url-a1b2c3d4e5f6/subdir/index.html",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when already serving the unique domain",
			requestDomain: "gitlab-example.com",
			requestPath:   "unique-url-group-unique-url-a1b2c3d4e5f6/",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when already serving the unique domain with a port",
			requestDomain: "gitlab-example.com:8080",
			requestPath:   "unique-url-group-unique-url-a1b2c3d4e5f6/",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when already serving the unique domain without trailing slash in path",
			requestDomain: "gitlab-example.com",
			requestPath:   "unique-url-group-unique-url-a1b2c3d4e5f6",
			redirectURL:   "//gitlab-example.com/unique-url-group-unique-url-a1b2c3d4e5f6/",
			httpStatus:    http.StatusFound,
		},
		{
			name:          "when already serving the unique domain with a port without trailing slash in path",
			requestDomain: "gitlab-example.com:8080",
			requestPath:   "unique-url-group-unique-url-a1b2c3d4e5f6",
			redirectURL:   "//gitlab-example.com:8080/unique-url-group-unique-url-a1b2c3d4e5f6/",
			httpStatus:    http.StatusFound,
		},
		{
			name:          "when already serving the unique domain with path",
			requestDomain: "gitlab-example.com",
			requestPath:   "unique-url-group-unique-url-a1b2c3d4e5f6/subdir/index.html",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when project does not have unique domain with a path",
			requestDomain: "gitlab-example.com",
			requestPath:   "group.unique-url/without-unique-url/subdir/index.html",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when project does not exist",
			requestDomain: "gitlab-example.com",
			requestPath:   "group.unique-url/inexisting-project/",
			httpStatus:    http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rsp, err := GetRedirectPage(t, httpsListener, test.requestDomain, test.requestPath)
			require.NoError(t, err)
			testhelpers.Close(t, rsp.Body)

			require.Equal(t, test.httpStatus, rsp.StatusCode)
			require.Equal(t, test.redirectURL, rsp.Header.Get("Location"))
		})
	}
}
