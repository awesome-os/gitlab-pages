package redirects

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	netlifyRedirects "github.com/tj/go-redirects"

	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
)

type testCaseData struct {
	rule         string
	path         string
	expectMatch  bool
	expectedPath string
}

type matchingTestSuite = map[string]testCaseData

func mergeTestSuites(suites ...matchingTestSuite) matchingTestSuite {
	var merged = make(matchingTestSuite)
	for _, suite := range suites {
		for name, test := range suite {
			merged[name] = test
		}
	}

	return merged
}

var testsWithoutPlaceholders = map[string]testCaseData{
	"exact_match": {
		rule:         "/foo/ /bar/",
		path:         "/foo/",
		expectMatch:  true,
		expectedPath: "/bar/",
	},
	"single_trailing_slash": {
		rule:         "/foo/ /bar/",
		path:         "/foo",
		expectMatch:  true,
		expectedPath: "/bar/",
	},
	"ignore_missing_slash": {
		rule:         "/foo /bar/",
		path:         "/foo/",
		expectMatch:  true,
		expectedPath: "/bar/",
	},
	"no_match": {
		rule:         "/foo /bar/",
		path:         "/foo/bar",
		expectMatch:  false,
		expectedPath: "",
	},
}

func TestMatchesRule(t *testing.T) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	tests := mergeTestSuites(testsWithoutPlaceholders, map[string]testCaseData{
		// Note: the following 3 cases behave differently when
		// placeholders are disabled. See the similar test cases below.
		"multiple_trailing_slashes": {
			rule:         "/foo/ /bar/",
			path:         "/foo//",
			expectMatch:  true,
			expectedPath: "/bar/",
		},
		// Since we are treating path as an entire path, so // is considered domain name
		"multiple_leading_slashes": {
			rule:         "/foo/ /bar/",
			path:         "//foo",
			expectMatch:  false,
			expectedPath: "",
		},
		"multiple_slashes_in_middle": {
			rule:         "/foo/bar /baz/",
			path:         "/foo//bar",
			expectMatch:  true,
			expectedPath: "/baz/",
		},

		"schema_host_from_url": {
			rule:         "http://pages.example.io/foo /baz/",
			path:         "http://pages.example.io/foo",
			expectMatch:  true,
			expectedPath: "/baz/",
		},

		"splat_match": {
			rule:         "/foo/*/bar /foo/:splat/qux",
			path:         "/foo/baz/bar",
			expectMatch:  true,
			expectedPath: "/foo/baz/qux",
		},
		"splat_match_multiple_segments": {
			rule:         "/foo/*/bar /foo/:splat/qux",
			path:         "/foo/hello/world/bar",
			expectMatch:  true,
			expectedPath: "/foo/hello/world/qux",
		},
		"splat_match_ignore_trailing_slash": {
			rule:         "/foo/*/bar /foo/:splat/qux",
			path:         "/foo/baz/bar/",
			expectMatch:  true,
			expectedPath: "/foo/baz/qux",
		},
		"splat_match_end": {
			rule:         "/foo/* /qux/:splat",
			path:         "/foo/baz/bar",
			expectMatch:  true,
			expectedPath: "/qux/baz/bar",
		},
		"splat_match_end_with_slash": {
			rule:         "/foo/* /qux/:splat",
			path:         "/foo/baz/bar/",
			expectMatch:  true,
			expectedPath: "/qux/baz/bar/",
		},
		"splat_match_beginning": {
			rule:         "/*/baz/bar /qux/:splat",
			path:         "/foo/baz/bar",
			expectMatch:  true,
			expectedPath: "/qux/foo",
		},
		"splat_match_empty_suffix": {
			rule:         "/foo/* /qux/:splat",
			path:         "/foo/",
			expectMatch:  true,
			expectedPath: "/qux/",
		},
		"splat_consumes_trailing_slash": {
			rule:         "/foo/* /qux/:splat",
			path:         "/foo",
			expectMatch:  true,
			expectedPath: "/qux/",
		},
		"splat_at_start_of_target_collapses_leading_slashes": {
			rule:         "/foo/* /:splat",
			path:         "/foo//bar/baz",
			expectMatch:  true,
			expectedPath: "/bar/baz",
		},
		"splat_match_empty_prefix": {
			rule:         "/*/foo /qux/:splat",
			path:         "/foo",
			expectMatch:  true,
			expectedPath: "/qux/",
		},
		"splat_mid_segment": {
			rule:         "/foo*bar /qux/:splat",
			path:         "/foobazbar",
			expectMatch:  false,
			expectedPath: "",
		},
		"splat_mid_segment_no_content": {
			rule:         "/foo*bar /qux/:splat",
			path:         "/foobar",
			expectMatch:  false,
			expectedPath: "",
		},
		"splat_ext_html": {
			rule:         "/*.html /:splat",
			path:         "/foo.html",
			expectMatch:  true,
			expectedPath: "/foo",
		},
		"splat_ext_html_nested": {
			rule:         "/blog/*.html /blog/:splat",
			path:         "/blog/my-post.html",
			expectMatch:  true,
			expectedPath: "/blog/my-post",
		},
		"splat_ext_html_no_match_different_ext": {
			rule:         "/*.html /:splat",
			path:         "/foo.txt",
			expectMatch:  false,
			expectedPath: "",
		},
		"splat_ext_html_no_match_no_ext": {
			rule:         "/*.html /:splat",
			path:         "/foo",
			expectMatch:  false,
			expectedPath: "",
		},
		"splat_ext_html_cross_directory": {
			rule:         "/*.html /:splat",
			path:         "/sub/foo.html",
			expectMatch:  true,
			expectedPath: "/sub/foo",
		},
		"splat_ext_html_deeply_nested": {
			rule:         "/*.html /:splat",
			path:         "/a/b/c/foo.html",
			expectMatch:  true,
			expectedPath: "/a/b/c/foo",
		},
		"splat_ext_html_empty_name": {
			rule:         "/*.html /:splat",
			path:         "/.html",
			expectMatch:  true,
			expectedPath: "/",
		},
		"lone_splat": {
			rule:         "/* /qux/:splat",
			path:         "/foo/bar",
			expectMatch:  true,
			expectedPath: "/qux/foo/bar",
		},
		"duplicate_splat_replacements": {
			rule:         "/foo/* /qux/:splat/:splat",
			path:         "/foo/hello",
			expectMatch:  true,
			expectedPath: "/qux/hello/hello",
		},
		"splat_missing_path_segment_behavior": {
			rule:         "/foo/*/bar /foo/:splat/qux",
			path:         "/foo/bar",
			expectMatch:  true,
			expectedPath: "/foo/qux",
		},
		"missing_splat_placeholder": {
			rule:         "/foo/ /qux/:splat",
			path:         "/foo/",
			expectMatch:  true,
			expectedPath: "/qux/",
		},
		"splat_rule_ending_with_number": {
			rule:         "/15.1/* /15/:splat",
			path:         "/15.1",
			expectMatch:  true,
			expectedPath: "/15/",
		},
		"placeholder_match": {
			rule:         "/foo/:year/:month/:day/bar /qux/:year-:month-:day",
			path:         "/foo/2021/08/16/bar",
			expectMatch:  true,
			expectedPath: "/qux/2021-08-16",
		},
		"placeholder_match_end": {
			rule:         "/foo/:placeholder /qux/:placeholder",
			path:         "/foo/bar",
			expectMatch:  true,
			expectedPath: "/qux/bar",
		},
		"placeholder_match_beginning": {
			rule:         "/:placeholder/foo /qux/:placeholder",
			path:         "/baz/foo",
			expectMatch:  true,
			expectedPath: "/qux/baz",
		},
		"placeholder_no_multiple_segments": {
			rule:         "/foo/:placeholder/bar /foo/:placeholder/qux",
			path:         "/foo/hello/world/bar",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_at_beginning_no_content": {
			rule:         "/:placeholder/foo /qux/:placeholder",
			path:         "/foo",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_at_end_no_content": {
			rule:         "/foo/:placeholder /qux/:placeholder",
			path:         "/foo/",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_mid_segment_in_from": {
			rule:         "/foo:placeholder /qux/:placeholder",
			path:         "/foorbar",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_mid_segment_in_to": {
			rule:         "/foo/:placeholder /qux/bar:placeholder",
			path:         "/foo/baz",
			expectMatch:  true,
			expectedPath: "/qux/barbaz",
		},
		"placeholder_missing_replacement_with_substring": {
			rule:         "/:foo /:foobar",
			path:         "/baz",
			expectMatch:  true,
			expectedPath: "/",
		},
		"placeholder_mid_segment_no_content": {
			rule:         "/foo:placeholder /qux/:splat",
			path:         "/foo",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_name_substring": {
			rule:         "/foo/:foo/:foobar /qux/:foo/:foobar",
			path:         "/foo/baz/quux",
			expectMatch:  true,
			expectedPath: "/qux/baz/quux",
		},
		"lone_placeholder": {
			rule:         "/:placeholder /qux/:placeholder",
			path:         "/foo",
			expectMatch:  true,
			expectedPath: "/qux/foo",
		},
		"duplicate_placeholders": {
			rule:         "/foo/:placeholder/bar/:placeholder/baz /qux/:placeholder",
			path:         "/foo/hello/bar/world/baz",
			expectMatch:  true,
			expectedPath: "/qux/hello",
		},
		"duplicate_placeholder_replacements": {
			rule:         "/foo/:placeholder /qux/:placeholder/:placeholder",
			path:         "/foo/hello",
			expectMatch:  true,
			expectedPath: "/qux/hello/hello",
		},
		"splat_and_placeholder_named_splat": {
			rule:         "/foo/*/bar/:splat /qux/:splat",
			path:         "/foo/hello/bar/world",
			expectMatch:  true,
			expectedPath: "/qux/hello",
		},
		"placeholder_named_splat_and_splat": {
			rule:         "/foo/:splat/bar/* /qux/:splat",
			path:         "/foo/hello/bar/world",
			expectMatch:  true,
			expectedPath: "/qux/hello",
		},

		// A literal "$" in the "to" path must be kept as is, and not be
		// read as a reference to a regular expression group.
		"literal_dollar_numbers_with_splat": {
			rule:         "/foo/* /qux/$1/$2/:splat",
			path:         "/foo/bar",
			expectMatch:  true,
			expectedPath: "/qux/$1/$2/bar",
		},
		"literal_dollar_braces_with_placeholder": {
			rule:         "/foo/:id /qux/${id}/:id",
			path:         "/foo/7",
			expectMatch:  true,
			expectedPath: "/qux/${id}/7",
		},
		"literal_double_dollar_with_placeholder": {
			rule:         "/foo/:id /qux/$$/:id",
			path:         "/foo/7",
			expectMatch:  true,
			expectedPath: "/qux/$$/7",
		},
		"literal_dollar_before_placeholder": {
			rule:         "/foo/:id /qux/$:id",
			path:         "/foo/7",
			expectMatch:  true,
			expectedPath: "/qux/$7",
		},
		"literal_dollar_multiple_trailing_slashes": {
			rule:         "/foo/ /qux/$1/",
			path:         "/foo//",
			expectMatch:  true,
			expectedPath: "/qux/$1/",
		},

		// Note: These differ slightly from Netlify's matching behavior.
		// GitLab replaces _all_ placeholders in the "to" path, even if
		// the placeholder doesn't have corresponding match in the "from".
		// Netlify only replaces placeholders that appear in the "from".
		"missing_placeholder_exact_match": {
			rule:        "/foo/ /qux/:placeholder",
			path:        "/foo/",
			expectMatch: true,

			// Netlify would instead redirect to "/qux/:placeholder"
			expectedPath: "/qux/",
		},
		"missing_placeholder_nonexact_match": {
			rule:        "/foo/:placeholderA /qux/:placeholderB",
			path:        "/foo/bar",
			expectMatch: true,

			// Netlify would instead redirect to "/qux/:placeholderB"
			expectedPath: "/qux/",
		},
	})

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rules, err := netlifyRedirects.ParseString(tt.rule)
			require.NoError(t, err)

			parsedURL, err := url.Parse(tt.path)
			require.NoError(t, err)

			isMatch, path := matchesRule(context.Background(), &rules[0], parsedURL, "correlation_id")
			require.Equal(t, tt.expectMatch, isMatch)
			require.Equal(t, tt.expectedPath, path)
		})
	}
}

