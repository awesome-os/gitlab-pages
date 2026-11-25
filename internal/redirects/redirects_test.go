package redirects

import (
	archZip "archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	netlifyRedirects "github.com/tj/go-redirects"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/confighelper"
	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs"
	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs/zip"
)

func TestRedirectsRewrite(t *testing.T) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	tests := []struct {
		name           string
		url            string
		rule           string
		expectedURL    string
		expectedStatus int
		expectedErr    error
	}{
		{
			name:           "No rules given",
			url:            "/no-redirect/",
			rule:           "",
			expectedURL:    "",
			expectedStatus: 0,
			expectedErr:    ErrNoRedirect,
		},
		{
			name:           "No matching rules",
			url:            "/no-redirect/",
			rule:           "/cake-portal.html  /still-alive.html 301",
			expectedURL:    "",
			expectedStatus: 0,
			expectedErr:    ErrNoRedirect,
		},
		{
			name:           "Matching rule redirects",
			url:            "/cake-portal.html",
			rule:           "/cake-portal.html  /still-alive.html 301",
			expectedURL:    "/still-alive.html",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "Does not redirect to invalid rule",
			url:            "/goto.html",
			rule:           "/goto.html GitLab.com 301",
			expectedURL:    "",
			expectedStatus: 0,
			expectedErr:    ErrNoRedirect,
		},
		{
			name:           "Matches trailing slash rule to no trailing slash URL",
			url:            "/cake-portal",
			rule:           "/cake-portal/  /still-alive/ 301",
			expectedURL:    "/still-alive/",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "Matches trailing slash rule to trailing slash URL",
			url:            "/cake-portal/",
			rule:           "/cake-portal/  /still-alive/ 301",
			expectedURL:    "/still-alive/",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "Matches no trailing slash rule to no trailing slash URL",
			url:            "/cake-portal",
			rule:           "/cake-portal  /still-alive 301",
			expectedURL:    "/still-alive",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "Matches no trailing slash rule to trailing slash URL",
			url:            "/cake-portal/",
			rule:           "/cake-portal  /still-alive 301",
			expectedURL:    "/still-alive",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "matches_splat_rule",
			url:            "/the-cake/is-delicious",
			rule:           "/the-cake/* /is-a-lie 200",
			expectedURL:    "/is-a-lie",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "replaces_splat_placeholdes",
			url:            "/from/weighted/companion/cube/path",
			rule:           "/from/*/path /to/:splat/path 200",
			expectedURL:    "/to/weighted/companion/cube/path",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "splat_rule_keeps_literal_dollar_in_target",
			url:            "/docs/intro",
			rule:           "/docs/* /archive/$1/:splat 301",
			expectedURL:    "/archive/$1/intro",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "matches_placeholder_rule",
			url:            "/the/cake/is/delicious",
			rule:           "/the/:placeholder/is/delicious /the/:placeholder/is/a/lie 200",
			expectedURL:    "/the/cake/is/a/lie",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "does_not_redirect_acme_challenges",
			url:            "/.well-known/acme-challenge/token",
			rule:           "/* /to/path 200",
			expectedURL:    "",
			expectedStatus: 0,
			expectedErr:    ErrNoRedirect,
		},
		{
			name:           "splat_rule_redirects_ending_with_number",
			url:            "/15.1",
			rule:           "/15.1/* /15/:splat 301",
			expectedURL:    "/15/",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "splat_rule_redirects_ending_with_number_2",
			url:            "/doc/4.0/",
			rule:           "/doc/4/* /doc/4.0.4/:splat 200\n /doc/4.0/* /doc/4.0.4/:splat 200",
			expectedURL:    "/doc/4.0.4/",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "splat_target_with_leading_slashes_stays_on_site",
			url:            "/blog//other.example/x",
			rule:           "/blog/* /:splat 301",
			expectedURL:    "/other.example/x",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "splat_target_with_three_leading_slashes_stays_on_site",
			url:            "/blog///other.example/x",
			rule:           "/blog/* /:splat 301",
			expectedURL:    "/other.example/x",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "splat_target_with_suffix_stays_on_site",
			url:            "/blog//other.example",
			rule:           "/blog/* /:splat/index.html 301",
			expectedURL:    "/other.example/index.html",
			expectedStatus: http.StatusMovedPermanently,
		},
		{
			name:           "splat_rewrite_with_leading_slashes_stays_on_site",
			url:            "/blog//other.example/x",
			rule:           "/blog/* /:splat 200",
			expectedURL:    "/other.example/x",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Redirects{}

			if tt.rule != "" {
				rules, err := netlifyRedirects.ParseString(tt.rule)
				require.NoError(t, err)
				r.rules = rules
			}

			requestURL, err := url.Parse(tt.url)
			require.NoError(t, err)

			toURL, status, err := r.Rewrite(context.Background(), requestURL, "correlationId")

			if tt.expectedURL != "" {
				require.Equal(t, tt.expectedURL, toURL.String())
			} else {
				require.Nil(t, toURL)
			}

			require.Equal(t, tt.expectedStatus, status)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestRedirectsRewriteLogsURLsWithoutQuery(t *testing.T) {
	rules, err := netlifyRedirects.ParseString("/cake-portal.html /still-alive.html 301")
	require.NoError(t, err)
	r := Redirects{rules: rules}
	requestURL, err := url.Parse("/cake-portal.html?token=request-secret#fragment-secret")
	require.NoError(t, err)

	buf := &bytes.Buffer{}
	level := slog.LevelDebug
	logger := logv2.NewWithConfig(&logv2.Config{Writer: buf, LogLevel: &level})
	ctx := logv2.WithLogger(context.Background(), logger)

	_, _, err = r.Rewrite(ctx, requestURL, "correlationId")

	require.NoError(t, err)
	require.NotEmpty(t, buf.String())
	require.NotContains(t, buf.String(), "request-secret")
	require.NotContains(t, buf.String(), "fragment-secret")
}

func TestRedirectsRewriteHandlesInvalidInterpolatedURL(t *testing.T) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	rules, err := netlifyRedirects.ParseString("/* /target/:splat 301")
	require.NoError(t, err)
	r := Redirects{rules: rules}
	requestURL, err := url.Parse("/50%25-off%3Ftoken=error-secret")
	require.NoError(t, err)

	buf := &bytes.Buffer{}
	level := slog.LevelDebug
	logger := logv2.NewWithConfig(&logv2.Config{Writer: buf, LogLevel: &level})
	ctx := logv2.WithLogger(context.Background(), logger)

	newURL, status, err := r.Rewrite(ctx, requestURL, "correlationId")

	require.Nil(t, newURL)
	require.Equal(t, http.StatusMovedPermanently, status)
	require.Error(t, err)

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.Empty(t, record["newURL"])
	errorMessage, ok := record["error_message"].(string)
	require.True(t, ok)
	require.NotContains(t, errorMessage, "error-secret")
}

func TestRedirectsRewriteRefusesURLThatLeavesSite(t *testing.T) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	// Collapse slashes as the matcher did before it collapsed leading ones,
	// so that templating gives a URL with a host and only Rewrite can stop it.
	collapse := regexMultipleSlashes
	regexMultipleSlashes = regexp.MustCompile(`([^:])//+`)
	t.Cleanup(func() { regexMultipleSlashes = collapse })

	rules, err := netlifyRedirects.ParseString("/blog/* /:splat 301")
	require.NoError(t, err)
	r := Redirects{rules: rules}
	requestURL, err := url.Parse("/blog//other.example/x")
	require.NoError(t, err)

	newURL, status, err := r.Rewrite(context.Background(), requestURL, "correlationId")

	require.Nil(t, newURL)
	require.Equal(t, http.StatusMovedPermanently, status)
	require.ErrorIs(t, err, errRewriteLeavesSite)
}

