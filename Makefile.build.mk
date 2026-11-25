BINDIR := $(CURDIR)/bin
GO_BUILD_TAGS   := continuous_profiler_stackdriver
MOCKGEN_VERSION=v1.6.0
FIPS_MODE       ?= 0
ifeq ($(FIPS_MODE), 1)
    # The Go Cryptographic Module (GOFIPS140, Go 1.24+) is mutually exclusive
    # with GOEXPERIMENT=boringcrypto. Toolchains that select the native module
    # report its version via "go env GOFIPS140"; legacy golang-fips toolchains
    # report "off" (or empty on Go < 1.24) and still require boringcrypto.
    GOFIPS140_MODULE := $(shell go env GOFIPS140 2>/dev/null)
    ifeq ($(filter-out off,$(GOFIPS140_MODULE)),)
        BORINGCRYPTO_SUPPORT := $(shell GOEXPERIMENT=boringcrypto go version > /dev/null 2>&1; echo $$?)
        ifeq ($(BORINGCRYPTO_SUPPORT), 0)
            # BoringCrypto requires cgo. Without a C compiler Go disables cgo
            # and silently omits BoringCrypto, while still recording
            # GOEXPERIMENT=boringcrypto. Force cgo so that case fails the build.
            GO_BUILD_ENV=CGO_ENABLED=1 GOEXPERIMENT=boringcrypto
        endif
    endif

    GO_BUILD_TAGS := $(GO_BUILD_TAGS),fips

    # Since Go 1.24 the standard library links symbols containing "FIPS" into
    # every binary using crypto/tls, and golang-fips toolchains compile their
    # OpenSSL backend into every build, so no symbol distinguishes a FIPS build.
    # Assert on the build settings recorded in the binary instead. The pattern
    # matches the whole line: "boringcrypto" must be a complete element of the
    # comma-separated GOEXPERIMENT list (so "noboringcrypto" does not match),
    # and GOFIPS140 must be a module version such as v1.0.0-c2097c7c.
    FIPS_BUILD_SETTINGS := ^[[:space:]]*build[[:space:]]+(GOEXPERIMENT=([^,[:space:]]+,)*boringcrypto(,[^,[:space:]]+)*|GOFIPS140=v[0-9]+\.[0-9]+\.[0-9]+(-[0-9a-f]+)?)$$
endif

.PHONY: all generate-mocks build clean

all: gitlab-pages

generate-mocks:
	$Q go run github.com/golang/mock/mockgen@$(MOCKGEN_VERSION) -source=internal/interface.go -destination=internal/handlers/mock/handler_mock.go -package=mock
	$Q go run github.com/golang/mock/mockgen@$(MOCKGEN_VERSION) -source=internal/source/source.go -destination=internal/source/mock/source_mock.go -package=mock
	$Q go run github.com/golang/mock/mockgen@$(MOCKGEN_VERSION) -source=internal/source/gitlab/mock/client_stub.go -destination=internal/source/gitlab/mock/client_mock.go -package=mock
	$Q go run github.com/golang/mock/mockgen@$(MOCKGEN_VERSION) -source=internal/domain/resolver.go -destination=internal/domain/mock/resolver_mock.go -package=mock

build:
	$Q GOBIN=$(BINDIR) $(GO_BUILD_ENV) go install $(if $V,-v) -ldflags="$(VERSION_FLAGS) -B gobuildid" -tags "${GO_BUILD_TAGS}" -buildmode exe $(IMPORT_PATH)
ifeq ($(FIPS_MODE), 1)
	go version -m $(BINDIR)/gitlab-pages | grep -qE '$(FIPS_BUILD_SETTINGS)' \
		&& echo "binary is correctly built in FIPS mode" \
		|| (echo "binary is not correctly built in FIPS mode" && exit 1)
endif

clean:
	$Q GOBIN=$(BINDIR) go clean -i -modcache -x

gitlab-pages: build
	$Q cp -f $(BINDIR)/gitlab-pages .
