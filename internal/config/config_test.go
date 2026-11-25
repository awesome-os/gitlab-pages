package config

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/namsral/flag"
	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/fixture"
)

func TestAssertLogFields(t *testing.T) {
	cache := Cache{
		CacheExpiry:          600,
		CacheCleanupInterval: 60,
		EntryRefreshTimeout:  60,
		RetrievalTimeout:     30,
		MaxRetrievalInterval: 1,
		MaxRetrievalRetries:  3,
	}
	config := &Config{
		General: General{
			Domain:                     "gitlab-example.com",
			MaxConns:                   0,
			MaxURILength:               1024,
			RedirectHTTP:               false,
			RootDir:                    "shared/pages",
			ServerShutdownTimeout:      30,
			StatusPath:                 "gitlab/status",
			DisableCrossOriginRequests: false,
			InsecureCiphers:            false,
			PropagateCorrelationID:     true,
			ShowVersion:                false,
			NamespaceInPath:            false,
			SlowRequestThreshold:       500 * time.Millisecond,
		},
		RateLimit: RateLimit{
			SourceIPLimitPerSecond:    0.0,
			SourceIPBurst:             100,
			DomainLimitPerSecond:      0.0,
			DomainBurst:               100,
			TLSSourceIPLimitPerSecond: 0.0,
			TLSSourceIPBurst:          100,
			TLSDomainLimitPerSecond:   0.0,
			TLSDomainBurst:            100,
		},
		ArtifactsServer: ArtifactsServer{
			URL:            "https://user:password@gitlab.example/artifacts-server?token=artifact-secret",
			TimeoutSeconds: 10,
		},
		Authentication: Auth{
			Secret:               "Secret",
			ClientID:             "ID",
			ClientSecret:         "CSecret",
			RedirectURI:          "https://user:password@pages.example/auth?token=redirect-secret",
			Scope:                "api",
			Timeout:              5,
			CookieSessionTimeout: 600,
		},
		GitLab: GitLab{
			PublicServer:       "https://user:password@public.gitlab.com?token=public-secret",
			InternalServer:     "https://user:password@internal.gitlab.com?token=internal-secret",
			APISecretKey:       []byte("api-secret"),
			ClientHTTPTimeout:  60,
			JWTTokenExpiration: 30,
			Cache:              cache,
			EnableDisk:         true,
		},
		Log: Log{
			Format:  "JSON",
			Verbose: false,
		},
		Redirects: Redirects{
			MaxConfigSize:   1024,
			MaxPathSegments: 25,
			MaxRuleCount:    1000,
		},
		Sentry: Sentry{
			DSN:         "https://sentry-secret@sentry.example/path",
			Environment: "Sentry Environment",
		},
		TLS: TLS{
			MinVersion:        tls.VersionTLS12,
			MaxVersion:        tls.VersionTLS13,
			ClientCert:        "",
			ClientAuthDomains: tlsClientAuthDomains.value,
		},
		Zip: ZipServing{
			ExpirationInterval: 60,
			CleanupInterval:    30,
			RefreshInterval:    30,
			OpenTimeout:        30,
			AllowedPaths:       []string{*pagesRoot},
			HTTPClientTimeout:  30,
		},
		Server: Server{
			ReadTimeout:       5,
			ReadHeaderTimeout: 1,
			WriteTimeout:      0,
			ListenKeepAlive:   15,
		},
		ListenHTTPStrings:         MultiStringFlag{value: []string{"80", "8000"}, separator: ","},
		ListenHTTPSStrings:        MultiStringFlag{value: []string{"443", "8080"}, separator: ","},
		ListenProxyStrings:        MultiStringFlag{value: []string{"3010"}, separator: ","},
		ListenHTTPSProxyv2Strings: MultiStringFlag{value: []string{"5010"}, separator: ","},
	}
	cert := "/path/to/client.crt"
	key := "/path/to/client.key"
	pagesRootCert = &cert
	pagesRootKey = &key
	metricsCertificate = &cert
	metricsKey = &key
	clientCert = &cert
	clientKey = &key
	clientCACerts = MultiStringFlag{value: []string{cert}, separator: ","}
	fields := logFields(config)
	expectedFields := map[string]interface{}{
		"artifacts-server":               "https://gitlab.example/artifacts-server",
		"artifacts-server-timeout":       config.ArtifactsServer.TimeoutSeconds,
		"default-config-filename":        "config",
		"disable-cross-origin-requests":  config.General.DisableCrossOriginRequests,
		"domain":                         config.General.Domain,
		"insecure-ciphers":               config.General.InsecureCiphers,
		"listen-http":                    config.ListenHTTPStrings,
		"listen-https":                   config.ListenHTTPSStrings,
		"listen-proxy":                   config.ListenProxyStrings,
		"listen-https-proxyv2":           config.ListenHTTPSProxyv2Strings,
		"log-format":                     config.Log.Format,
		"log-verbose":                    config.Log.Verbose,
		"log-field-schema-version":       config.Log.FieldSchemaVersion,
		"log-field-dual-emit-target":     config.Log.FieldDualEmitTarget,
		"metrics-address":                config.Metrics.Address,
		"metrics-certificate":            cert,
		"metrics-key":                    key,
		"pages-domain":                   config.General.Domain,
		"pages-root":                     config.General.RootDir,
		"pages-status":                   config.General.StatusPath,
		"propagate-correlation-id":       config.General.PropagateCorrelationID,
		"redirect-http":                  config.General.RedirectHTTP,
		"root-cert":                      cert,
		"root-key":                       key,
		"status_path":                    config.General.StatusPath,
		"tls-min-version":                config.TLS.MinVersion,
		"tls-max-version":                config.TLS.MaxVersion,
		"tls-client-auth":                config.TLS.ClientAuth,
		"tls-client-cert":                config.TLS.ClientCert,
		"tls-client-auth-domains":        config.TLS.ClientAuthDomains,
		"gitlab-server":                  "https://public.gitlab.com",
		"internal-gitlab-server":         "https://internal.gitlab.com",
		"api-secret-key-configured":      true,
		"enable-disk":                    config.GitLab.EnableDisk,
		"auth-redirect-uri":              "https://pages.example/auth",
		"auth-scope":                     config.Authentication.Scope,
		"auth-cookie-session-timeout":    config.Authentication.CookieSessionTimeout,
		"auth-timeout":                   config.Authentication.Timeout,
		"max-conns":                      config.General.MaxConns,
		"max-uri-length":                 config.General.MaxURILength,
		"zip-cache-expiration":           config.Zip.ExpirationInterval,
		"zip-cache-cleanup":              config.Zip.CleanupInterval,
		"zip-cache-refresh":              config.Zip.RefreshInterval,
		"zip-open-timeout":               config.Zip.OpenTimeout,
		"zip-http-client-timeout":        config.Zip.HTTPClientTimeout,
		"rate-limit-source-ip":           config.RateLimit.SourceIPLimitPerSecond,
		"rate-limit-source-ip-burst":     config.RateLimit.SourceIPBurst,
		"rate-limit-domain":              config.RateLimit.DomainLimitPerSecond,
		"rate-limit-domain-burst":        config.RateLimit.DomainBurst,
		"rate-limit-tls-source-ip":       config.RateLimit.TLSSourceIPLimitPerSecond,
		"rate-limit-tls-source-ip-burst": config.RateLimit.TLSSourceIPBurst,
		"rate-limit-tls-domain":          config.RateLimit.TLSDomainLimitPerSecond,
		"rate-limit-tls-domain-burst":    config.RateLimit.TLSDomainBurst,
		"rate-limit-subnets-allow-list":  config.RateLimit.RateLimitBypassCIDRs,
		"gitlab-client-http-timeout":     config.GitLab.ClientHTTPTimeout,
		"gitlab-client-jwt-expiry":       config.GitLab.JWTTokenExpiration,
		"gitlab-cache-expiry":            config.GitLab.Cache.CacheExpiry,
		"gitlab-cache-refresh":           config.GitLab.Cache.CacheCleanupInterval,
		"gitlab-cache-cleanup":           config.GitLab.Cache.EntryRefreshTimeout,
		"gitlab-retrieval-timeout":       config.GitLab.Cache.RetrievalTimeout,
		"gitlab-retrieval-interval":      config.GitLab.Cache.MaxRetrievalInterval,
		"gitlab-retrieval-retries":       config.GitLab.Cache.MaxRetrievalRetries,
		"redirects-max-config-size":      config.Redirects.MaxConfigSize,
		"redirects-max-path-segments":    config.Redirects.MaxPathSegments,
		"redirects-max-rule-count":       config.Redirects.MaxRuleCount,
		"server-read-timeout":            config.Server.ReadTimeout,
		"server-read-header-timeout":     config.Server.ReadHeaderTimeout,
		"server-write-timeout":           config.Server.WriteTimeout,
		"server-keep-alive":              config.Server.ListenKeepAlive,
		"server-shutdown-timeout":        config.General.ServerShutdownTimeout,
		"sentry-enabled":                 true,
		"sentry-environment":             config.Sentry.Environment,
		"version":                        config.General.ShowVersion,
		"namespace-in-path":              config.General.NamespaceInPath,
		"client-cert":                    cert,
		"client-key":                     key,
		"client-ca-certs":                config.GitLab.ClientCfg.CAFiles,
		"slow-requests-threshold":        config.General.SlowRequestThreshold,
	}
	for fieldName := range fields {
		require.Equal(t, expectedFields[fieldName], fields[fieldName])
	}
	require.NotContains(t, fields, "api-secret-key")
	require.NotContains(t, fields, "sentry-dsn")
}

