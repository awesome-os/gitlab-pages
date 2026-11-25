package request

import (
	"net"
	"net/http"
	"net/netip"
)

const (
	// SchemeHTTP name for the HTTP scheme
	SchemeHTTP = "http"
	// SchemeHTTPS name for the HTTPS scheme
	SchemeHTTPS = "https"
	// IPV6PrefixLength is the length of the IPv6 prefix
	IPV6PrefixLength = 64
)

// IsHTTPS checks whether the request originated from HTTP or HTTPS.
// It checks the value from r.URL.Scheme
func IsHTTPS(r *http.Request) bool {
	return r.URL.Scheme == SchemeHTTPS
}

// GetHostWithoutPort returns a host without the port. The host(:port) comes
// from a Host: header if it is provided, otherwise it is a server name.
func GetHostWithoutPort(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		return r.Host
	}

	return host
}

// GetRemoteAddrWithoutPort strips the port from the r.RemoteAddr
func GetRemoteAddrWithoutPort(r *http.Request) string {
	remoteAddr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return remoteAddr
}

// GetIPV4orIPV6PrefixWithoutPort strips the port from the r.RemoteAddr
// and returns IPV4 address or IPV6 Prefix.
func GetIPV4orIPV6PrefixWithoutPort(r *http.Request) string {
	return GetIPV4orIPV6Prefix(r.RemoteAddr)
}

// GetIPV4orIPV6Prefix returns either the full IPv4 address or the /64 prefix
// of the IPv6 address from the provided remote address, without the port.
// For IPv4 it returns the full address. For IPv6 it returns the /64 prefix.
func GetIPV4orIPV6Prefix(remoteAddr string) string {
	remoteIP, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		remoteIP = remoteAddr
	}

	addr, err := netip.ParseAddr(remoteIP)
	if err != nil {
		return remoteIP
	}

	if addr.Is4() {
		return remoteIP
	} else if addr.Is6() {
		ipv6Prefix, err := addr.Prefix(IPV6PrefixLength)
		if err != nil {
			return remoteIP
		}
		return ipv6Prefix.String()
	}

	return remoteIP
}
