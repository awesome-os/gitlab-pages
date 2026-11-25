# GitLab Pages - Agent Guide

This document provides guidance for AI agents working with the GitLab Pages codebase.

## Project Overview

**GitLab Pages** is a Go daemon that serves static websites for GitLab projects. It resolves
domains through the GitLab internal API, serves content from ZIP archives (object storage or
disk) through a virtual file system, and handles custom domains, TLS and ACME, access control
(OAuth), redirects, custom headers, and rate limiting.

- Default branch: `master`
- Go version: see `.tool-versions`
- Module path: `gitlab.com/gitlab-org/gitlab-pages`

## Project Structure

```plaintext
.
├── main.go, app.go, server.go   # Entry point, application wiring, HTTP servers
├── internal/
│   ├── acme/                    # ACME challenge handling
│   ├── artifact/                # Proxying to job artifacts
│   ├── auth/                    # Access control and OAuth with GitLab
│   ├── config/                  # Flags and configuration
│   ├── customheaders/           # Custom response headers
│   ├── domain/                  # Domain model and resolver
│   ├── feature/                 # Environment variable based feature flags
│   ├── handlers/                # HTTP handlers and middleware
│   ├── httprange/, httpfs/      # Read ZIP archives over HTTP range requests or from allowed disk paths
│   ├── ratelimiter/             # Rate limiting
│   ├── redirects/               # _redirects file support
│   ├── routing/, serving/       # Domain lookup per request, and content serving
│   ├── source/                  # Domain sources, including the GitLab API client
│   ├── tls/                     # TLS configuration
│   ├── vfs/                     # Virtual file system over ZIP archives
│   └── testhelpers/             # Shared test utilities
├── test/
│   ├── acceptance/              # Acceptance tests against a built binary
│   ├── gitlabstub/              # Stub GitLab API server for tests
│   └── testdata/                # TLS test fixtures
├── shared/pages/                # Fixture Pages sites used by tests
├── metrics/                     # Prometheus metrics
└── docs/                        # IP restriction notes (main docs live on docs.gitlab.com)
```

## Development Workflow

### Building

```shell
make                # Build ./gitlab-pages
FIPS_MODE=1 make    # Build in FIPS mode
```

### Running locally

Pages resolves every domain through the GitLab API, so it serves nothing without a GitLab
instance. To verify changes, use the tests in [Testing](#testing) instead. Acceptance tests
stub the API with `test/gitlabstub`.

To run Pages against a real GitLab instance, use GDK with `gitlab_pages.enabled: true` in
`gdk.yml`. GDK runs `bin/gitlab-pages` and does not rebuild it, so run `make` first from
`$GDK_ROOT/gitlab-pages`:

```shell
make && gdk restart gitlab-pages && gdk tail gitlab-pages
```

All flags are in `internal/config/flags.go`.

### Testing

```shell
make unit-test      # Unit tests only (-short), no binary needed
make test           # All tests, including acceptance (builds the binary first)
make acceptance     # Acceptance tests in ./test/acceptance
make race           # All tests with the race detector
make cover          # Coverage report (excludes acceptance tests)
```

To run a single package or test:

```shell
go test ./internal/vfs/zip -count=1
go test ./internal/auth -run TestName -count=1
```

Acceptance tests run against the compiled `gitlab-pages` binary. Rebuild with `make` before
running them directly with `go test ./test/acceptance`.

### Code quality

```shell
make lint                 # golangci-lint, configured in .golangci.yml
make format               # golangci-lint with --fix
make deps-check           # go mod tidy must leave go.mod and go.sum unchanged
make mocks-check          # Generated mocks must be up to date
make generate-mocks       # Regenerate mocks after changing mocked interfaces
make validate-log-fields  # LabKit logging field validation
```

Run `make lint` and the relevant tests before proposing changes.

## Code Style and Standards

- Follow the [Go standards and style guidelines](https://docs.gitlab.com/development/go_guide/).
- Code must pass `golangci-lint` with the project configuration in `.golangci.yml`.
- Return errors explicitly. Do not `panic` in production code.
- Exported identifiers need doc comments.
- Match the patterns already used in the package you are changing.
- If you change an interface that has a generated mock, run `make generate-mocks` and commit
  the result.
- Run `make deps-check` after adding or updating dependencies.

### Logging fields

Follow the [observability field standardisation](https://handbook.gitlab.com/handbook/engineering/architecture/design-documents/observability_field_standardisation/):

- Use constants from the [LabKit `fields` package](https://gitlab.com/gitlab-org/labkit/-/tree/master/v2/fields)
  for common log fields instead of string literals.
- Do not generate field names dynamically.
- Do not hand-edit `.labkit_logging_todo.yml`. It is generated by `validate-log-fields -update-todo`.

### Feature flags

Pages feature flags are environment variables defined in `internal/feature/feature.go`, not
Rails feature flag YAML files. See [feature flags](https://docs.gitlab.com/development/pages/#feature-flags).

- Add a `Feature` with an `FF_*` environment variable and a doc comment.
- New flags must be disabled by default: leave `defaultEnabled` unset. A disabled flag turns on
  when its variable is `true`; an enabled flag turns off when it is `false`.
- Create a rollout issue with the `Feature Flag` issue template.
- Add the `~"feature flag"` label to MRs that add, change, or remove a feature flag.

## Testing Guidelines

- Unit tests live next to the code in `*_test.go` files.
- Add acceptance tests in `test/acceptance/` for behavior that is visible end to end
  (routing, auth, headers, redirects). Use `test/gitlabstub` to stub the GitLab API.
- Bug fixes should include a regression test.
- Reuse helpers in `internal/testhelpers/` before writing new ones.

## Always-on Rules

These rules apply to every task:

- **Minimal fix**: apply the smallest change that fixes the root cause, not the symptom.
  Make no refactors, cleanups, or unrelated improvements. If the real fix needs one, say so and
  explain why instead of doing it silently.
- **Security fixes**: an issue needs a security fix when it is confidential, has the
  `~security` label, and does NOT have the `~security-fix-in-public` label. When all three
  hold, DO NOT push to `gitlab-org/gitlab-pages`. Work in
  [`gitlab-org/security/gitlab-pages`](https://gitlab.com/gitlab-org/security/gitlab-pages)
  and follow the security release section of `PROCESS.md`.
- **Comment discipline**: keep code comments to 1-3 lines, and add one only when the reason is
  not obvious, like an invariant, a gotcha, or a tradeoff. Do not write comments that restate the code.
- **Prose clarity**: write comments, MR and issue text, and docs in concise, plain language,
  and check facts before presenting them.

## Merge Requests

- Use the `Default` MR template and complete its checklist (feature flag, tests, docs).
- Add a `Changelog` trailer to commits that belong in the changelog, for example
  `Changelog: fixed`. Valid categories are in `.gitlab/changelog_config.yml`.
- Keep each MR small and focused on one issue or feature.
- Never rewrite the history of `master` or `X-Y-stable` branches, and never delete tags.
- For backports to stable branches, follow the stable releases section of `PROCESS.md`.

## AI-authored GitLab Comments

State the point first. Put supporting reasoning or evidence in a `<details>` block so readers
can skip it. Leave out the block when there is nothing to justify.

## Related Documentation

- [GitLab Pages development](https://docs.gitlab.com/development/pages/)
- [Code review guidelines](https://docs.gitlab.com/development/code_review/)
- `PROCESS.md`: reviewing, versioning, releases, and security releases
- `CONTRIBUTING.md`: contribution guidelines
