package acceptance_test

import (
	"bufio"
	"context"
	"crypto/tls"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

//go:embed testdata/client.crt
var fixtureClientCert string

//go:embed testdata/client.key
var fixtureClientKey string

func TestGlobalMutualTLS(t *testing.T) {
	RunPagesProcess(t,
		withListeners([]ListenSpec{httpsListener}),
		withExtraArgument("tls-client-auth", "requireandverifyclientcert"),
		withExtraArgument("tls-client-cert", "../../test/acceptance/testdata/ca.crt"),
	)

	t.Run("client with cert works", func(t *testing.T) {
		clientCert, err := tls.X509KeyPair([]byte(fixtureClientCert), []byte(fixtureClientKey))
		require.NoError(t, err)

		client, cleanup := ClientWithConfig(&tls.Config{
			Certificates: []tls.Certificate{clientCert},
		})
		defer cleanup()

		rsp, err := client.Get(httpsListener.URL("/"))
		require.NoError(t, err)
		require.NoError(t, rsp.Body.Close())
	})
	t.Run("client without cert fails", func(t *testing.T) {
		client, cleanup := ClientWithConfig(&tls.Config{})
		defer cleanup()

		rsp, err := client.Get(httpsListener.URL("/"))
		require.Error(t, err)
		require.ErrorContains(t, err, tlsMissingCertificateErrorMessage)
		require.Nil(t, rsp)
	})
}

func TestGlobalDomainBasedMutualTLS(t *testing.T) {
	RunPagesProcess(t,
		withListeners([]ListenSpec{clientCertListener}),
		withExtraArgument("tls-client-auth", "requireandverifyclientcert"),
		withExtraArgument("tls-client-cert", "../../test/acceptance/testdata/ca.crt"),
		withExtraArgument("tls-client-auth-domains", "test.gitlab-example.com"),
	)

	noClientCertListener := ListenSpec{
		Type:       httpsListener.Type,
		Host:       httpsListener.Host,
		Port:       clientCertListener.Port,
		ClientCert: false,
	}

	t.Run("client with cert works to mtls domain", func(t *testing.T) {
		rsp, err := GetPageFromListener(t, clientCertListener, "test.gitlab-example.com", "/")

		require.NoError(t, err)
		require.NoError(t, rsp.Body.Close())
	})
	t.Run("client without cert fails", func(t *testing.T) {
		rsp, err := GetPageFromListener(t, noClientCertListener, "test.gitlab-example.com", "/")
		require.Error(t, err)
		require.ErrorContains(t, err, tlsMissingCertificateErrorMessage)
		require.Nil(t, rsp)
	})
	t.Run("client without cert works to non-mtls domain", func(t *testing.T) {
		rsp, err := GetPageFromListener(t, noClientCertListener, "other-test.gitlab-example.com", "/")

		require.NoError(t, err)
		require.NoError(t, rsp.Body.Close())
	})
}

func TestGitLabAPIBasedMutualTLS(t *testing.T) {
	// Debug statement to print the current working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	t.Logf("Current working directory: %s", cwd)

	RunPagesProcess(t,
		withListeners([]ListenSpec{clientCertListener}),
		withExtraArgument("tls-client-auth", "requireandverifyclientcert"),
		withExtraArgument("tls-client-auth-domains", "mtls.gitlab-example.com"),
		withExtraArgument("tls-client-cert", "../../test/acceptance/testdata/ca.crt"),
	)

	noClientCertListener := ListenSpec{
		Type:       httpsListener.Type,
		Host:       httpsListener.Host,
		Port:       clientCertListener.Port,
		ClientCert: false,
	}

	t.Run("client with cert works to mtls domain", func(t *testing.T) {
		rsp, err := GetPageFromListener(t, clientCertListener, "mtls.gitlab-example.com", "/")

		require.NoError(t, err)
		require.EqualValues(t, http.StatusOK, rsp.StatusCode)
		require.NoError(t, rsp.Body.Close())
	})
	t.Run("client without cert fails", func(t *testing.T) {
		rsp, err := GetPageFromListener(t, noClientCertListener, "mtls.gitlab-example.com", "/")
		require.Error(t, err)
		require.ErrorContains(t, err, tlsMissingCertificateErrorMessage)
		require.Nil(t, rsp)
	})
	t.Run("client without cert works to non-mtls domain", func(t *testing.T) {
		rsp, err := GetPageFromListener(t, noClientCertListener, "group.gitlab-example.com", "/")

		require.NoError(t, err)
		require.NoError(t, rsp.Body.Close())
		require.EqualValues(t, http.StatusOK, rsp.StatusCode)
	})
	t.Run("client with cert works to non-mtls domain", func(t *testing.T) {
		rsp, err := GetPageFromListener(t, clientCertListener, "group.gitlab-example.com", "/")

		require.NoError(t, err)
		require.NoError(t, rsp.Body.Close())
		require.EqualValues(t, http.StatusOK, rsp.StatusCode)
	})
}

func TestAcceptsSupportedCiphers(t *testing.T) {
	RunPagesProcess(t,
		withListeners([]ListenSpec{httpsListener}),
	)

	tlsConfig := &tls.Config{
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		},
	}
	client, cleanup := ClientWithConfig(tlsConfig)
	defer cleanup()

	rsp, err := client.Get(httpsListener.URL("/"))
	require.NoError(t, err)

	t.Cleanup(func() {
		rsp.Body.Close()
	})
}

func tlsConfigWithInsecureCiphersOnly() *tls.Config {
	return &tls.Config{
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
			tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
		},
		MaxVersion: tls.VersionTLS12, // ciphers for TLS1.3 are not configurable and will work if enabled
	}
}

