package utils

import "net/url"

// IsDomainURL checks if the given urlString is a valid domain URL with scheme and host parts.
// Returns true if urlString is a valid domain URL, false otherwise.
func IsDomainURL(urlString string) bool {
	parsedURL, err := url.Parse(urlString)
	if err != nil {
		return false
	}
	if len(parsedURL.Scheme) > 0 && len(parsedURL.Host) > 0 {
		return true
	}
	return false
}

// MatchHost checks if the originalURL matches the domain and path provided in path argument.
// It returns a bool indicating if there is a host match and the matched path.
// It returns true and path when path does not contain scheme and host
func MatchHost(originalURL *url.URL, path string) (bool, string) {
	if !IsDomainURL(path) {
		return true, path
	}

	parsedURL, err := url.Parse(path)
	if err != nil {
		return false, ""
	}

	if originalURL.Scheme == parsedURL.Scheme && originalURL.Host == parsedURL.Host {
		return true, parsedURL.Path
	}
	return false, ""
}

// ParseURL validates and parses a URL string.
func ParseURL(urlStr string) (*url.URL, bool) {
	if urlStr == "" {
		return nil, false
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, false
	}

	return parsedURL, true
}
