package redirects

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	netlifyRedirects "github.com/tj/go-redirects"

	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
)

// Tests validation rules that only apply when the `FF_ENABLE_PLACEHOLDERS`
// feature flag is not enabled. These tests can be removed when the
// `FF_ENABLE_PLACEHOLDERS` flag is removed.
func TestRedirectsValidateUrlNoPlaceholders(t *testing.T) {
	// disable placeholders on purpose
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "false")

	tests := map[string]struct {
		url         string
		expectedErr error
	}{
		"no_splats": {
			url:         "/blog/*",
			expectedErr: errNoSplats,
		},
		"no_placeholders": {
			url:         "/news/:year/:month/:date/:slug",
			expectedErr: errNoPlaceholders,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateToURL(tt.url, http.StatusMovedPermanently)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestRedirectsValidateRule(t *testing.T) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	tests := map[string]struct {
		rule        string
		expectedErr error
	}{
		"valid_rule": {
			rule: "/goto.html /target.html 301",
		},
		"valid_from_host_url": {
			rule: "http://valid.com/ /teapot.html 302",
		},
		"valid_to_url": {
			rule: "/goto.html https://example.com/target.html 301",
		},
		"invalid_from_url": {
			rule:        "invalid.com /teapot.html 302",
			expectedErr: errNoValidStartingInURLPath,
		},
		"invalid_to_url": {
			rule:        "/goto.html invalid.com",
			expectedErr: errNoValidStartingInURLPath,
		},
		"no_parameters": {
			rule:        "/        /something      302     foo=bar",
			expectedErr: errNoParams,
		},
		"invalid_status": {
			rule:        "/goto.html /target.html 418",
			expectedErr: errUnsupportedStatus,
		},
		"force_not_supported": {
			rule:        "/goto.html /target.html 302!",
			expectedErr: errNoForce,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rules, err := netlifyRedirects.ParseString(tt.rule)
			require.NoError(t, err)

			err = validateRule(rules[0])
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestRedirectsValidateFromUrl(t *testing.T) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	tests := map[string]struct {
		url         string
		expectedErr error
	}{
		"valid_url": {
			url: "/goto.html",
		},
		"domain_level": {
			url: "https://GitLab.com",
		},
		"no_special_characters_escape_domain_level": {
			url:         "/\\GitLab.com",
			expectedErr: errNoValidStartingInURLPath,
		},
		"no_schemaless_url_domain_level": {
			url:         "//GitLab.com/pages.html",
			expectedErr: errNoValidStartingInURLPath,
		},
		"no_bare_domain_level": {
			url:         "GitLab.com",
			expectedErr: errNoValidStartingInURLPath,
		},
		"no_parent_traversing_relative_url": {
			url:         "../target.html",
			expectedErr: errNoValidStartingInURLPath,
		},
		"too_many_slashes": {
			url:         strings.Repeat("/a", 26),
			expectedErr: fmt.Errorf("url path cannot contain more than %d forward slashes", defaultMaxPathSegments),
		},
		"placeholders": {
			url: "/news/:year/:month/:date/:slug",
		},
		"domain_redirect_placeholders": {
			url: "https://GitLab.com/news/:year/:month/:date/:slug",
		},
		"splats": {
			url: "/blog/*",
		},
		"lone_splats": {
			url: "https://GitLab.com/*",
		},
		"domain_redirect_splats": {
			url: "https://GitLab.com/blog/*",
		},
		"splat_placeholders": {
			url: "/new/path/:splat",
		},
		"domain_redirect_splat_placeholders": {
			url: "https://GitLab.com/new/path/:splat",
		},
		"extension_splat": {
			url: "/*.html",
		},
		"extension_splat_nested": {
			url: "/blog/*.html",
		},
		"domain_extension_splat": {
			url: "https://GitLab.com/*.html",
		},
		"multiple_extension_splats": {
			url:         "/blog/*.html/archive/*.xml",
			expectedErr: errMoreThanOneSplats,
		},
		"extension_splat_and_full_splat": {
			url:         "/*.html/*",
			expectedErr: errMoreThanOneSplats,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateFromURL(tt.url)
			if tt.expectedErr != nil {
				require.EqualError(t, err, tt.expectedErr.Error())
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestRedirectsValidateToUrl(t *testing.T) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	tests := map[string]struct {
		url         string
		status      int
		expectedErr error
	}{
		"valid_url": {
			url:    "/goto.html",
			status: http.StatusOK,
		},
		"domain_level_redirects": {
			url:    "https://GitLab.com",
			status: http.StatusMovedPermanently,
		},
		"domain_level_rewrite": {
			url:         "https://GitLab.com",
			status:      http.StatusOK,
			expectedErr: errNoDomainLevelRewrite,
		},
		"no_special_characters_escape_domain_level_redirects": {
			url:         "/\\GitLab.com",
			status:      http.StatusMovedPermanently,
			expectedErr: errNoValidStartingInURLPath,
		},
		"no_schemaless_url_domain_level_redirects": {
			url:         "//GitLab.com/pages.html",
			status:      http.StatusMovedPermanently,
			expectedErr: errNoValidStartingInURLPath,
		},
		"no_bare_domain_level_redirects": {
			url:         "GitLab.com",
			status:      http.StatusMovedPermanently,
			expectedErr: errNoValidStartingInURLPath,
		},
		"no_parent_traversing_relative_url": {
			url:         "../target.html",
			status:      http.StatusMovedPermanently,
			expectedErr: errNoValidStartingInURLPath,
		},
		"too_many_slashes": {
			url:         strings.Repeat("/a", 26),
			status:      http.StatusMovedPermanently,
			expectedErr: fmt.Errorf("url path cannot contain more than %d forward slashes", defaultMaxPathSegments),
		},
		"placeholders": {
			url:    "/news/:year/:month/:date/:slug",
			status: http.StatusMovedPermanently,
		},
		"domain_redirect_placeholders": {
			url:    "https://GitLab.com/news/:year/:month/:date/:slug",
			status: http.StatusMovedPermanently,
		},
		"splats": {
			url:    "/blog/*",
			status: http.StatusMovedPermanently,
		},
		"lone_splats": {
			url:    "https://GitLab.com/*",
			status: http.StatusMovedPermanently,
		},
		"domain_redirect_splats": {
			url:    "https://GitLab.com/blog/*",
			status: http.StatusMovedPermanently,
		},
		"splat_placeholders": {
			url:    "/new/path/:splat",
			status: http.StatusMovedPermanently,
		},
		"domain_redirect_splat_placeholders": {
			url:    "https://GitLab.com/new/path/:splat",
			status: http.StatusMovedPermanently,
		},
		"extension_splat": {
			url:    "/*.html",
			status: http.StatusMovedPermanently,
		},
		"extension_splat_nested": {
			url:    "/blog/*.html",
			status: http.StatusMovedPermanently,
		},
		"domain_extension_splat": {
			url:    "https://GitLab.com/*.html",
			status: http.StatusMovedPermanently,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateToURL(tt.url, tt.status)
			if tt.expectedErr != nil {
				require.EqualError(t, err, tt.expectedErr.Error())
				return
			}

			require.NoError(t, err)
		})
	}
}
