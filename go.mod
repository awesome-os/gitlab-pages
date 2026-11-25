module gitlab.com/gitlab-org/gitlab-pages

// before bumping this:
// - update the minimum version used in ci (.gitlab-ci.yml default image and
//   the .go-versions matrix in .gitlab/ci/test.yml)
// - make sure the internal/vfs/serving package is synced with upstream
//   net/http/fs.go, tracked in
//   https://gitlab.com/gitlab-org/gitlab-pages/-/issues/1212
go 1.26.9

require (
	github.com/3th1nk/cidr v0.4.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/golang/mock v1.7.0-rc.1
	github.com/gorilla/handlers v1.5.2
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/securecookie v1.1.2
	github.com/gorilla/sessions v1.3.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/karlseguin/ccache/v3 v3.0.8
	github.com/namsral/flag v1.7.4-pre
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pires/go-proxyproto v0.16.0
	github.com/prometheus/client_golang v1.25.0
	github.com/rs/cors v1.11.1
	github.com/sirupsen/logrus v1.10.2
	github.com/stretchr/testify v1.12.1
	github.com/tj/go-redirects v0.0.0-20200911105812-fd1ba1020b37
	gitlab.com/feistel/go-contentencoding v1.0.0
	gitlab.com/gitlab-org/go-mimedb v1.52.0
	gitlab.com/gitlab-org/labkit v1.65.5
	gitlab.com/gitlab-org/labkit/v2 v2.33.1
	go.uber.org/automaxprocs v1.6.0
	go.uber.org/goleak v1.3.0
	golang.org/x/crypto v0.59.0
	golang.org/x/net v0.62.0
	golang.org/x/sync v0.25.0
	golang.org/x/time v0.16.0
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.23.2 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/profiler v0.6.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/getsentry/sentry-go v0.50.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.20 // indirect
	github.com/googleapis/gax-go/v2 v2.24.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.4 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oklog/ulid/v2 v2.1.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.72.0 // indirect
	github.com/prometheus/procfs v0.22.0 // indirect
	github.com/sebest/xff v0.0.0-20210106013422-671bd2870b3a // indirect
	github.com/tj/assert v0.0.3 // indirect
	gitlab.com/gitlab-org/go/reopen v1.0.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/api v0.298.0 // indirect
	google.golang.org/genproto v0.0.0-20260715232425-e75dac1f907d // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260715232425-e75dac1f907d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
