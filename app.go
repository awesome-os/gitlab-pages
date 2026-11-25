package main

import (
	"context"
	cryptotls "crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	ghandlers "github.com/gorilla/handlers"
	"github.com/hashicorp/go-multierror"
	"github.com/rs/cors"
	"gitlab.com/gitlab-org/go-mimedb"
	"gitlab.com/gitlab-org/labkit/correlation"
	labmetrics "gitlab.com/gitlab-org/labkit/metrics"
	"gitlab.com/gitlab-org/labkit/monitoring"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"
	"golang.org/x/sync/errgroup"

	"gitlab.com/gitlab-org/gitlab-pages/internal/artifact"
	"gitlab.com/gitlab-org/gitlab-pages/internal/auth"
	cfg "gitlab.com/gitlab-org/gitlab-pages/internal/config"
	"gitlab.com/gitlab-org/gitlab-pages/internal/customheaders"
	"gitlab.com/gitlab-org/gitlab-pages/internal/domain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/errortracking"
	"gitlab.com/gitlab-org/gitlab-pages/internal/handlers"
	health "gitlab.com/gitlab-org/gitlab-pages/internal/healthcheck"
	"gitlab.com/gitlab-org/gitlab-pages/internal/httperrors"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging/slowlogs"
	"gitlab.com/gitlab-org/gitlab-pages/internal/namespaceinpath"
	"gitlab.com/gitlab-org/gitlab-pages/internal/netutil"
	"gitlab.com/gitlab-org/gitlab-pages/internal/primarydomain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/redirects"
	"gitlab.com/gitlab-org/gitlab-pages/internal/rejectmethods"
	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
	"gitlab.com/gitlab-org/gitlab-pages/internal/routing"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving/disk/zip"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source"
	"gitlab.com/gitlab-org/gitlab-pages/internal/source/gitlab"
	"gitlab.com/gitlab-org/gitlab-pages/internal/tls"
	"gitlab.com/gitlab-org/gitlab-pages/internal/uniquedomain"
	"gitlab.com/gitlab-org/gitlab-pages/internal/urilimiter"
	"gitlab.com/gitlab-org/gitlab-pages/metrics"
)

var (
	corsHandler = cors.New(cors.Options{
		AllowedMethods:       []string{http.MethodGet, http.MethodHead},
		OptionsSuccessStatus: http.StatusOK,
	})

	// Define a package-level error variable for TLS configuration retrieval failure
	errUnableToRetrieveTLSConfig = errors.New("unable to retrieve tls config")
)

type theApp struct {
	config    *cfg.Config
	source    source.Source
	tlsConfig *cryptotls.Config
	Artifact  *artifact.Artifact
	Auth      *auth.Auth
	Handlers  *handlers.Handlers
}

// GetConfigForClient is a placeholder implementation for theApp.
func (a *theApp) GetConfigForClient(info *cryptotls.ClientHelloInfo) (*cryptotls.Config, error) {
	return nil, nil
}

func (a *theApp) getTLSConfig(ctx context.Context) *cryptotls.Config {
	logger := logv2.FromContext(ctx)
	var err error
	a.tlsConfig, err = tls.GetTLSConfig(a.config, a.source.GetDomain, a.GetConfigForClient)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get TLS config", logv2.Error(err))
		return nil
	}
	if a.tlsConfig == nil {
		logger.ErrorContext(ctx, "Failed to get TLS config", logv2.Error(errUnableToRetrieveTLSConfig))
		return nil
	}
	return a.tlsConfig
}

// serveFileOrNotFoundHandler will serve static content or
// return a 404 Not Found response
func (a *theApp) serveFileOrNotFoundHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			metrics.ServingTime.Observe(time.Since(start).Seconds())
		}()

		domain := domain.FromRequest(r)
		fileServed := domain.ServeFileHTTP(w, r)

		if !fileServed {
			// We need to trigger authentication flow here if file does not exist to prevent exposing possibly private project existence,
			// because the projects override the paths of the namespace project and they might be private even though
			// namespace project is public
			if domain.IsNamespaceProject(r) {
				if a.Auth.CheckAuthenticationWithoutProject(w, r, domain) {
					return
				}
			}

			// domain found and authentication succeeds
			domain.ServeNotFoundHTTP(w, r)
		}
	})
}

