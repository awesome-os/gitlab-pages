package config

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/textproto"
	"os"
	"strings"
	"time"

	"github.com/3th1nk/cidr"
	"github.com/hashicorp/go-multierror"

	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

// Config stores all the config options relevant to GitLab Pages.
type Config struct {
	General         General
	RateLimit       RateLimit
	ArtifactsServer ArtifactsServer
	Authentication  Auth
	GitLab          GitLab
	Log             Log
	Redirects       Redirects
	Sentry          Sentry
	Server          Server
	TLS             TLS
	Zip             ZipServing
	Metrics         Metrics

	// These fields contain the raw strings passed for listen-http,
	// listen-https, listen-proxy and listen-https-proxyv2 settings. It is used
	// by appmain() to create listeners.
	ListenHTTPStrings         MultiStringFlag
	ListenHTTPSStrings        MultiStringFlag
	ListenProxyStrings        MultiStringFlag
	ListenHTTPSProxyv2Strings MultiStringFlag
}

// General groups settings that are general to GitLab Pages and can not
// be categorized under other head.
type General struct {
	Domain                string
	MaxConns              int
	MaxURILength          int
	RedirectHTTP          bool
	RootCertificate       []byte
	RootDir               string
	RootKey               []byte
	ServerShutdownTimeout time.Duration
	StatusPath            string

	DisableCrossOriginRequests bool
	InsecureCiphers            bool
	PropagateCorrelationID     bool

	ShowVersion bool

	CustomHeaders http.Header

	NamespaceInPath bool

	SlowRequestThreshold time.Duration
}

// RateLimit config struct
type RateLimit struct {
	// HTTP limits
	SourceIPLimitPerSecond float64
	SourceIPBurst          int
	DomainLimitPerSecond   float64
	DomainBurst            int

	// TLS connections limits
	TLSSourceIPLimitPerSecond float64
	TLSSourceIPBurst          int
	TLSDomainLimitPerSecond   float64
	TLSDomainBurst            int

	// Bypass CIDRs
	RateLimitBypassCIDRs []cidr.CIDR
}

// ArtifactsServer groups settings related to configuring Artifacts
// server
type ArtifactsServer struct {
	URL            string
	TimeoutSeconds int
}

// Auth groups settings related to configuring Authentication with
// GitLab
type Auth struct {
	Secret               string
	ClientID             string
	ClientSecret         string
	RedirectURI          string
	Scope                string
	Timeout              time.Duration
	CookieSessionTimeout time.Duration
}

// Cache configuration for GitLab API
type Cache struct {
	CacheExpiry          time.Duration
	CacheCleanupInterval time.Duration
	EntryRefreshTimeout  time.Duration
	RetrievalTimeout     time.Duration
	MaxRetrievalInterval time.Duration
	MaxRetrievalRetries  int
}

// GitLab groups settings related to configuring GitLab client used to
// interact with GitLab API
type GitLab struct {
	PublicServer       string
	InternalServer     string
	APISecretKey       []byte
	ClientHTTPTimeout  time.Duration
	JWTTokenExpiration time.Duration
	Cache              Cache
	EnableDisk         bool
	ClientCfg          HTTPClientCfg
}

type HTTPClientCfg struct {
	CAFiles    []string
	Cert       *tls.Certificate
	MinVersion uint16
	MaxVersion uint16
}

// Log groups settings related to configuring logging
type Log struct {
	Format  string
	Verbose bool

	// FieldSchemaVersion mirrors the GitLab admin setting
	// `logging_field_schema_version`. 0 means current field names.
	FieldSchemaVersion int

	// FieldDualEmitTarget mirrors the GitLab admin setting
	// `logging_field_dual_emit_target`. 0 disables dual emission; when set it
	// must be strictly greater than FieldSchemaVersion.
	FieldDualEmitTarget int
}

// Redirects groups settings related to configuring _redirects limits
type Redirects struct {
	MaxConfigSize   int
	MaxPathSegments int
	MaxRuleCount    int
}

// Sentry groups settings related to configuring Sentry
type Sentry struct {
	DSN         string
	Environment string
}

// TLS groups settings related to configuring TLS
type TLS struct {
	MinVersion        uint16
	MaxVersion        uint16
	ClientAuth        tls.ClientAuthType
	ClientCert        string
	ClientAuthDomains []string
}

