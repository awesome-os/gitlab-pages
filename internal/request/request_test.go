package request

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsHTTPS(t *testing.T) {
	tests := map[string]struct {
		u                    string
		scheme               string
		xForwardedProtoHTTPS bool
		expected             bool
	}{
		"when scheme is http": {
			u:        "/",
			scheme:   SchemeHTTP,
			expected: false,
		},
		"when scheme is https": {
			u:        "/",
			scheme:   SchemeHTTPS,
			expected: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.u, nil)
			req.URL.Scheme = test.scheme

			if test.xForwardedProtoHTTPS {
				req.Header.Set("X-Forwarded-Proto", "https")
			}

			require.Equal(t, test.expected, IsHTTPS(req))
		})
	}
}

func TestGetHostWithoutPort(t *testing.T) {
	tests := map[string]struct {
		u        string
		host     string
		expected string
	}{
		"when port component is provided": {
			u:        "https://example.com:443",
			host:     "my.example.com:8080",
			expected: "my.example.com",
		},
		"when port component is not provided": {
			u:        "http://example.com",
			host:     "my.example.com",
			expected: "my.example.com",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.u, nil)
			req.Host = test.host

			host := GetHostWithoutPort(req)
			require.Equal(t, test.expected, host)
		})
	}
}

func TestGetRemoteAddrWithoutPort(t *testing.T) {
	tests := map[string]struct {
		u          string
		remoteAddr string
		expected   string
	}{
		"when port component is provided": {
			u:          "https://example.com:443",
			remoteAddr: "127.0.0.1:1000",
			expected:   "127.0.0.1",
		},
		"when port component is not provided": {
			u:          "http://example.com",
			remoteAddr: "127.0.0.1",
			expected:   "127.0.0.1",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.u, nil)
			req.RemoteAddr = test.remoteAddr

			addr := GetRemoteAddrWithoutPort(req)
			require.Equal(t, test.expected, addr)
		})
	}
}

func TestGetIPV4orIPV6PrefixWithoutPort(t *testing.T) {
	tests := map[string]struct {
		u          string
		remoteAddr string
		expected   string
	}{
		"when IPv4 and port component is provided": {
			u:          "https://example.com:443",
			remoteAddr: "127.0.0.1:1000",
			expected:   "127.0.0.1",
		},
		"when IPv4 and port component is not provided": {
			u:          "http://example.com",
			remoteAddr: "127.0.0.1",
			expected:   "127.0.0.1",
		},
		"when IPv6 and port component is provided": {
			u:          "https://[2001:db8:3333:4444:5555:6666:7777:8888]:1234",
			remoteAddr: "[2001:db8:3333:4444:5555:6666:7777:8888]:1234",
			expected:   "2001:db8:3333:4444::/64",
		},
		"when IPv6 and port component is not provided": {
			u:          "https://[2001:db8:3333:4444:5555:6666:7777:8888]",
			remoteAddr: "2001:db8:3333:4444:5555:6666:7777:8888",
			expected:   "2001:db8:3333:4444::/64",
		},
		"when empty remoteAddr is provided": {
			u:          "http://example.com",
			remoteAddr: "",
			expected:   "",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.u, nil)
			req.RemoteAddr = test.remoteAddr

			addr := GetIPV4orIPV6PrefixWithoutPort(req)
			require.Equal(t, test.expected, addr)
		})
	}
}

func TestGetIPV4orIPV6Prefix(t *testing.T) {
	tests := map[string]struct {
		remoteAddr string
		expected   string
	}{
		"when IPv4 and port component is provided": {
			remoteAddr: "127.0.0.1:1000",
			expected:   "127.0.0.1",
		},
		"when IPv4 and port component is not provided": {
			remoteAddr: "127.0.0.1",
			expected:   "127.0.0.1",
		},
		"when IPv6 and port component is provided": {
			remoteAddr: "[2001:db8:3333:4444:5555:6666:7777:8888]:1234",
			expected:   "2001:db8:3333:4444::/64",
		},
		"when IPv6 and port component is not provided": {
			remoteAddr: "2001:db8:3333:4444:5555:6666:7777:8888",
			expected:   "2001:db8:3333:4444::/64",
		},
		"when empty remoteAddr is provided": {
			remoteAddr: "",
			expected:   "",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			addr := GetIPV4orIPV6Prefix(test.remoteAddr)
			require.Equal(t, test.expected, addr)
		})
	}
}