func Test_leavesSite(t *testing.T) {
	tests := map[string]struct {
		to        string
		rewritten string
		expected  bool
	}{
		"path_rewritten_to_path": {
			to:        "/:splat",
			rewritten: "/other.example/x",
			expected:  false,
		},
		"path_rewritten_to_host": {
			to:        "/:splat",
			rewritten: "//other.example/x",
			expected:  true,
		},
		"path_rewritten_to_scheme": {
			to:        "/:splat",
			rewritten: "mailto:user@other.example",
			expected:  true,
		},
		"url_rewritten_to_url": {
			to:        "https://example.com/:splat",
			rewritten: "https://example.com/x",
			expected:  false,
		},
		"unparsable_target": {
			to:        "/%zz/:splat",
			rewritten: "/x",
			expected:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rewritten, err := url.Parse(tt.rewritten)
			require.NoError(t, err)

			require.Equal(t, tt.expected, leavesSite(tt.to, rewritten))
		})
	}
}

func TestRedirectsParseRedirects(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		redirectsFile string
		expectedRules int
		expectedErr   error
	}{
		{
			name:          "No `_redirects` file present",
			redirectsFile: "",
			expectedRules: 0,
			expectedErr:   errConfigNotFound,
		},
		{
			name:          "Everything working as expected",
			redirectsFile: `/goto.html /target.html 301`,
			expectedRules: 1,
		},
		{
			name:          "Invalid _redirects syntax gives no rules",
			redirectsFile: `foobar::baz`,
			expectedRules: 0,
		},
		{
			name:          "Config file too big",
			redirectsFile: strings.Repeat("a", 2*cfg.MaxConfigSize),
			expectedRules: 0,
			expectedErr:   errFileTooLarge,
		},
		// In future versions of `github.com/tj/go-redirects`,
		// this may not throw a parsing error and this test could be removed
		{
			name:          "Parsing error is caught",
			redirectsFile: "/store id=:id  /blog/:id  301",
			expectedRules: 0,
			expectedErr:   errFailedToParseConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a zip file with _redirects file populated based on redirectsCount
			zipContent, err := generateZipContent(tt.redirectsFile)
			require.NoError(t, err)

			// Serve the zip file with a mock HTTP server
			server := testhelpers.ServeZipFile(zipContent, "/public.zip")
			defer server.Close()

			defaultZipVFS := zip.New(&confighelper.DefaultZipCfgForTest)
			defer defaultZipVFS.Stop()

			// Create a VFS root from the served zip file
			vfsRoot, err := createVFSRoot(server.URL+"/public.zip", defaultZipVFS)
			require.NoError(t, err)

			redirects := ParseRedirects(ctx, vfsRoot)

			require.ErrorIs(t, redirects.error, tt.expectedErr)
			require.Len(t, redirects.rules, tt.expectedRules)
		})
	}
}