// ZipServing groups settings to be used by the zip VFS opening and caching
type ZipServing struct {
	ExpirationInterval time.Duration
	CleanupInterval    time.Duration
	RefreshInterval    time.Duration
	OpenTimeout        time.Duration
	AllowedPaths       []string
	HTTPClientTimeout  time.Duration
}

type Server struct {
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	ListenKeepAlive   time.Duration
}

type Metrics struct {
	Address   string
	TLSConfig *tls.Config
}

var (
	errDuplicateHeader        = errors.New("duplicate header")
	errInvalidHeaderParameter = errors.New("invalid syntax specified as header parameter")
	errMetricsNoCertificate   = errors.New("metrics certificate path must not be empty")
	errMetricsNoKey           = errors.New("metrics private key path must not be empty")
)

func internalGitlabServerFromFlags() string {
	if *internalGitLabServer != "" {
		return *internalGitLabServer
	}

	return *publicGitLabServer
}

func artifactsServerFromFlags() string {
	if *artifactsServer != "" {
		return *artifactsServer
	}

	return internalGitlabServerFromFlags() + "/api/v4"
}

func setGitLabAPISecretKey(secretFile string, config *Config) error {
	if secretFile == "" {
		return nil
	}

	encoded, err := os.ReadFile(secretFile)
	if err != nil {
		return fmt.Errorf("reading secret file: %w", err)
	}

	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	secretLength, err := base64.StdEncoding.Decode(decoded, encoded)
	if err != nil {
		return fmt.Errorf("decoding GitLab API secret: %w", err)
	}

	if secretLength != 32 {
		return fmt.Errorf("expected 32 bytes GitLab API secret but got %d bytes", secretLength)
	}

	config.GitLab.APISecretKey = decoded
	return nil
}

func loadMetricsConfig() (metrics Metrics, err error) {
	// don't validate anything if metrics are disabled
	if *metricsAddress == "" {
		return metrics, nil
	}
	metrics.Address = *metricsAddress

	// no error when using HTTP
	if *metricsCertificate == "" && *metricsKey == "" {
		return metrics, nil
	}

	if *metricsCertificate == "" {
		return metrics, errMetricsNoCertificate
	}

	if *metricsKey == "" {
		return metrics, errMetricsNoKey
	}

	cert, err := tls.LoadX509KeyPair(*metricsCertificate, *metricsKey)
	if err != nil {
		return metrics, err
	}

	metrics.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	return metrics, nil
}

// parseClientAuthType converts the tls.ClientAuthType enum from names
// to the underlying value. Passing an empty string assumes
// tls.NoClientCert
func parseClientAuthType(clientAuth string) (tls.ClientAuthType, error) {
	switch strings.ToLower(clientAuth) {
	// if nothing is provided, assume that
	// the user does not want to enable any form
	// of client authentication
	case "":
		fallthrough
	case "noclientcert":
		return tls.NoClientCert, nil
	case "requestclientcert":
		return tls.RequestClientCert, nil
	case "requireanyclientcert":
		return tls.RequireAnyClientCert, nil
	case "verifyclientcertifgiven":
		return tls.VerifyClientCertIfGiven, nil
	case "requireandverifyclientcert":
		return tls.RequireAndVerifyClientCert, nil
	default:
		return -1, fmt.Errorf("unknown client auth type %s: supported values can be found at https://pkg.go.dev/crypto/tls#ClientAuthType", clientAuth)
	}
}

func parseHeaderString(customHeaders []string) (http.Header, error) {
	headers := make(http.Header, len(customHeaders))

	var result *multierror.Error
	for _, h := range customHeaders {
		h = h + "\n\n"
		tp := textproto.NewReader(bufio.NewReader(strings.NewReader(h)))

		mimeHeader, err := tp.ReadMIMEHeader()
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("parsing error %s: %w", h, errInvalidHeaderParameter))
		}

		for key, value := range mimeHeader {
			if _, ok := headers[key]; ok {
				result = multierror.Append(result, fmt.Errorf("%s already specified with value '%s': %w", key, value, errDuplicateHeader))
			}

			headers[key] = value
		}
	}

	if result.ErrorOrNil() != nil {
		return nil, result
	}

	return headers, nil
}

