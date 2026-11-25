package acceptance_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func TestPrimaryDomain(t *testing.T) {
	RunPagesProcess(t)

	tests := []struct {
		name          string
		requestDomain string
		requestPath   string
		redirectURL   string
		httpStatus    int
	}{
		{
			name:          "when project has a primary domain",
			requestDomain: "redirect.gitlab-example.com",
			requestPath:   "/project1",
			redirectURL:   "https://default.gitlab-example.com",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when project has a primary domain with subpath",
			requestDomain: "redirect.gitlab-example.com",
			requestPath:   "/project1/subpath/index.html",
			redirectURL:   "https://default.gitlab-example.com/subpath/index.html",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when project is a part of subgroup redirect without prefix",
			requestDomain: "redirect.gitlab-example.com",
			requestPath:   "/subgroup1/subgroup2/project1/subpath/index.html",
			redirectURL:   "https://default.gitlab-example.com/subpath/index.html",
			httpStatus:    http.StatusPermanentRedirect,
		},
		{
			name:          "when primary domain url is malformed",
			requestDomain: "redirect.gitlab-example.com",
			requestPath:   "/with-malformed-default-domain-redirect-url/",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when already on the primary domain",
			requestDomain: "default.gitlab-example.com",
			requestPath:   "/",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when project does not have a primary domain",
			requestDomain: "custom.gitlab-example.com",
			requestPath:   "/no-redirect/",
			httpStatus:    http.StatusOK,
		},
		{
			name:          "when project does not exist",
			requestDomain: "unknown.gitlab-example.com",
			requestPath:   "/",
			httpStatus:    http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rsp, err := GetRedirectPage(t, httpsListener, test.requestDomain, test.requestPath)
			require.NoError(t, err)
			testhelpers.Close(t, rsp.Body)

			require.Equal(t, test.httpStatus, rsp.StatusCode)

			if test.redirectURL != "" {
				require.Equal(t, test.redirectURL, rsp.Header.Get("Location"))
			}
		})
	}
}