// Tests matching behavior when the `FF_ENABLE_PLACEHOLDERS`
// feature flag is not enabled. These tests can be removed when the
// `FF_ENABLE_PLACEHOLDERS` flag is removed.
func Test_matchesRule_NoPlaceholders(t *testing.T) {
	// disable placeholders on purpose
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "false")

	tests := mergeTestSuites(testsWithoutPlaceholders, map[string]testCaseData{
		// Note: the following 3 case behaves differently when
		// placeholders are enabled. See the similar test cases above.
		"multiple_trailing_slashes": {
			rule:         "/foo/ /bar/",
			path:         "/foo//",
			expectMatch:  false,
			expectedPath: "",
		},
		"multiple_leading_slashes": {
			rule:         "/foo/ /bar/",
			path:         "//foo",
			expectMatch:  false,
			expectedPath: "",
		},
		"multiple_slashes_in_middle": {
			rule:         "/foo/bar /baz/",
			path:         "/foo//bar",
			expectMatch:  false,
			expectedPath: "",
		},
	})

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rules, err := netlifyRedirects.ParseString(tt.rule)
			require.NoError(t, err)

			parsedURL, err := url.Parse(tt.path)
			require.NoError(t, err)

			isMatch, path := matchesRule(context.Background(), &rules[0], parsedURL, "correlation_id")
			require.Equal(t, tt.expectMatch, isMatch)
			require.Equal(t, tt.expectedPath, path)
		})
	}
}