func TestLogFields(t *testing.T) {
	loggingFlags := logFields(&Config{})

	var missingFlags []string

	flag.VisitAll(func(f *flag.Flag) {
		_, logging := loggingFlags[f.Name]

		if nonLoggableFlags[f.Name] || logging {
			return
		}

		missingFlags = append(missingFlags, f.Name)
	})

	require.Empty(
		t,
		missingFlags,
		"New flag is added, but not logged. Consider adding it to nonLoggableFlags if it contains any sensitive data such as keys",
	)
}

func Test_loadMetricsConfig(t *testing.T) {
	defaultMetricsAdress := ":9325"
	defaultDir, defaultMetricsKey, defaultMetricsCertificate := setupHTTPSFixture(t)

	tests := map[string]struct {
		metricsAddress     string
		metricsCertificate string
		metricsKey         string
		expectedError      error
	}{
		"no metrics": {},
		"http metrics": {
			metricsAddress: defaultMetricsAdress,
		},
		"https metrics": {
			metricsAddress:     defaultMetricsAdress,
			metricsCertificate: defaultMetricsCertificate,
			metricsKey:         defaultMetricsKey,
		},
		"https metrics no certificate": {
			metricsAddress: defaultMetricsAdress,
			metricsKey:     defaultMetricsKey,
			expectedError:  errMetricsNoCertificate,
		},
		"https metrics no key": {
			metricsAddress:     defaultMetricsAdress,
			metricsCertificate: defaultMetricsCertificate,
			expectedError:      errMetricsNoKey,
		},
		"https metrics invalid certificate path": {
			metricsAddress:     defaultMetricsAdress,
			metricsCertificate: filepath.Join(defaultDir, "domain.certificate.missing"),
			metricsKey:         defaultMetricsKey,
			expectedError:      os.ErrNotExist,
		},
		"https metrics invalid key path": {
			metricsAddress:     defaultMetricsAdress,
			metricsCertificate: defaultMetricsCertificate,
			metricsKey:         filepath.Join(defaultDir, "domain.key.missing"),
			expectedError:      os.ErrNotExist,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			metricsAddress = &tc.metricsAddress
			metricsCertificate = &tc.metricsCertificate
			metricsKey = &tc.metricsKey
			_, err := loadMetricsConfig()
			require.ErrorIs(t, err, tc.expectedError)
		})
	}
}

