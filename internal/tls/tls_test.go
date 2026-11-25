package tls

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"gitlab.com/gitlab-org/labkit/v2/fields"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/metrics"
)

var cert = []byte(`-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----`)

var key = []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----`)

var rootNamespaceID = 1

var getConfig = func(ch *tls.ClientHelloInfo) (*tls.Config, error) {
	return &tls.Config{MinVersion: tls.VersionTLS12}, nil
}

func TestInvalidKeyPair(t *testing.T) {
	cfg := &config.Config{}
	getDomain := func(ctx context.Context, name string) (*domain.Domain, error) {
		return nil, nil
	}
	config, err := GetTLSConfig(cfg, getDomain, getConfig)
	require.Error(t, err)
	require.Nil(t, config)
}

func TestClientCert(t *testing.T) {
	cfg := &config.Config{
		General: config.General{
			RootCertificate: cert,
			RootKey:         key,
		},
		TLS: config.TLS{
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCert: "./testdata/cert.crt",
		},
	}

	getDomain := func(ctx context.Context, name string) (*domain.Domain, error) {
		return domain.New(name, string(cert), string(key), "", rootNamespaceID, nil), nil
	}

	tlsconfig, err := GetTLSConfig(cfg, getDomain, getConfig)
	require.NoError(t, err)
	require.NotNil(t, tlsconfig)

	// Test that the certificate is properly loaded
	cert, err := tlsconfig.GetCertificate(&tls.ClientHelloInfo{ServerName: "test.example.com"})
	require.NoError(t, err)
	require.NotNil(t, cert)

	// Fix type assertions to match correct function signatures
	require.IsType(t, (func(*tls.ClientHelloInfo) (*tls.Certificate, error))(nil), tlsconfig.GetCertificate)
	require.IsType(t, (func(*tls.ClientHelloInfo) (*tls.Config, error))(nil), tlsconfig.GetConfigForClient)
	require.Equal(t, tls.RequireAndVerifyClientCert, tlsconfig.ClientAuth)
	require.NotNil(t, tlsconfig.ClientCAs)
}

func TestGetTLSConfig(t *testing.T) {
	tests := map[string]struct {
		cfg       *config.Config
		getDomain func(context.Context, string) (*domain.Domain, error)
		expected  *tls.Config
		wantErr   bool
	}{
		"invalid key pair": {
			cfg: &config.Config{},
			getDomain: func(ctx context.Context, name string) (*domain.Domain, error) {
				return nil, nil
			},
			expected: nil,
			wantErr:  true,
		},
		"valid config with client cert": {
			cfg: &config.Config{
				General: config.General{
					RootCertificate: cert,
					RootKey:         key,
				},
				TLS: config.TLS{
					ClientAuth: tls.RequireAndVerifyClientCert,
					ClientCert: "./testdata/cert.crt",
					MinVersion: tls.VersionTLS12,
				},
			},
			getDomain: func(ctx context.Context, name string) (*domain.Domain, error) {
				return domain.New(name, string(cert), string(key), "", rootNamespaceID, nil), nil
			},
			expected: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			wantErr: false,
		},
		"valid config without client cert": {
			cfg: &config.Config{
				General: config.General{
					RootCertificate: cert,
					RootKey:         key,
				},
				TLS: config.TLS{
					MinVersion: tls.VersionTLS12,
				},
			},
			getDomain: func(ctx context.Context, name string) (*domain.Domain, error) {
				return domain.New(name, string(cert), string(key), "", rootNamespaceID, nil), nil
			},
			expected: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			wantErr: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			config, err := GetTLSConfig(tc.cfg, tc.getDomain, getConfig)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, config)
			require.Equal(t, tc.expected.MinVersion, config.MinVersion)
			require.Equal(t, preferredCipherSuites, config.CipherSuites)
			require.NotNil(t, config.GetCertificate)
			require.NotNil(t, config.GetConfigForClient)

			if len(tc.cfg.TLS.ClientCert) > 0 {
				require.Equal(t, tc.cfg.TLS.ClientAuth, config.ClientAuth)
				require.NotNil(t, config.ClientCAs)
			} else {
				require.Equal(t, tls.NoClientCert, config.ClientAuth)
				require.Nil(t, config.ClientCAs)
			}
		})
	}
}

// TestGetCertificateWithoutSNI checks that a ClientHello with no SNI is served
// the wildcard certificate without resolving the empty hostname.
func TestGetCertificateWithoutSNI(t *testing.T) {
	cfg := &config.Config{
		General: config.General{
			RootCertificate: cert,
			RootKey:         key,
		},
		TLS: config.TLS{MinVersion: tls.VersionTLS12},
	}

	wildcard, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)

	var lookups []string
	getDomain := func(_ context.Context, name string) (*domain.Domain, error) {
		lookups = append(lookups, name)
		return domain.New(name, string(cert), string(key), "", rootNamespaceID, nil), nil
	}

	tlsconfig, err := GetTLSConfig(cfg, getDomain, getConfig)
	require.NoError(t, err)

	gotCert, err := tlsconfig.GetCertificate(&tls.ClientHelloInfo{})
	require.NoError(t, err)
	require.NotNil(t, gotCert)
	require.Equal(t, wildcard.Certificate, gotCert.Certificate)
	require.Empty(t, lookups, "getDomain must not be called for a ClientHello without SNI")

	// A ClientHello that does carry SNI must still be resolved.
	gotCert, err = tlsconfig.GetCertificate(&tls.ClientHelloInfo{ServerName: "test.example.com"})
	require.NoError(t, err)
	require.NotNil(t, gotCert)
	require.Equal(t, []string{"test.example.com"}, lookups)
}

// stubConn is a net.Conn that only reports a remote address.
type stubConn struct {
	net.Conn
	remoteAddr net.Addr
}

func (s stubConn) RemoteAddr() net.Addr {
	return s.remoteAddr
}

// TestGetCertificateFallsBackToWildcard checks that any failure to resolve a
// certificate for the requested name degrades to the wildcard certificate,
// and that unexpected failures are logged and counted.
func TestGetCertificateFallsBackToWildcard(t *testing.T) {
	cfg := &config.Config{
		General: config.General{
			RootCertificate: cert,
			RootKey:         key,
		},
		TLS: config.TLS{MinVersion: tls.VersionTLS12},
	}

	wildcard, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)

	tests := map[string]struct {
		getDomain GetDomainFunc
		// errorType is the expected error_type log field and counter label, empty when
		// nothing is expected to be logged or counted.
		errorType string
		// errorMessage is the expected error_message log field, empty to only require
		// that it is present.
		errorMessage string
	}{
		"domain not found": {
			getDomain: func(context.Context, string) (*domain.Domain, error) {
				return nil, domain.ErrDomainDoesNotExist
			},
		},
		"lookup error": {
			getDomain: func(context.Context, string) (*domain.Domain, error) {
				return nil, errors.New("api unavailable")
			},
			errorType:    errorTypeDomainLookupFailed,
			errorMessage: "api unavailable",
		},
		"no domain for hostname": {
			getDomain: func(context.Context, string) (*domain.Domain, error) {
				return nil, nil
			},
		},
		"domain without custom certificate": {
			getDomain: func(_ context.Context, name string) (*domain.Domain, error) {
				return domain.New(name, "", "", "", rootNamespaceID, nil), nil
			},
		},
		"invalid custom certificate": {
			getDomain: func(_ context.Context, name string) (*domain.Domain, error) {
				return domain.New(name, "invalid-cert", "invalid-key", "", rootNamespaceID, nil), nil
			},
			errorType: errorTypeCertificateLoadFailed,
		},
	}

	errorTypes := []string{errorTypeDomainLookupFailed, errorTypeCertificateLoadFailed}
	remoteAddr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4242}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			previousLogger := logging.Logger
			logging.Logger = logging.NewLogger(&logv2.Config{Writer: buf})
			t.Cleanup(func() { logging.Logger = previousLogger })

			before := map[string]float64{}
			for _, r := range errorTypes {
				before[r] = testutil.ToFloat64(metrics.TLSCertificateErrors.WithLabelValues(r))
			}

			tlsconfig, err := GetTLSConfig(cfg, tc.getDomain, getConfig)
			require.NoError(t, err)

			gotCert, err := tlsconfig.GetCertificate(&tls.ClientHelloInfo{
				ServerName: "test.example.com",
				Conn:       stubConn{remoteAddr: remoteAddr},
			})
			require.NoError(t, err)
			require.Equal(t, wildcard.Certificate, gotCert.Certificate)

			for _, r := range errorTypes {
				want := 0.0
				if r == tc.errorType {
					want = 1
				}
				got := testutil.ToFloat64(metrics.TLSCertificateErrors.WithLabelValues(r)) - before[r]
				require.Equal(t, want, got, "counter delta for error_type %q", r)
			}

			if tc.errorType == "" {
				require.Empty(t, buf.String())
				return
			}

			var entry map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &entry), buf.String())
			require.Equal(t, slog.LevelWarn.String(), entry[slog.LevelKey])
			require.Equal(t, "test.example.com", entry[fields.HTTPHost])
			require.Equal(t, tc.errorType, entry[fields.ErrorType])
			require.Equal(t, remoteAddr.String(), entry[fields.RemoteAddr])
			if tc.errorMessage != "" {
				require.Equal(t, tc.errorMessage, entry[fields.ErrorMessage])
			} else {
				require.NotEmpty(t, entry[fields.ErrorMessage])
			}
		})
	}
}

func TestEnsureCertificate(t *testing.T) {
	tests := map[string]struct {
		domain   *domain.Domain
		wantCert *tls.Certificate
		wantErr  bool
	}{
		"nil domain": {
			domain:   nil,
			wantCert: nil,
			wantErr:  true,
		},
		"valid domain": {
			domain:   domain.New("test.example.com", string(cert), string(key), "", rootNamespaceID, nil),
			wantCert: &tls.Certificate{},
			wantErr:  false,
		},
		"invalid certificate": {
			domain:   domain.New("invalid.example.com", "invalid-cert", "invalid-key", "", rootNamespaceID, nil),
			wantCert: nil,
			wantErr:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			certResult, err := tc.domain.EnsureCertificate()
			if tc.wantErr {
				require.Error(t, err)
				require.Nil(t, certResult)
				return
			}
			require.NoError(t, err)
			if tc.wantCert == nil {
				require.Nil(t, certResult)
			} else {
				require.NotNil(t, certResult)
			}
		})
	}
}
