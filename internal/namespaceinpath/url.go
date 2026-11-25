package namespaceinpath

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var (
	errURLNotPagesDomain    = errors.New("URL does not match pages domain")
	errFirstSegmentEmpty    = errors.New("can't extract namespace from path because first segment is empty")
	errCantExtractNamespace = errors.New("can't extract namespace from host")
)

type customURL struct {
	*url.URL
	pagesDomain string
}

// convertFromNamespaceInPath: Converts a URL with the namespace in the path to a URL with the namespace in the host.
// This function extracts the namespace from the path and appends it to the host, then removes the namespace from the path.
// For example, it converts "example.com/namespace/a-path" to "namespace.example.com/a-path".
func (u *customURL) convertFromNamespaceInPath() error {
	if !u.isPagesDomain() {
		return errURLNotPagesDomain
	}

	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return errFirstSegmentEmpty
	}

	namespace := segments[0]

	hostWithNamespace := namespace + "." + u.Host
	u.Host = hostWithNamespace

	u.Path = strings.TrimPrefix(u.Path, "/"+namespace)
	return nil
}

// convertToNamespaceInPath: Converts a URL with the namespace in the host to a URL with the namespace in the path.
// This function extracts the namespace from the host and prepends it to the path, then removes the namespace from the host.
// For example, it converts "namespace.example.com/a-path" to "example.com/namespace/a-path".
func (u *customURL) convertToNamespaceInPath() error {
	_, port, _ := net.SplitHostPort(u.Host)

	pagesDomainWithPort := u.pagesDomain
	if port != "" {
		pagesDomainWithPort = u.pagesDomain + ":" + port
	}

	if strings.HasSuffix(u.Host, "."+pagesDomainWithPort) {
		namespace := strings.Trim(strings.TrimSuffix(u.Host, pagesDomainWithPort), ".")
		if len(namespace) > 0 {
			u.Host = pagesDomainWithPort
			u.Path = "/" + namespace + "/" + strings.TrimPrefix(u.Path, "/")
			return nil
		}
	}

	return errCantExtractNamespace
}

func (u *customURL) isPagesDomain() bool {
	hostWithoutPort, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		hostWithoutPort = u.Host
	}
	return hostWithoutPort == u.pagesDomain
}
