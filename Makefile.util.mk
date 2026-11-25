.PHONY: lint format test unit-test race acceptance bench cover deps-check mocks-check deps-download changelog zip validate-log-fields

LINT_FLAGS ?=  $(if $V,-v)
REPORT_FILE ?=
GOLANGCI_VERSION=v2.12.2
GOTESTSUM_VERSION=v1.10.0
COVERAGE_PACKAGES=$(shell (go list ./... | grep -v -e "test/acceptance" | tr "\n", "," | sed 's/\(.*\),/\1 /'))
# Labkit validate-log-fields version
LABKIT_VALIDATE_VERSION=v2.33.0

lint:
	$Q go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./... $(LINT_FLAGS) | tee ${REPORT_FILE}

format:
	$Q go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./... --fix $(LINT_FLAGS) | tee ${REPORT_FILE}

test: gitlab-pages
	go run gotest.tools/gotestsum@$(GOTESTSUM_VERSION) --junitfile junit-test-report.xml --format testname -- ./... ${ARGS}

unit-test:
	go run gotest.tools/gotestsum@$(GOTESTSUM_VERSION) --junitfile junit-test-report.xml --format testname -- -short ./... ${ARGS}

race: gitlab-pages
	go run gotest.tools/gotestsum@$(GOTESTSUM_VERSION) --junitfile junit-test-report.xml --format testname -- -race $(if $V,-v) ./... ${ARGS}

acceptance: gitlab-pages
	go run gotest.tools/gotestsum@$(GOTESTSUM_VERSION) --junitfile junit-test-report.xml --format testname -- $(if $V,-v) ./test/acceptance ${ARGS}

bench: gitlab-pages
	go test -bench=. -run=^$$ ./...

# The acceptance tests cannot count for coverage
cover:
	@echo "NOTE: make cover does not exit 1 on failure, don't use it to check for tests success!"
	$Q rm -f .cover/test.coverage
	$Q mkdir -p .cover
	$Q go test -short -cover -coverpkg=$(COVERAGE_PACKAGES) -coverprofile=.cover/test.coverage ./...
	$Q go tool cover -html .cover/test.coverage -o coverage.html
	@echo ""
	@echo "=====> Total test coverage: <====="
	@echo ""
	$Q go tool cover -func .cover/test.coverage

deps-check:
	go mod tidy
	@if git diff --color=always --exit-code -- go.mod go.sum; then \
		echo "go.mod and go.sum are ok"; \
	else \
    echo ""; \
		echo "go.mod and go.sum are modified, please commit them";\
		exit 1; \
  fi;

mocks-check: generate-mocks
	@if git diff --color=always --exit-code -- *_mock.go; then \
		echo "mocks are ok"; \
	else \
    echo ""; \
		echo "mocks needs to be regenerated, please run 'make generate-mocks' and commit them";\
		exit 1; \
  fi;

deps-download:
	go mod download

changelog:
	TOKEN='$(GITLAB_PRIVATE_TOKEN)' VERSION='$(shell cat VERSION)' BRANCH='$(BRANCH)'  bash ./.gitlab/scripts/changelog.sh
ifndef GITLAB_PRIVATE_TOKEN
	$(error GITLAB_PRIVATE_TOKEN is undefined)
endif

zip:
	cd $(PWD)/shared/pages/$(PROJECT_SUBDIR)/ && \
	zip -r ../public.zip ./

# Validate logging fields using labkit's validate-log-fields tool.
validate-log-fields:
	go run gitlab.com/gitlab-org/labkit/v2/cmd/validate-log-fields@$(LABKIT_VALIDATE_VERSION) .
