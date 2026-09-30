# AGENTS.md

Project commands and specifics for go-mesi. The development process (issue,
worktree, review, PR, merge) is in [docs/workflow.md](docs/workflow.md), the release
process in [docs/release-workflow.md](docs/release-workflow.md).

Everything is written in English (code, comments, docs, commits, issues).
Non-ASCII test data (for example in `mesi/parser_test.go`) is allowed.

## Layout

The repository is a set of Go modules. The root module is `github.com/crazy-goat/go-mesi`.

| Path | Content |
|---|---|
| `mesi/` | Core ESI parser (tokenizer, includes, fetch modes, SSRF guard, caches) |
| `middleware/` | HTTP `ResponseWriter` wrapper used by server integrations |
| `libgomesi/` | C ABI (`libgomesi.so`/`.a`) used by nginx, Apache and the PHP extension |
| `cli/` | `mesi-cli` (own module) |
| `php-ext/` | PHP extension built on `libgomesi` |
| `servers/caddy`, `servers/traefik`, `servers/roadrunner`, `servers/proxy` | Go integrations, each its own module |
| `servers/nginx`, `servers/apache`, `servers/frankenphp` | C modules or Docker setups on top of `libgomesi` |
| `servers/test-server` | Backend used by the integration suites |
| `tests/` | End-to-end runner and fixtures (`*.html` + `*.html.expected`) |
| `examples/` | Usage examples |
| `docs/` | `features.md` (feature matrix), `specs/` (design decisions), process docs |

Read `CHANGELOG.md`, `docs/features.md` and `docs/specs/` before changing behaviour.
They record decisions, for example that errors are never silent defaults.

## Commands

Go version: 1.23 (CI). `servers/traefik` and `servers/caddy` declare newer versions in their own `go.mod`.

```bash
# Guard: no generated artifacts may be tracked
./scripts/check-no-generated-artifacts.sh

# Shared library used by many suites
(cd libgomesi && make build)

# Lint and unit tests of the core (CI runs golangci-lint on ./mesi/... and on cli/)
golangci-lint run ./mesi/...
go vet ./mesi/...
go test -count=1 ./mesi/...

# CLI
(cd cli && golangci-lint run ./...)
(cd cli && go test -count=1 ./...)
(cd cli && bash test.sh)

# Go server modules
(cd servers/proxy && go test -count=1 ./... && bash test.sh)
(cd servers/roadrunner && bash test.sh)
(cd servers/traefik && go test -run TestYaegiCompatibility -count=1 .)

# Docker-based integration suites (need Docker)
(cd servers/apache && ./test.sh)      # also nginx, caddy, traefik, frankenphp

# PHP extension (needs phpize and libgomesi)
make test-php-ext-integration         # runs php-ext/test.sh

# Top-level Makefile targets
make build-cli build-libgomesi test-cli-unit test-cli-e2e test-e2e
```

Docker suites publish host ports from compose variables (`APACHE_HTTP_PORT`,
`NGINX_HTTP_PORT`, `CADDY_HTTP_PORT`, `TRAEFIK_HTTP_PORT`, `FRANKENPHP_HTTP_PORT`,
`PHP_EXT_HTTP_PORT`, and `APACHE_PORT_8081` ... `APACHE_PORT_8095`). The defaults
keep the old ports (18080 and 8081-8095). `bin/worktree.sh` writes free ports to
`.env.worktree`; load it with `set -a && . ./.env.worktree && set +a`.

`libgomesi.so`, `libgomesi.a`, `*.test`, `coverage.*` and
`servers/test-server/test-server` are generated and must never be committed.

## CI

`.github/workflows/tests.yaml` runs on pull requests. The `changes` job detects
documentation-only changes; the `docs` job checks them fast. All build and test
jobs run only for code changes. The required check is `ci-ok`.

## Conventions

- Commit scopes: `mesi`, `cli`, `apache`, `nginx`, `caddy`, `traefik`, `roadrunner`,
  `frankenphp`, `proxy`, `php-ext`, `libgomesi`, `docs`, `tests`, `e2e`, `ci`.
  Example: `feat(roadrunner): add block_private_ips config option (#198)`.
- Tests: unit tests first, then `httptest` integration, then an e2e fixture in
  `tests/fixtures/` when the behaviour is user-visible.
- Follow existing `mesi/*` patterns. Do not add silent defaults or silent substitutions.
- `libgomesi` entry points are ABI-relevant: changing them affects nginx, Apache and the
  PHP extension.
- Labels for areas are `area:<name>` (`area:apache`, `area:caddy`, `area:nginx`,
  `area:traefik`, `area:roadrunner`, `area:cli`, `area:php-extension`, `area:proxy`, `area:mesi`).