// httpInitialMiddleware sets up HTTP requests
func (a *theApp) httpInitialMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, setRequestScheme(r))
	})
}

// setRequestScheme will update r.URL.Scheme if empty based on r.TLS
func setRequestScheme(r *http.Request) *http.Request {
	if r.URL.Scheme == request.SchemeHTTPS || r.TLS != nil {
		// make sure is set for non-proxy requests
		r.URL.Scheme = request.SchemeHTTPS
	} else {
		r.URL.Scheme = request.SchemeHTTP
	}

	return r
}

// TODO: move the pipeline configuration to internal/pipeline https://gitlab.com/gitlab-org/gitlab-pages/-/issues/670
func (a *theApp) buildHandlerPipeline() http.Handler {
	// Handlers should be applied in a reverse order
	handler := slowlogs.LogHandlerTiming(a.serveFileOrNotFoundHandler(), "serveFileOrNotFound")
	handler = slowlogs.LogHandlerTiming(uniquedomain.NewMiddleware(handler), "UniqueDomain")
	handler = slowlogs.LogHandlerTiming(primarydomain.NewMiddleware(handler), "PrimaryDomain")
	handler = slowlogs.LogHandlerTiming(a.Auth.AuthorizationMiddleware(handler), "Authorization")

	handler = slowlogs.LogHandlerTiming(handlers.ArtifactMiddleware(handler, a.Handlers), "Artifacts")
	handler = slowlogs.LogHandlerTiming(a.Auth.AuthenticationMiddleware(handler, a.source), "Authentication")
	handler = slowlogs.LogHandlerTiming(routing.NewMiddleware(handler, a.source), "Routing")
	handler = slowlogs.LogHandlerTiming(handlers.AcmeMiddleware(handler, a.source, a.config.GitLab.PublicServer), "Acme")

	if !a.config.General.DisableCrossOriginRequests {
		handler = slowlogs.LogHandlerTiming(corsHandler.Handler(handler), "cors")
	}

	// Add auto redirect
	handler = slowlogs.LogHandlerTiming(handlers.HTTPSRedirectMiddleware(handler, a.config.General.RedirectHTTP), "HTTPSRedirect")

	handler = slowlogs.LogHandlerTiming(handlers.NewRateLimiterHandler(context.Background(), handler, &a.config.RateLimit), "RateLimiter")

	// Health Check
	handler = slowlogs.LogHandlerTiming(health.NewMiddleware(handler, a.config.General.StatusPath), "HealthCheck")

	// Custom response headers
	handler = slowlogs.LogHandlerTiming(customheaders.NewMiddleware(handler, a.config.General.CustomHeaders), "CustomHeaders")

	// Access logs and metrics
	handler = logging.BasicAccessLogger(handler, a.config.Log.Format)
	metricsMiddleware := labmetrics.NewHandlerFactory(labmetrics.WithNamespace("gitlab_pages"))
	handler = slowlogs.LogHandlerTiming(metricsMiddleware(handler), "Metrics")

	handler = slowlogs.NewMiddleware(handler, logging.LogRequest, a.config.General.SlowRequestThreshold, logging.Logger)

	// Correlation ID injection middleware
	var correlationOpts []correlation.InboundHandlerOption
	correlationOpts = append(correlationOpts, correlation.WithSetResponseHeader())
	if a.config.General.PropagateCorrelationID {
		correlationOpts = append(correlationOpts, correlation.WithPropagation())
	}

	handler = correlation.InjectCorrelationID(handler, correlationOpts...)

	handler = handlePanicMiddleware(handler)

	// These middlewares MUST be added in the end.
	// Being last means they will be evaluated first
	// preventing any operation on bogus requests.
	handler = urilimiter.NewMiddleware(handler, a.config.General.MaxURILength)

	if a.config.General.NamespaceInPath {
		handler = namespaceinpath.NewMiddleware(handler, a.config.General.Domain, a.config.Authentication.RedirectURI, a.config.General.StatusPath)
	}

	handler = logging.WithLoggerMiddleware(handler)
	handler = rejectmethods.NewMiddleware(handler)

	return handler
}