func setupHTTPSFixture(t *testing.T) (dir string, key string, cert string) {
	t.Helper()

	tmpDir := t.TempDir()

	keyfile, err := os.CreateTemp(tmpDir, "https-fixture")
	require.NoError(t, err)
	key = keyfile.Name()
	keyfile.Close()

	certfile, err := os.CreateTemp(tmpDir, "https-fixture")
	require.NoError(t, err)
	cert = certfile.Name()
	certfile.Close()

	require.NoError(t, os.WriteFile(key, []byte(fixture.Key), 0644))
	require.NoError(t, os.WriteFile(cert, []byte(fixture.Certificate), 0644))

	return tmpDir, keyfile.Name(), certfile.Name()
}

func TestParseClientAuthType(t *testing.T) {
	tests := []struct {
		name       string
		clientAuth string
		valid      bool
		expected   tls.ClientAuthType
	}{
		{
			name:       "empty string",
			clientAuth: "",
			valid:      true,
			expected:   tls.NoClientCert,
		},
		{
			name:       "unknown value",
			clientAuth: "no cert",
			valid:      false,
			expected:   -1,
		},
		{
			name:       "explicitly no cert",
			clientAuth: "NoClientCert",
			valid:      true,
			expected:   tls.NoClientCert,
		},
		{
			name:       "request cert",
			clientAuth: "RequestClientCert",
			valid:      true,
			expected:   tls.RequestClientCert,
		},
		{
			name:       "require any cert",
			clientAuth: "RequireAnyClientCert",
			valid:      true,
			expected:   tls.RequireAnyClientCert,
		},
		{
			name:       "verify cert if given",
			clientAuth: "VerifyClientCertIfGiven",
			valid:      true,
			expected:   tls.VerifyClientCertIfGiven,
		},
		{
			name:       "require and verify cert",
			clientAuth: "RequireAndVerifyClientCert",
			valid:      true,
			expected:   tls.RequireAndVerifyClientCert,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authType, err := parseClientAuthType(tt.clientAuth)
			if tt.valid {
				require.NoError(t, err)
				require.EqualValues(t, tt.expected, authType)
				return
			}
			require.Error(t, err)
		})
	}
}

