package redirects

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	netlifyRedirects "github.com/tj/go-redirects"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/utils"
)

var (
	regexMultipleSlashes     = regexp.MustCompile(`(^|[^:])//+`)
	regexPlaceholderOrSplats = regexp.MustCompile(`(?i)\*|:[a-z]+`)
)

// matchesRule returns `true` if the rule's "from" pattern matches the requested URL.
// This internally calls matchesRuleWithPlaceholderOrSplats to match rules.
//
// For example, given a "from" URL like this:
//
//	/a/*/url/with/:placeholders
//
// this function would match URLs like this:
//
//	/a/nice/url/with/text
//	/a/super/extra/nice/url/with/matches
//
// If the first return value is `true`, the second return value is the path that this
// rule should redirect/rewrite to. This path is effectively the rule's "to" path that
// has been templated with all the placeholders (if any) from the originally requested URL.
func matchesRule(ctx context.Context, rule *netlifyRedirects.Rule, originalURL *url.URL, correlationID string) (bool, string) {
	hostMatches, fromPath := utils.MatchHost(originalURL, rule.From)

	if !hostMatches {
		return false, ""
	}

	path := originalURL.Path

	// If the requested URL exactly matches this rule's "from" path,
	// exit early and return the rule's "to" path to avoid building
	// and compiling the regex below.
	// However, only do this if there's nothing to template in the "to" path,
	// to avoid redirect/rewriting to a originalURL with a literal `:placeholder` in it.
	if normalizePath(fromPath) == normalizePath(path) && !regexPlaceholderOrSplats.MatchString(rule.To) {
		return true, rule.To
	}

	return matchesRuleWithPlaceholderOrSplats(ctx, path, fromPath, rule.To, rule.Status, correlationID)
}

// matchesRuleWithPlaceholderOrSplats returns `true` if the rule's "from" pattern matches the requested URL.
// This is specifically for Placeholders and Splats matching
//
// For example, given a "from" URL like this:
//
//	/a/*/originalURL/with/:placeholders
//
// this function would match URLs like this:
//
//	/a/nice/originalURL/with/text
//	/a/super/extra/nice/originalURL/with/matches
//
// If the first return value is `true`, the second return value is the path that this
// rule should redirect/rewrite to. This path is effectively the rule's "to" path that
// has been templated with all the placeholders (if any) from the originally requested URL.
func matchesRuleWithPlaceholderOrSplats(ctx context.Context, requestPath string, fromPath string, toPath string, status int, correlationID string) (bool, string) {
	// Any logic beyond this point handles placeholders and splats.
	// If the FF_ENABLE_PLACEHOLDERS feature flag isn't enabled, exit now.
	if !feature.RedirectsPlaceholders.Enabled() {
		return false, ""
	}

	regexSegments := convertToRegexSegments(fromPath)
	if len(regexSegments) == 0 {
		return false, ""
	}

	fromRegexString := `(?i)^` + strings.Join(regexSegments, "") + `/*$`
	fromRegex, err := regexp.Compile(fromRegexString)
	if err != nil {
		logv2.FromContext(ctx).WarnContext(ctx,
			fmt.Sprintf("matchesRule generated an invalid regex: %q", fromRegexString),
			logv2.Error(err),
			slog.String("fromRegexString", fromRegexString),
			slog.String("rule.From", fromPath),
			slog.String("rule.To", toPath),
			slog.Int("rule.Status", status),
			slog.String("path", requestPath),
			logv2.CorrelationID(correlationID),
		)

		return false, ""
	}

	// Escape any literal "$" in the "to" path first, so that ExpandString
	// below keeps it as is instead of reading it as a group reference.
	escapedToPath := strings.ReplaceAll(toPath, "$", "$$")
	template := regexPlaceholderReplacement.ReplaceAllString(escapedToPath, `${$placeholder}`)
	subMatchIndex := fromRegex.FindStringSubmatchIndex(requestPath)

	if subMatchIndex == nil {
		return false, ""
	}

	var templatedToPath []byte
	templatedToPath = fromRegex.ExpandString(templatedToPath, template, requestPath, subMatchIndex)

	// Some replacements result in subsequent slashes. For example, a rule with a "to"
	// like `foo/:splat/bar` will result in a path like `foo//bar` if the splat
	// character matches nothing. To avoid this, replace all instances
	// of multiple subsequent forward slashes with a single forward slash.
	// The regex captures any character except a colon ([^:]) before the double slashes
	// and includes it in the replacement. It also matches slashes at the start of the
	// path, so that a "to" like `/:splat` whose splat starts with a slash never begins
	// with `//`, which URL parsing reads as a host.
	templatedToPath = regexMultipleSlashes.ReplaceAll(templatedToPath, []byte("$1/"))

	return true, string(templatedToPath)
}

// convertToRegexSegments converts the path string to an array of regex segments
// It replaces placeholders with named capture groups and splat characters with a wildcard regex
// This allows matching the path segments to the request path and extracting matched placeholder values
func convertToRegexSegments(path string) []string {
	var regexSegments []string

	for _, segment := range strings.Split(path, "/") {
		if segment == "" {
			continue
		} else if regexSplat.MatchString(segment) {
			regexSegments = append(regexSegments, `(/(?P<splat>.*))?/*`)
		} else if regexSplatExt.MatchString(segment) {
			parts := strings.SplitN(segment, "*", 2)
			prefix := regexp.QuoteMeta(parts[0])
			suffix := regexp.QuoteMeta(parts[1])
			regexSegments = append(regexSegments, fmt.Sprintf(`/+%s(?P<splat>.*)%s`, prefix, suffix))
		} else if regexPlaceholder.MatchString(segment) {
			segmentName := strings.Replace(segment, ":", "", 1)
			regexSegments = append(regexSegments, fmt.Sprintf(`/+(?P<%s>[^/]+)`, segmentName))
		} else {
			regexSegments = append(regexSegments, "/+"+regexp.QuoteMeta(segment))
		}
	}

	return regexSegments
}

// `match` returns:
// 1. The first valid redirect or rewrite rule that matches the requested URL
// 2. The URL to redirect/rewrite to
//
// If no rule matches, this function returns `nil` and an empty string
func (r *Redirects) match(ctx context.Context, originalURL *url.URL, correlationID string) (*netlifyRedirects.Rule, string) {
	for i := range r.rules {
		if i >= cfg.MaxRuleCount {
			// do not process any more rules
			return nil, ""
		}

		// assign rule to a new var to prevent the following gosec error
		// G601: Implicit memory aliasing in for loop
		rule := r.rules[i]

		if validateRule(rule) != nil {
			continue
		}

		if isMatch, path := matchesRule(ctx, &rule, originalURL, correlationID); isMatch {
			return &rule, path
		}
	}

	return nil, ""
}