// nolint: gocyclo // ignore this
func (a *theApp) Run() error {
	var limiter *netutil.Limiter
	if a.config.General.MaxConns > 0 {
		limiter = netutil.NewLimiterWithMetrics(
			a.config.General.MaxConns,
			metrics.LimitListenerMaxConns,
			metrics.LimitListenerConcurrentConns,
			metrics.LimitListenerWaitingConns,
		)
	}

	// Use a common pipeline to use a single instance of each handler,
	// instead of making two nearly identical pipelines
	commonHandlerPipeline := a.buildHandlerPipeline()

	proxyHandler := ghandlers.ProxyHeaders(commonHandlerPipeline)

	httpHandler := a.httpInitialMiddleware(commonHandlerPipeline)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx = logv2.WithLogger(ctx, logging.Logger)

	eg, ctx := errgroup.WithContext(ctx)
	var servers []*http.Server

	// Listen for HTTP
	for _, addr := range a.config.ListenHTTPStrings.Split() {
		s := a.listen(
			eg,
			addr,
			httpHandler,
			errortracking.WithField("listener", request.SchemeHTTP),
			withLimiter(limiter),
		)
		servers = append(servers, s)
	}

	// Listen for HTTPS
	for _, addr := range a.config.ListenHTTPSStrings.Split() {
		tlsConfig := a.getTLSConfig(ctx)
		if tlsConfig == nil {
			return fmt.Errorf("unable to retrieve tls config")
		}

		s := a.listen(
			eg,
			addr,
			httpHandler,
			errortracking.WithField("listener", request.SchemeHTTPS),
			withLimiter(limiter),
			withTLSConfig(tlsConfig),
		)
		servers = append(servers, s)
	}

	// Listen for HTTP proxy requests
	for _, addr := range a.config.ListenProxyStrings.Split() {
		s := a.listen(
			eg,
			addr,
			proxyHandler,
			errortracking.WithField("listener", "http proxy"),
			withLimiter(limiter),
		)
		servers = append(servers, s)
	}

	// Listen for HTTPS PROXYv2 requests
	for _, addr := range a.config.ListenHTTPSProxyv2Strings.Split() {
		tlsConfig := a.getTLSConfig(ctx)
		if tlsConfig == nil {
			return fmt.Errorf("unable to retrieve tls config")
		}

		s := a.listen(
			eg,
			addr,
			httpHandler,
			errortracking.WithField("listener", "https proxy"),
			withLimiter(limiter),
			withTLSConfig(tlsConfig),
			withProxyV2(),
		)
		servers = append(servers, s)
	}

	// Serve metrics for Prometheus
	if a.config.Metrics.Address != "" {
		s := a.listenMetrics(eg, a.config.Metrics)
		servers = append(servers, s)
	}

	<-ctx.Done()

	var result *multierror.Error

	for _, srv := range servers {
		ctx, cancel := context.WithTimeout(context.Background(), a.config.General.ServerShutdownTimeout)

		if err := srv.Shutdown(ctx); err != nil {
			result = multierror.Append(result, err)
		}

		cancel()
	}

	if err := eg.Wait(); err != nil {
		result = multierror.Append(result, err)
	}

	if result.ErrorOrNil() != nil {
		errortracking.CaptureErrWithStackTrace(result.ErrorOrNil())
		return result.ErrorOrNil()
	}

	return nil
}