func TestRejectsUnsupportedCiphers(t *testing.T) {
	RunPagesProcess(t,
		withListeners([]ListenSpec{httpsListener}),
	)

	client, cleanup := ClientWithConfig(tlsConfigWithInsecureCiphersOnly())
	defer cleanup()

	rsp, err := client.Get(httpsListener.URL("/"))
	require.Nil(t, rsp)
	require.Error(t, err)
}

// DO NOT REMOVE for compatibility with the flag definition
// GoLang 1.23 no longer supports insecure ciphers.
// Therefore, starting from GitLab 17.10, the insecure-ciphers flag is ignored.
func TestEnableInsecureCiphers(t *testing.T) {
	RunPagesProcess(t,
		withListeners([]ListenSpec{httpsListener}),
		withExtraArgument("-insecure-ciphers", "true"),
	)

	client, cleanup := ClientWithConfig(tlsConfigWithInsecureCiphersOnly())
	defer cleanup()

	rsp, err := client.Get(httpsListener.URL("/"))
	require.Nil(t, rsp)
	require.Error(t, err)
}

func TestTLSVersions(t *testing.T) {
	tests := map[string]struct {
		tlsMin      string
		tlsMax      string
		tlsClient   uint16
		expectError bool
	}{
		"client version not supported":             {tlsMin: "tls1.2", tlsMax: "tls1.3", tlsClient: tls.VersionTLS10, expectError: true},
		"client version supported":                 {tlsMin: "tls1.2", tlsMax: "tls1.3", tlsClient: tls.VersionTLS12, expectError: false},
		"client and server using default settings": {tlsMin: "", tlsMax: "", tlsClient: 0, expectError: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var args []string
			if tc.tlsMin != "" {
				args = append(args, "-tls-min-version", tc.tlsMin)
			}
			if tc.tlsMax != "" {
				args = append(args, "-tls-max-version", tc.tlsMax)
			}

			RunPagesProcess(t,
				withListeners([]ListenSpec{httpsListener}),
				withArguments(args),
			)

			tlsConfig := &tls.Config{}
			if tc.tlsClient != 0 {
				tlsConfig.MinVersion = tc.tlsClient
				tlsConfig.MaxVersion = tc.tlsClient
			}
			client, cleanup := ClientWithConfig(tlsConfig)
			defer cleanup()

			rsp, err := client.Get(httpsListener.URL("/"))

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				rsp.Body.Close()
			}
		})
	}
}

// TestHTTPSWithoutSNI checks that a ClientHello with no server_name extension
// is served the wildcard certificate and logs no error.
func TestHTTPSWithoutSNI(t *testing.T) {
	logBuf := RunPagesProcess(t, withListeners([]ListenSpec{httpsListener}))

	spec := currentListener(httpsListener)

	conn, err := spec.DialTLSWithoutSNI(t)
	require.NoError(t, err)

	state := conn.ConnectionState()
	require.Empty(t, state.ServerName, "the ClientHello must not carry an SNI extension")
	require.NotEmpty(t, state.PeerCertificates)
	require.Contains(t, state.PeerCertificates[0].DNSNames, "*.gitlab-example.com",
		"expected the wildcard certificate configured via -root-cert")

	// SNI and the Host header are independent, so a client that omits SNI still
	// gets its page.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://group.gitlab-example.com/", nil)
	require.NoError(t, err)
	require.NoError(t, req.Write(conn))

	rsp, err := http.ReadResponse(bufio.NewReader(conn), req)
	require.NoError(t, err)
	t.Cleanup(func() { rsp.Body.Close() })

	require.Equal(t, http.StatusOK, rsp.StatusCode)

	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, "OK\n", string(body))

	// The access log line is written after the handshake on the same stream, so
	// waiting for it makes the assertion below ordered rather than racy.
	assertLogFound(t, logBuf, []string{`"host":"group.gitlab-example.com"`})

	require.NotContains(t, logBuf.String(), "invalid hostname")
	require.NotContains(t, logBuf.String(), "hostname cannot be empty")
}

// TestTLSRateLimitsWithoutSNI checks that TLS connections without SNI are
// still rate limited by both limiters.
func TestTLSRateLimitsWithoutSNI(t *testing.T) {
	const (
		rateLimit = 1
		attempts  = 10
	)

	// The domain limiter keys on the server name, so every SNI-less connection
	// shares the one "" bucket. Cover both limiters.
	tests := map[string]struct {
		limitFlag string
		limitName string
	}{
		"source IP limit": {
			limitFlag: "rate-limit-tls-source-ip",
			limitName: "tls_connections_by_source_ip",
		},
		"domain limit": {
			limitFlag: "rate-limit-tls-domain",
			limitName: "tls_connections_by_domain",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			logBuf := RunPagesProcess(t,
				withListeners([]ListenSpec{httpsListener}),
				withExtraArgument(tt.limitFlag, fmt.Sprint(rateLimit)),
				withExtraArgument(tt.limitFlag+"-burst", fmt.Sprint(rateLimit)),
			)

			spec := currentListener(httpsListener)

			// The startup health check already spent at least one token, so do
			// not assert on the exact number of handshakes that succeed first.
			var rateLimited bool
			for i := 0; i < attempts; i++ {
				if _, err := spec.DialTLSWithoutSNI(t); err != nil {
					require.ErrorContains(t, err, "remote error: tls: internal error", "handshake %d", i)
					rateLimited = true

					break
				}
			}

			require.True(t, rateLimited, "SNI-less handshakes bypassed the TLS rate limiter")

			assertLogFound(t, logBuf, []string{
				"TLS connection rate-limited",
				`"rate_limiter_name":"` + tt.limitName + `"`,
			})
		})
	}
}