func loadConfig() (*Config, error) {
	config := &Config{
		General: General{
			Domain:                     strings.ToLower(*pagesDomain),
			MaxConns:                   *maxConns,
			MaxURILength:               *maxURILength,
			RedirectHTTP:               *redirectHTTP,
			RootDir:                    *pagesRoot,
			StatusPath:                 *pagesStatus,
			ServerShutdownTimeout:      *serverShutdownTimeout,
			DisableCrossOriginRequests: *disableCrossOriginRequests,
			InsecureCiphers:            *insecureCiphers,
			PropagateCorrelationID:     *propagateCorrelationID,
			ShowVersion:                *showVersion,
			NamespaceInPath:            *namespaceInPath,
		},
		RateLimit: RateLimit{
			SourceIPLimitPerSecond: *rateLimitSourceIP,
			SourceIPBurst:          *rateLimitSourceIPBurst,
			DomainLimitPerSecond:   *rateLimitDomain,
			DomainBurst:            *rateLimitDomainBurst,

			TLSSourceIPLimitPerSecond: *rateLimitTLSSourceIP,
			TLSSourceIPBurst:          *rateLimitTLSSourceIPBurst,
			TLSDomainLimitPerSecond:   *rateLimitTLSDomain,
			TLSDomainBurst:            *rateLimitTLSDomainBurst,
		},
		GitLab: GitLab{
			ClientHTTPTimeout:  *gitlabClientHTTPTimeout,
			JWTTokenExpiration: *gitlabClientJWTExpiry,
			EnableDisk:         *enableDisk,
			Cache: Cache{
				CacheExpiry:          *gitlabCacheExpiry,
				CacheCleanupInterval: *gitlabCacheCleanup,
				EntryRefreshTimeout:  *gitlabCacheRefresh,
				RetrievalTimeout:     *gitlabRetrievalTimeout,
				MaxRetrievalInterval: *gitlabRetrievalInterval,
				MaxRetrievalRetries:  *gitlabRetrievalRetries,
			},
		},
		ArtifactsServer: ArtifactsServer{
			TimeoutSeconds: *artifactsServerTimeout,
			URL:            *artifactsServer,
		},
		Authentication: Auth{
			Secret:               *secret,
			ClientID:             *clientID,
			ClientSecret:         *clientSecret,
			RedirectURI:          *redirectURI,
			Scope:                *authScope,
			Timeout:              *authTimeout,
			CookieSessionTimeout: *authCookieSessionTimeout,
		},
		Log: Log{
			Format:              *logFormat,
			Verbose:             *logVerbose,
			FieldSchemaVersion:  *logFieldSchemaVersion,
			FieldDualEmitTarget: *logFieldDualEmitTarget,
		},
		Redirects: Redirects{
			MaxConfigSize:   *redirectsMaxConfigSize,
			MaxPathSegments: *redirectsMaxPathSegments,
			MaxRuleCount:    *redirectsMaxRuleCount,
		},
		Sentry: Sentry{
			DSN:         *sentryDSN,
			Environment: *sentryEnvironment,
		},
		TLS: TLS{
			MinVersion:        allTLSVersions[*tlsMinVersion],
			MaxVersion:        allTLSVersions[*tlsMaxVersion],
			ClientCert:        *tlsClientCert,
			ClientAuthDomains: tlsClientAuthDomains.value,
		},
		Zip: ZipServing{
			ExpirationInterval: *zipCacheExpiration,
			CleanupInterval:    *zipCacheCleanup,
			RefreshInterval:    *zipCacheRefresh,
			OpenTimeout:        *zipOpenTimeout,
			AllowedPaths:       []string{*pagesRoot},
			HTTPClientTimeout:  *zipHTTPClientTimeout,
		},
		Server: Server{
			ReadTimeout:       *serverReadTimeout,
			ReadHeaderTimeout: *serverReadHeaderTimeout,
			WriteTimeout:      *serverWriteTimeout,
			ListenKeepAlive:   *serverKeepAlive,
		},

		// Actual listener pointers will be populated in appMain. We populate the
		// raw strings here so that they are available in appMain
		ListenHTTPStrings:         listenHTTP,
		ListenHTTPSStrings:        listenHTTPS,
		ListenProxyStrings:        listenProxy,
		ListenHTTPSProxyv2Strings: listenHTTPSProxyv2,
	}

	var err error

	// Validating and populating Metrics config
	if config.Metrics, err = loadMetricsConfig(); err != nil {
		return nil, err
	}

	// Populating remaining General settings
	for _, file := range []struct {
		contents *[]byte
		path     string
	}{
		{&config.General.RootCertificate, *pagesRootCert},
		{&config.General.RootKey, *pagesRootKey},
	} {
		if file.path != "" {
			if *file.contents, err = os.ReadFile(file.path); err != nil {
				return nil, err
			}
		}
	}

	cert, err := loadClientCert(*clientCert, *clientKey)
	if err != nil {
		return nil, err
	}

	config.GitLab.ClientCfg = HTTPClientCfg{
		Cert:       cert,
		CAFiles:    clientCACerts.Split(),
		MinVersion: allTLSVersions[*tlsMinVersion],
		MaxVersion: allTLSVersions[*tlsMaxVersion],
	}

	customHeaders, err := parseHeaderString(header.Split())
	if err != nil {
		return nil, fmt.Errorf("unable to parse header string: %w", err)
	}

	clientAuthType, err := parseClientAuthType(*tlsClientAuth)
	if err != nil {
		return nil, err
	}
	config.TLS.ClientAuth = clientAuthType

	config.General.CustomHeaders = customHeaders

	// Populating remaining GitLab settings
	config.GitLab.PublicServer = *publicGitLabServer

	config.GitLab.InternalServer = internalGitlabServerFromFlags()

	config.ArtifactsServer.URL = artifactsServerFromFlags()

	if err = setGitLabAPISecretKey(*gitLabAPISecretKey, config); err != nil {
		return nil, err
	}

	bypassCIDRs, err := parseCIDRs(rateLimitBypassCIDRs.Split())
	if err != nil {
		return nil, err
	}
	config.RateLimit.RateLimitBypassCIDRs = bypassCIDRs

	// Set slow request threshold from flag (convert ms to duration)
	config.General.SlowRequestThreshold = *slowRequestThreshold

	return config, nil
}

