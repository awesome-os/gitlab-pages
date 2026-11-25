package acceptance_test

import (
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
)

func runRedirectStatusTests(t *testing.T, listener ListenSpec, host, path string, extraArgs ...processOption) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	RunPagesProcess(t,
		append(extraArgs, withListeners([]ListenSpec{httpListener}))...,
	)

	rsp, err := GetPageFromListener(t, currentListener(listener), host, path)
	require.NoError(t, err)

	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	testhelpers.Close(t, rsp.Body)

	require.Contains(t, string(body), "15 rules")
	require.Equal(t, http.StatusOK, rsp.StatusCode)
}

func TestRedirectStatusPage(t *testing.T) {
	runRedirectStatusTests(t, httpListener, "group.redirects.gitlab-example.com", "/project-redirects/_redirects")
}

func TestNamespaceInPathEnabled_RedirectStatusPage(t *testing.T) {
	runRedirectStatusTests(t,
		httpListener,
		"gitlab-example.com",
		"/group.redirects/project-redirects/_redirects",
		withExtraArgument("namespace-in-path", "true"),
	)
}

type redirect struct {
	host             string
	path             string
	expectedStatus   int
	expectedLocation string
}

func testRedirect(t *testing.T, tests []redirect, extraArgs ...processOption) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	RunPagesProcess(t,
		append(extraArgs, withListeners([]ListenSpec{httpListener}))...,
	)

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s%s -> %s (%d)", tt.host, tt.path, tt.expectedLocation, tt.expectedStatus), func(t *testing.T) {
			rsp, err := GetRedirectPage(t, httpListener, tt.host, tt.path)
			require.NoError(t, err)
			testhelpers.Close(t, rsp.Body)

			require.Equal(t, tt.expectedLocation, rsp.Header.Get("Location"))
			require.Equal(t, tt.expectedStatus, rsp.StatusCode)
		})
	}
}

func TestRedirect(t *testing.T) {
	tests := []redirect{
		// Serving a file works
		{
			host:             "group.redirects.gitlab-example.com",
			path:             "/project-redirects/index.html",
			expectedStatus:   http.StatusOK,
			expectedLocation: "",
		},
		// Project domain
		{
			host:             "group.redirects.gitlab-example.com",
			path:             "/project-redirects/redirect-portal.html",
			expectedStatus:   http.StatusFound,
			expectedLocation: "http://group.redirects.gitlab-example.com/project-redirects/magic-land.html",
		},
		// Make sure invalid rule does not redirect
		{
			host:             "group.redirects.gitlab-example.com",
			path:             "/project-redirects/goto-no-schema-domain.html",
			expectedStatus:   http.StatusNotFound,
			expectedLocation: "",
		},
		// Actual file on disk should override any redirects that match
		{
			host:             "group.redirects.gitlab-example.com",
			path:             "/project-redirects/file-override.html",
			expectedStatus:   http.StatusOK,
			expectedLocation: "",
		},
		// Group-level domain
		{
			host:             "group.redirects.gitlab-example.com",
			path:             "/redirect-portal.html",
			expectedStatus:   http.StatusFound,
			expectedLocation: "http://group.redirects.gitlab-example.com/magic-land.html",
		},
		// Custom domain
		{
			host:             "redirects.custom-domain.com",
			path:             "/redirect-portal.html",
			expectedStatus:   http.StatusFound,
			expectedLocation: "http://redirects.custom-domain.com/magic-land.html",
		},
		// Permanent redirect for splat (*) with replacement (:splat)
		{
			host:             "group.redirects.gitlab-example.com",
			path:             "/project-redirects/jobs/assistant-to-the-regional-manager.html",
			expectedStatus:   http.StatusMovedPermanently,
			expectedLocation: "http://group.redirects.gitlab-example.com/project-redirects/careers/assistant-to-the-regional-manager.html",
		},
		// Domain redirect
		{
			host:             "group.redirects.gitlab-example.com",
			path:             "/project-redirects/goto-domain.html",
			expectedStatus:   http.StatusFound,
			expectedLocation: "https://GitLab.com/pages.html",
		},
	}
	testRedirect(t, tests)
}

func TestNamespaceInPathEnabled_Redirect(t *testing.T) {
	tests := []redirect{
		// Serving a file works
		{
			host:             "gitlab-example.com",
			path:             "/group.redirects/project-redirects/index.html",
			expectedStatus:   http.StatusOK,
			expectedLocation: "",
		},
		// Project domain
		{
			host:             "gitlab-example.com",
			path:             "/group.redirects/project-redirects/redirect-portal.html",
			expectedStatus:   http.StatusFound,
			expectedLocation: "http://gitlab-example.com/group.redirects/project-redirects/magic-land.html",
		},
		// Make sure invalid rule does not redirect
		{
			host:             "gitlab-example.com",
			path:             "/group.redirects/project-redirects/goto-no-schema-domain.html",
			expectedStatus:   http.StatusNotFound,
			expectedLocation: "",
		},
		// Actual file on disk should override any redirects that match
		{
			host:             "gitlab-example.com",
			path:             "/group.redirects/project-redirects/file-override.html",
			expectedStatus:   http.StatusOK,
			expectedLocation: "",
		},
		// Group-level domain
		{
			host:             "gitlab-example.com",
			path:             "/group.redirects/redirect-portal.html",
			expectedStatus:   http.StatusFound,
			expectedLocation: "http://gitlab-example.com/group.redirects/magic-land.html",
		},
		// Custom domain
		{
			host:             "redirects.custom-domain.com",
			path:             "/redirect-portal.html",
			expectedStatus:   http.StatusFound,
			expectedLocation: "http://redirects.custom-domain.com/magic-land.html",
		},
		// Permanent redirect for splat (*) with replacement (:splat)
		{
			host:             "gitlab-example.com",
			path:             "/group.redirects/project-redirects/jobs/assistant-to-the-regional-manager.html",
			expectedStatus:   http.StatusMovedPermanently,
			expectedLocation: "http://gitlab-example.com/group.redirects/project-redirects/careers/assistant-to-the-regional-manager.html",
		},
		// Domain redirect
		{
			host:             "gitlab-example.com",
			path:             "/group.redirects/project-redirects/goto-domain.html",
			expectedStatus:   http.StatusFound,
			expectedLocation: "https://GitLab.com/pages.html",
		},
	}
	testRedirect(t, tests, withExtraArgument("namespace-in-path", "true"))
}
