package logging

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"gitlab.com/gitlab-org/labkit/correlation"
	labkitfields "gitlab.com/gitlab-org/labkit/v2/fields"
	logv2 "gitlab.com/gitlab-org/labkit/v2/log"

	"gitlab.com/gitlab-org/gitlab-pages/internal/request"
)

// LevelTrace is below Debug, for very verbose diagnostic logs.
const LevelTrace = slog.LevelDebug - 4

// Logger is the global application logger. It is initialised with defaults
// and reconfigured by ConfigureLogging. Build any replacement with NewLogger,
// or wrap it with WithRequestAttrs, or lines logged with a LogRequest context
// lose their request fields.
var Logger = NewLogger(nil)

// Mapping is the per-service logging field migration mapping. It is rebuilt
// once at boot by ConfigureLogging from the resolved schema-version and
// dual-emit-target settings, and is read-only thereafter; callers may invoke
// its AppendDeprecated / AppendDeprecatedDual methods on the hot path.
//
// Initialised at package load to default settings (legacy aliases, no dual
// emit) so any log call made before ConfigureLogging runs still reproduces
// pre-variant behaviour.
var Mapping = labkitfields.NewServiceMapping()

// ConfigureLogging initialises the global Logger with the requested format and
// verbosity, and rebuilds Mapping from the resolved logging field variant
// settings. Both are set exactly once at boot.
func ConfigureLogging(format string, verbose bool, schemaVersion, dualEmitTarget int) error {
	if format == "" {
		format = "json"
	}

	level := slog.LevelInfo
	if verbose {
		level = LevelTrace
	}

	Logger = NewLogger(&logv2.Config{
		UseTextFormat: format == "text",
		LogLevel:      &level,
	})

	Mapping = labkitfields.NewServiceMapping(
		labkitfields.WithSchemaVersion(schemaVersion),
		labkitfields.WithDualEmitTarget(dualEmitTarget),
	)

	return nil
}

// LogRequest returns a context carrying the request's correlation_id, host and
// path. Every logger built by NewLogger, the global Logger included, adds them
// to the lines it writes with that context.
func LogRequest(r *http.Request) context.Context {
	ctx := r.Context()
	return context.WithValue(ctx, requestAttrsKey{}, &requestAttrs{
		logv2.CorrelationID(correlation.ExtractFromContext(ctx)),
		logv2.HTTPHost(r.Host),
		slog.String("path", r.URL.Path),
	})
}

type requestAttrsKey struct{}

// requestAttrs is fixed-size so LogRequest allocates it in one piece.
type requestAttrs [3]slog.Attr

// NewLogger returns a labkit logger that also writes the attributes LogRequest
// stores on the context.
func NewLogger(cfg *logv2.Config) *slog.Logger {
	return WithRequestAttrs(logv2.NewWithConfig(cfg))
}

// WithRequestAttrs returns l with the request attributes of a LogRequest
// context added to every line. Use it for a logger NewLogger cannot build.
// Wrap a logger once. Only the outermost handler is checked for an earlier
// wrap, so wrapping again above another handler writes the fields twice.
func WithRequestAttrs(l *slog.Logger) *slog.Logger {
	if _, ok := l.Handler().(requestAttrsHandler); ok {
		return l
	}
	return slog.New(requestAttrsHandler{l.Handler()})
}

// requestAttrsHandler adds the attributes LogRequest stored on the context to
// each record. The lookup is a single ctx.Value call and allocates nothing when
// the context carries none.
type requestAttrsHandler struct{ slog.Handler }

func (h requestAttrsHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs, ok := ctx.Value(requestAttrsKey{}).(*requestAttrs); ok {
		r.AddAttrs(attrs[:]...)
	}
	return h.Handler.Handle(ctx, r)
}

func (h requestAttrsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestAttrsHandler{h.Handler.WithAttrs(attrs)}
}

func (h requestAttrsHandler) WithGroup(name string) slog.Handler {
	return requestAttrsHandler{h.Handler.WithGroup(name)}
}

// NillableError returns an slog.Attr for an error that may be nil. When err is
// nil, an empty attribute is returned (discarded by slog) so it is safe to
// include in every log call regardless of whether an error occurred.
func NillableError(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	return logv2.Error(err)
}

// responseRecorder wraps http.ResponseWriter to capture the status code and
// response body size.
type responseRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.size += n
	return n, err
}

// Unwrap returns the underlying ResponseWriter so that http.ResponseController
// can access optional interfaces (http.Flusher, http.Hijacker, etc.).
func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// BasicAccessLogger returns HTTP middleware that logs each request using the
// global Logger.
func BasicAccessLogger(handler http.Handler, _ string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rr := &responseRecorder{ResponseWriter: w, status: http.StatusOK}

		handler.ServeHTTP(rr, r)

		duration := time.Since(start)
		durationMs := slog.Float64Value(float64(duration.Nanoseconds()) / 1e6)
		durationS := slog.Float64Value(duration.Seconds())
		ctx := r.Context()

		// Pre-size to fit the 12 fixed attrs, the variant duration field (one
		// or two attrs depending on dual emit), and any client-cert pairs.
		args := make([]slog.Attr, 0, 16)
		args = append(args,
			logv2.CorrelationID(correlation.ExtractFromContext(ctx)),
			logv2.HTTPMethod(r.Method),
			logv2.HTTPHost(r.Host),
			logv2.HTTPURI(CleanURL(r.RequestURI)),
			logv2.HTTPProto(r.Proto),
			logv2.RemoteAddr(r.RemoteAddr),
			logv2.RemoteIP(request.GetRemoteAddrWithoutPort(r)),
			logv2.HTTPStatusCode(rr.status),
			slog.Int("response_size", rr.size),
			logv2.HTTPUserAgent(r.UserAgent()),
			logv2.HTTPReferrer(CleanURL(r.Referer())),
			slog.Bool("pages_https", request.IsHTTPS(r)),
		)
		args = Mapping.AppendDeprecatedDual(args, "duration_ms", durationMs, durationS)

		// Log TLS client certificate info if present.
		if r.TLS != nil {
			for i, cert := range r.TLS.PeerCertificates {
				args = append(args,
					slog.String(fmt.Sprintf("x509_subject_%d", i), cert.Subject.ToRDNSequence().String()),
					slog.String(fmt.Sprintf("x509_issuer_%d", i), cert.Issuer.ToRDNSequence().String()),
				)
			}
		}

		Logger.LogAttrs(ctx, slog.LevelInfo, "access", args...)
	})
}