func TestMaxRuleCount(t *testing.T) {
	zipContent, err := generateZipContent(strings.Repeat("/goto.html /target.html 301\n", cfg.MaxRuleCount-1) +
		"/1000.html /target1000 301\n" +
		"/1001.html /target1001 301\n")
	require.NoError(t, err)

	// Serve the zip file with a mock HTTP server
	server := testhelpers.ServeZipFile(zipContent, "/public.zip")
	defer server.Close()

	defaultZipVFS := zip.New(&confighelper.DefaultZipCfgForTest)
	defer defaultZipVFS.Stop()

	// Create a VFS root from the served zip file
	vfsRoot, err := createVFSRoot(server.URL+"/public.zip", defaultZipVFS)
	require.NoError(t, err)

	redirects := ParseRedirects(context.Background(), vfsRoot)

	testFn := func(path, expectedToURL string, expectedStatus int, expectedErr error) func(t *testing.T) {
		return func(t *testing.T) {
			originalURL, err := url.Parse(path)
			require.NoError(t, err)

			toURL, status, err := redirects.Rewrite(context.Background(), originalURL, "correlationId")
			if expectedErr != nil {
				require.ErrorIs(t, err, expectedErr)
				return
			}

			require.NoError(t, err)

			require.Equal(t, expectedToURL, toURL.String())
			require.Equal(t, expectedStatus, status)
		}
	}

	t.Run("maxRuleCount matches", testFn("/1000.html", "/target1000", http.StatusMovedPermanently, nil))
	t.Run("maxRuleCount+1 does not match", testFn("/1001.html", "", 0, ErrNoRedirect))
}

// generateZipContent generates the content of a zip file with _redirects file populated based on redirectsCount
func generateZipContent(redirectContent string) ([]byte, error) {
	buff := new(bytes.Buffer)
	zw := archZip.NewWriter(buff)
	if len(redirectContent) > 0 {
		w, err := zw.Create(ConfigFile)
		if err != nil {
			return nil, err
		}
		_, err = io.WriteString(w, redirectContent)
		if err != nil {
			return nil, err
		}
	}
	err := zw.Close()
	if err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

// createVFSRoot creates a VFS root from the served zip file
func createVFSRoot(url string, vfs vfs.VFS) (vfs.Root, error) {
	pathSha := testhelpers.Sha(url)
	return vfs.Root(context.Background(), url, pathSha)
}

// parseRedirectsFromVFS parses redirects from the VFS root
func parseRedirectsFromVFS(ctx context.Context, root vfs.Root) (*Redirects, error) {
	redirects := ParseRedirects(ctx, root)
	if redirects.error != nil {
		return nil, redirects.error
	}
	return redirects, nil
}