// Tests matching behavior when the `FF_ENABLE_DOMAIN_REDIRECT`
// feature flag is enabled.
func Test_matchesDomainRule(t *testing.T) {
	t.Setenv(feature.RedirectsPlaceholders.EnvVariable, "true")

	tests := map[string]testCaseData{
		"exact_path_match": {
			rule:         "/foo/ http://test.example.io/bar/",
			path:         "http://pages.example.io/foo/",
			expectMatch:  true,
			expectedPath: "http://test.example.io/bar/",
		},
		"exact_url_match": {
			rule:         "http://pages.example.io/foo/ http://test.example.io/bar/",
			path:         "http://pages.example.io/foo/",
			expectMatch:  true,
			expectedPath: "http://test.example.io/bar/",
		},
		"single_trailing_slash": {
			rule:         "http://pages.example.io/foo/ http://test.example.io/bar/",
			path:         "http://pages.example.io/foo",
			expectMatch:  true,
			expectedPath: "http://test.example.io/bar/",
		},
		"ignore_missing_slash": {
			rule:         "http://pages.example.io/foo http://test.example.io/bar/",
			path:         "http://pages.example.io/foo/",
			expectMatch:  true,
			expectedPath: "http://test.example.io/bar/",
		},
		"no_match": {
			rule:         "http://pages.example.io/foo http://test.example.io/bar/",
			path:         "http://pages.example.io/foo/bar",
			expectMatch:  false,
			expectedPath: "",
		},

		// Note: the following 3 cases behave differently when
		// placeholders are disabled. See the similar test cases below.
		"multiple_trailing_slashes": {
			rule:         "http://pages.example.io/foo/ http://test.example.io/bar/",
			path:         "http://pages.example.io/foo//",
			expectMatch:  true,
			expectedPath: "http://test.example.io/bar/",
		},
		"multiple_slashes_in_middle": {
			rule:         "http://pages.example.io/foo/bar http://test.example.io/baz/",
			path:         "http://pages.example.io/foo//bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/baz/",
		},

		"domain_redirect_different_protocol": {
			rule:         "http://pages.example.io/foo/bar https://test.example.io/baz/",
			path:         "http://pages.example.io/foo//bar",
			expectMatch:  true,
			expectedPath: "https://test.example.io/baz/",
		},

		"splat_match": {
			rule:         "http://pages.example.io/foo/*/bar http://test.example.io/foo/:splat/qux",
			path:         "http://pages.example.io/foo/baz/bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/foo/baz/qux",
		},
		"splat_match_multiple_segments": {
			rule:         "http://pages.example.io/foo/*/bar http://test.example.io/foo/:splat/qux",
			path:         "http://pages.example.io/foo/hello/world/bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/foo/hello/world/qux",
		},
		"splat_match_ignore_trailing_slash": {
			rule:         "http://pages.example.io/foo/*/bar http://test.example.io/foo/:splat/qux",
			path:         "http://pages.example.io/foo/baz/bar/",
			expectMatch:  true,
			expectedPath: "http://test.example.io/foo/baz/qux",
		},
		"splat_match_end": {
			rule:         "http://pages.example.io/foo/* http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo/baz/bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/baz/bar",
		},
		"splat_match_end_with_slash": {
			rule:         "http://pages.example.io/foo/* http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo/baz/bar/",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/baz/bar/",
		},
		"splat_match_beginning": {
			rule:         "http://pages.example.io/*/baz/bar http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo/baz/bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/foo",
		},
		"splat_match_empty_suffix": {
			rule:         "http://pages.example.io/foo/* http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo/",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/",
		},
		"splat_consumes_trailing_slash": {
			rule:         "http://pages.example.io/foo/* http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/",
		},
		"splat_match_empty_prefix": {
			rule:         "http://pages.example.io/*/foo http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/",
		},
		"splat_mid_segment": {
			rule:         "http://pages.example.io/foo*bar http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foobazbar",
			expectMatch:  false,
			expectedPath: "",
		},
		"splat_mid_segment_no_content": {
			rule:         "http://pages.example.io/foo*bar http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foobar",
			expectMatch:  false,
			expectedPath: "",
		},
		"splat_ext_html": {
			rule:         "http://pages.example.io/*.html http://test.example.io/:splat",
			path:         "http://pages.example.io/foo.html",
			expectMatch:  true,
			expectedPath: "http://test.example.io/foo",
		},
		"splat_ext_html_nested": {
			rule:         "http://pages.example.io/blog/*.html http://test.example.io/blog/:splat",
			path:         "http://pages.example.io/blog/my-post.html",
			expectMatch:  true,
			expectedPath: "http://test.example.io/blog/my-post",
		},
		"splat_ext_html_no_match_different_ext": {
			rule:         "http://pages.example.io/*.html http://test.example.io/:splat",
			path:         "http://pages.example.io/foo.txt",
			expectMatch:  false,
			expectedPath: "",
		},
		"splat_ext_html_cross_directory": {
			rule:         "http://pages.example.io/*.html http://test.example.io/:splat",
			path:         "http://pages.example.io/sub/foo.html",
			expectMatch:  true,
			expectedPath: "http://test.example.io/sub/foo",
		},
		"lone_splat": {
			rule:         "http://pages.example.io/* http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo/bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/foo/bar",
		},
		"duplicate_splat_replacements": {
			rule:         "http://pages.example.io/foo/* http://test.example.io/qux/:splat/:splat",
			path:         "http://pages.example.io/foo/hello",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/hello/hello",
		},
		"splat_missing_path_segment_behavior": {
			rule:         "http://pages.example.io/foo/*/bar http://test.example.io/foo/:splat/qux",
			path:         "http://pages.example.io/foo/bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/foo/qux",
		},
		"missing_splat_placeholder": {
			rule:         "http://pages.example.io/foo/ http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo/",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/",
		},
		"placeholder_match": {
			rule:         "http://pages.example.io/foo/:year/:month/:day/bar http://test.example.io/qux/:year-:month-:day",
			path:         "http://pages.example.io/foo/2021/08/16/bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/2021-08-16",
		},
		"placeholder_match_end": {
			rule:         "http://pages.example.io/foo/:placeholder http://test.example.io/qux/:placeholder",
			path:         "http://pages.example.io/foo/bar",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/bar",
		},
		"placeholder_match_beginning": {
			rule:         "http://pages.example.io/:placeholder/foo http://test.example.io/qux/:placeholder",
			path:         "http://pages.example.io/baz/foo",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/baz",
		},
		"placeholder_no_multiple_segments": {
			rule:         "http://pages.example.io/foo/:placeholder/bar http://test.example.io/foo/:placeholder/qux",
			path:         "http://pages.example.io/foo/hello/world/bar",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_at_beginning_no_content": {
			rule:         "http://pages.example.io/:placeholder/foo http://test.example.io/qux/:placeholder",
			path:         "http://pages.example.io/foo",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_at_end_no_content": {
			rule:         "http://pages.example.io/foo/:placeholder http://test.example.io/qux/:placeholder",
			path:         "http://pages.example.io/foo/",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_mid_segment_in_from": {
			rule:         "http://pages.example.io/foo:placeholder http://test.example.io/qux/:placeholder",
			path:         "http://pages.example.io/foorbar",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_mid_segment_in_to": {
			rule:         "http://pages.example.io/foo/:placeholder http://test.example.io/qux/bar:placeholder",
			path:         "http://pages.example.io/foo/baz",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/barbaz",
		},
		"placeholder_missing_replacement_with_substring": {
			rule:         "http://pages.example.io/:foo http://test.example.io/:foobar",
			path:         "http://pages.example.io/baz",
			expectMatch:  true,
			expectedPath: "http://test.example.io/",
		},
		"placeholder_mid_segment_no_content": {
			rule:         "http://pages.example.io/foo:placeholder http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo",
			expectMatch:  false,
			expectedPath: "",
		},
		"placeholder_name_substring": {
			rule:         "http://pages.example.io/foo/:foo/:foobar http://test.example.io/qux/:foo/:foobar",
			path:         "http://pages.example.io/foo/baz/quux",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/baz/quux",
		},
		"lone_placeholder": {
			rule:         "http://pages.example.io/:placeholder http://test.example.io/qux/:placeholder",
			path:         "http://pages.example.io/foo",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/foo",
		},
		"duplicate_placeholders": {
			rule:         "http://pages.example.io/foo/:placeholder/bar/:placeholder/baz http://test.example.io/qux/:placeholder",
			path:         "http://pages.example.io/foo/hello/bar/world/baz",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/hello",
		},
		"duplicate_placeholder_replacements": {
			rule:         "http://pages.example.io/foo/:placeholder http://test.example.io/qux/:placeholder/:placeholder",
			path:         "http://pages.example.io/foo/hello",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/hello/hello",
		},
		"splat_and_placeholder_named_splat": {
			rule:         "http://pages.example.io/foo/*/bar/:splat http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo/hello/bar/world",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/hello",
		},
		"placeholder_named_splat_and_splat": {
			rule:         "http://pages.example.io/foo/:splat/bar/* http://test.example.io/qux/:splat",
			path:         "http://pages.example.io/foo/hello/bar/world",
			expectMatch:  true,
			expectedPath: "http://test.example.io/qux/hello",
		},

		// Note: These differ slightly from Netlify's matching behavior.
		// GitLab replaces _all_ placeholders in the "to" path, even if
		// the placeholder doesn't have corresponding match in the "from".
		// Netlify only replaces placeholders that appear in the "from".
		"missing_placeholder_exact_match": {
			rule:        "http://pages.example.io/foo/ http://test.example.io/qux/:placeholder",
			path:        "http://pages.example.io/foo/",
			expectMatch: true,

			// Netlify would instead redirect to "/qux/:placeholder"
			expectedPath: "http://test.example.io/qux/",
		},
		"missing_placeholder_nonexact_match": {
			rule:        "http://pages.example.io/foo/:placeholderA http://test.example.io/qux/:placeholderB",
			path:        "http://pages.example.io/foo/bar",
			expectMatch: true,

			// Netlify would instead redirect to "/qux/:placeholderB"
			expectedPath: "http://test.example.io/qux/",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rules, err := netlifyRedirects.ParseString(tt.rule)
			require.NoError(t, err)

			parsedURL, err := url.Parse(tt.path)
			require.NoError(t, err)

			isMatch, path := matchesRule(context.Background(), &rules[0], parsedURL, "correlation_id")
			require.Equal(t, tt.expectMatch, isMatch)
			require.Equal(t, tt.expectedPath, path)
		})
	}
}
