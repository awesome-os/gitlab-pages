package redirects

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	netlifyRedirects "github.com/tj/go-redirects"

	"gitlab.com/gitlab-org/gitlab-pages/internal/feature"
	"gitlab.com/gitlab-org/gitlab-pages/internal/utils"
)

var (
	regexPlaceholder            = regexp.MustCompile(`(?i)^:[a-z]+$`)
	regexSplat                  = regexp.MustCompile(`^\*$`)
	regexSplatExt               = regexp.MustCompile(`^\*\..+$`)
	regexPlaceholderReplacement = regexp.MustCompile(`(?i):(?P<placeholder>[a-z]+)`)
)

// validateFromURL validates the from URL in a redirect rule.
// It checks for various invalid cases like unsupported schemes,
// relative URLs, domain redirects without scheme, etc.
// Returns `nil` if the URL is valid.
// nolint: gocyclo
func validateFromURL(urlText string) error {
	fromURL, err := url.Parse(urlText)
	if err != nil {
		return errFailedToParseURL
	}

	// No support for domain level redirects starting with special characters without scheme:
	// - `//google.com`
	// - `/\google.com`
	if (fromURL.Host == "") != (fromURL.Scheme == "") || strings.HasPrefix(fromURL.Path, "/\\") {
		return errNoValidStartingInURLPath
	}

	if fromURL.Scheme != "" && fromURL.Scheme != "http" && fromURL.Scheme != "https" {
		return errNoValidStartingInURLPath
	}

	if fromURL.Scheme == "" && fromURL.Host == "" {
		// No parent traversing relative URL's with `./` or `../`
		// No ambiguous URLs like bare domains `GitLab.com`
		if !strings.HasPrefix(urlText, "/") {
			return errNoValidStartingInURLPath
		}
	}

	if feature.RedirectsPlaceholders.Enabled() && countSplats(fromURL.Path) > 1 {
		return errMoreThanOneSplats
	}

	return validateSplatAndPlaceholders(fromURL.Path)
}

// validateURL runs validations against a rule URL.
// Returns `nil` if the URL is valid.
func validateToURL(urlText string, status int) error {
	toURL, err := url.Parse(urlText)
	if err != nil {
		return errFailedToParseURL
	}

	// No support for domain level redirects starting with // or special characters:
	// - `//google.com`
	// - `/\google.com`
	if (toURL.Host == "") != (toURL.Scheme == "") || strings.HasPrefix(toURL.Path, "/\\") {
		return errNoValidStartingInURLPath
	}

	// No support for domain level rewrite
	if utils.IsDomainURL(urlText) && status == http.StatusOK {
		return errNoDomainLevelRewrite
	}

	allowedPrefix := []string{"/", "http://", "https://"}
	// No parent traversing relative URL's with `./` or `../`
	// No ambiguous URLs like bare domains `GitLab.com`
	if !startsWithAnyPrefix(urlText, allowedPrefix...) {
		return errNoValidStartingInURLPath
	}

	return validateSplatAndPlaceholders(toURL.Path)
}

func validateSplatAndPlaceholders(path string) error {
	if feature.RedirectsPlaceholders.Enabled() {
		maxPathSegments := cfg.MaxPathSegments
		// Limit the number of path segments a rule can contain.
		// This prevents the matching logic from generating regular
		// expressions that are too large/complex.
		if strings.Count(path, "/") > maxPathSegments {
			return fmt.Errorf("url path cannot contain more than %d forward slashes", cfg.MaxPathSegments)
		}
	} else {
		// No support for splats, https://docs.netlify.com/routing/redirects/redirect-options/#splats
		if strings.Contains(path, "*") {
			return errNoSplats
		}

		// No support for placeholders, https://docs.netlify.com/routing/redirects/redirect-options/#placeholders
		if regexpPlaceholder.MatchString(path) {
			return errNoPlaceholders
		}
	}

	return nil
}

// validateRule runs all validation rules on the provided rule.
// Returns `nil` if the rule is valid
func validateRule(r netlifyRedirects.Rule) error {
	if err := validateFromURL(r.From); err != nil {
		return err
	}

	if err := validateToURL(r.To, r.Status); err != nil {
		return err
	}

	// No support for query parameters, https://docs.netlify.com/routing/redirects/redirect-options/#query-parameters
	if r.Params != nil {
		return errNoParams
	}

	// We strictly validate return status codes
	switch r.Status {
	case http.StatusOK, http.StatusMovedPermanently, http.StatusFound:
		// noop
	default:
		return errUnsupportedStatus
	}

	// No support for rules that use ! force
	if r.Force {
		return errNoForce
	}

	return nil
}

func countSplats(path string) int {
	count := 0
	for _, segment := range strings.Split(path, "/") {
		if strings.Contains(segment, "*") {
			count++
		}
	}
	return count
}

func startsWithAnyPrefix(s string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
