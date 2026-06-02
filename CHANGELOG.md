# Changelog

## [0.9.0] - Unreleased

### Added
- CLI memory cache backend: `-cache-backend=memory`, `-cache-size`, and `-cache-ttl` flags wire the `mesi.MemoryCache` into CLI invocations so duplicate `<esi:include>` URLs are served from cache within a single run (#207)

## [0.8.0] - Unreleased

### Added
- Traefik integration tests: docker-compose, test.sh, and CI job (#79, #302)
- RoadRunner integration tests: Dockerfile, docker-compose, Makefile, test.sh, and CI job (#80, #306)
- Nginx integration tests: Dockerfile, docker-compose, test.sh, and CI job (#81, #300)
- Caddy integration tests: Dockerfile, docker-compose, Caddyfile.test, test.sh, and CI job (#82, #301)
- FrankenPHP integration tests: Dockerfile, docker-compose, Caddyfile.ci, test.sh, PHP fixtures, and CI job (#83, #304)
- Standalone proxy tests: Go unit tests, E2E test.sh, and CI job (#84, #271)
- PHP extension test suite: `.phpt` tests, Dockerfile, docker-compose, test.sh, and CI job (#85, #305)
- CLI tests: Go unit tests, E2E test.sh, and separate unit/E2E CI jobs (#86, #272)