func loadClientCert(cert, key string) (*tls.Certificate, error) {
	if len(cert) > 0 && len(key) > 0 {
		tlsCert, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		return &tlsCert, nil
	}

	return nil, nil
}

func parseCIDRs(cidrStrs []string) ([]cidr.CIDR, error) {
	var cidrs []cidr.CIDR
	for _, cidrStr := range cidrStrs {
		c, err := cidr.Parse(cidrStr)
		if err != nil {
			return nil, fmt.Errorf("unable to parse rate-limit-subnets-allow-list, subnet: %s: %w", cidrStr, err)
		}
		cidrs = append(cidrs, *c)
	}
	return cidrs, nil
}

func logFields(config *Config) map[string]any {
	return map[string]any{
		"artifacts-server":               logging.CleanURL(config.ArtifactsServer.URL),
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
		"metrics-certificate":            *metricsCertificate,
		"metrics-key":                    *metricsKey,
		"pages-domain":                   config.General.Domain,
		"pages-root":                     config.General.RootDir,
		"pages-status":                   config.General.StatusPath,
		"propagate-correlation-id":       config.General.PropagateCorrelationID,
		"redirect-http":                  config.General.RedirectHTTP,
		"root-cert":                      *pagesRootCert,
		"root-key":                       *pagesRootKey,
		"status_path":                    config.General.StatusPath,
		"tls-min-version":                config.TLS.MinVersion,
		"tls-max-version":                config.TLS.MaxVersion,
		"tls-client-auth":                config.TLS.ClientAuth,
		"tls-client-cert":                config.TLS.ClientCert,
		"tls-client-auth-domains":        config.TLS.ClientAuthDomains,
		"gitlab-server":                  logging.CleanURL(config.GitLab.PublicServer),
		"internal-gitlab-server":         logging.CleanURL(config.GitLab.InternalServer),
		"api-secret-key-configured":      len(config.GitLab.APISecretKey) > 0,
		"enable-disk":                    config.GitLab.EnableDisk,
		"auth-redirect-uri":              logging.CleanURL(config.Authentication.RedirectURI),
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
		"sentry-enabled":                 config.Sentry.DSN != "",
		"sentry-environment":             config.Sentry.Environment,
		"version":                        config.General.ShowVersion,
		"namespace-in-path":              config.General.NamespaceInPath,
		"client-cert":                    *clientCert,
		"client-key":                     *clientKey,
		"client-ca-certs":                config.GitLab.ClientCfg.CAFiles,
		"slow-requests-threshold":        *slowRequestThreshold,
	}
}

func LogConfig(config *Config) {
	fields := logFields(config)
	attrs := make([]slog.Attr, 0, len(fields))
	for k, v := range fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	logging.Logger.LogAttrs(context.Background(), slog.LevelDebug, "Start Pages with configuration", attrs...)
}

// LoadConfig parses configuration settings passed as command line arguments or
// via config file, and populates a Config object with those values
func LoadConfig() (*Config, error) {
	initFlags()

	return loadConfig()
}