func TestParseHeaderString(t *testing.T) {
	tests := []struct {
		name          string
		headerStrings []string
		valid         bool
		expectedLen   int
	}{
		{
			name:          "Normal case",
			headerStrings: []string{"X-Test-String: Test"},
			valid:         true,
			expectedLen:   1,
		},
		{
			name:          "Non-tracking header case",
			headerStrings: []string{"Tk: N"},
			valid:         true,
			expectedLen:   1,
		},
		{
			name:          "Content security header case",
			headerStrings: []string{"content-security-policy: default-src 'self'"},
			valid:         true,
			expectedLen:   1,
		},
		{
			name:          "Multiple header strings",
			headerStrings: []string{"content-security-policy: default-src 'self'", "X-Test-String: Test", "My amazing header : Amazing"},
			valid:         true,
			expectedLen:   3,
		},
		{
			name:          "Multiple invalid cases",
			headerStrings: []string{"content-security-policy: default-src 'self'", "test-case"},
			valid:         false,
		},
		{
			name:          "Not valid case",
			headerStrings: []string{"Tk= N"},
			valid:         false,
		},
		{
			name:          "duplicate headers",
			headerStrings: []string{"Tk: N", "Tk: M"},
			valid:         false,
		},
		{
			name:          "Not valid case",
			headerStrings: []string{"X-Test-String Some-Test"},
			valid:         false,
		},
		{
			name:          "Valid and not valid case",
			headerStrings: []string{"content-security-policy: default-src 'self'", "test-case"},
			valid:         false,
		},
		{
			name:          "Multiple headers in single string parsed as one header",
			headerStrings: []string{"content-security-policy: default-src 'self',X-Test-String: Test,My amazing header : Amazing"},
			valid:         true,
			expectedLen:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHeaderString(tt.headerStrings)
			if tt.valid {
				require.NoError(t, err)
				require.Len(t, got, tt.expectedLen)
				return
			}

			require.Error(t, err)
		})
	}
}