func (a *theApp) listen(eg *errgroup.Group, addr string, h http.Handler, errTrackingOpt errortracking.CaptureOption, opts ...option) *http.Server {
	server := newHTTPServer(a)
	eg.Go(func() error {
		if err := a.listenAndServe(server, addr, h, opts...); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errortracking.CaptureErrWithStackTrace(err, errTrackingOpt)
			return err
		}

		return nil
	})

	return server
}

func (a *theApp) listenMetrics(eg *errgroup.Group, config cfg.Metrics) *http.Server {
	server := newHTTPServer(a)
	eg.Go(func() error {
		l, err := net.Listen("tcp", config.Address)
		if err != nil {
			errortracking.CaptureErrWithStackTrace(err, errortracking.WithField("listener", "metrics"))
			return fmt.Errorf("failed to listen on addr %s: %w", config.Address, err)
		}

		if config.TLSConfig != nil {
			l = cryptotls.NewListener(l, config.TLSConfig)
		}

		monitoringOpts := []monitoring.Option{
			monitoring.WithBuildInformation(VERSION, ""),
			monitoring.WithListener(l),
			monitoring.WithServer(server),
		}

		err = monitoring.Start(monitoringOpts...)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errortracking.CaptureErrWithStackTrace(err, errortracking.WithField("listener", "metrics"))
			return err
		}

		return nil
	})

	return server
}

func runApp(config *cfg.Config) error {
	redirects.SetConfig(config.Redirects)

	source, err := gitlab.New(&config.GitLab)
	if err != nil {
		return fmt.Errorf("could not create domains config source: %w", err)
	}

	a := theApp{config: config, source: source}

	err = logging.ConfigureLogging(a.config.Log.Format, a.config.Log.Verbose, a.config.Log.FieldSchemaVersion, a.config.Log.FieldDualEmitTarget)
	if err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}

	a.Artifact = artifact.New(config.ArtifactsServer.URL, config.ArtifactsServer.TimeoutSeconds, config.General.Domain, config.GitLab.ClientCfg)

	if err := a.setAuth(config); err != nil {
		return err
	}

	a.Handlers = handlers.New(a.Auth, a.Artifact)

	if err := mimedb.LoadTypes(); err != nil {
		logging.Logger.WarnContext(context.Background(), "Loading extended MIME database failed", logv2.Error(err))
	}

	// TODO: reconfigure all VFS'
	//  https://gitlab.com/gitlab-org/gitlab-pages/-/issues/512
	if err := zip.Instance().Reconfigure(config); err != nil {
		return fmt.Errorf("failed to reconfigure zip VFS: %w", err)
	}

	return a.Run()
}

func (a *theApp) setAuth(config *cfg.Config) error {
	if config.Authentication.ClientID == "" {
		return nil
	}

	var err error
	a.Auth, err = auth.New(&auth.Options{
		PagesDomain:          config.General.Domain,
		StoreSecret:          config.Authentication.Secret,
		ClientID:             config.Authentication.ClientID,
		ClientSecret:         config.Authentication.ClientSecret,
		RedirectURI:          config.Authentication.RedirectURI,
		InternalGitlabServer: config.GitLab.InternalServer,
		PublicGitlabServer:   config.GitLab.PublicServer,
		AuthScope:            config.Authentication.Scope,
		AuthTimeout:          config.Authentication.Timeout,
		CookieSessionTimeout: config.Authentication.CookieSessionTimeout,
		AllowNamespaceInPath: config.General.NamespaceInPath,
		ClientCfg:            config.GitLab.ClientCfg,
	})
	if err != nil {
		return fmt.Errorf("could not initialize auth package: %w", err)
	}

	return nil
}

// handlePanicMiddleware logs and captures the recover() information from any panic
func handlePanicMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			i := recover()
			if i != nil {
				err := fmt.Errorf("panic trace: %v", i)
				metrics.PanicRecoveredCount.Inc()
				logging.Logger.ErrorContext(logging.LogRequest(r), "recovered from panic", "error", err)
				errortracking.CaptureErrWithReqAndStackTrace(err, r)
				httperrors.Serve500(w)
			}
		}()

		handler.ServeHTTP(w, r)
	})
}
