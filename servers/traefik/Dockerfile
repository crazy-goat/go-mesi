# Stage 1: Build plugin source tree for Yaegi interpreter
FROM golang:1.24-alpine AS deps

RUN apk add --no-cache git jq

WORKDIR /src

COPY go.mod go.sum ./
COPY mesi/ ./mesi/
COPY middleware/ ./middleware/
# Only copy Traefik server files that don't use build tags or third-party cache backends
COPY servers/traefik/go.mod servers/traefik/go.sum servers/traefik/.traefik.yml ./servers/traefik/
COPY servers/traefik/mesi.go servers/traefik/cache.go servers/traefik/cache_memory.go ./servers/traefik/

RUN cd servers/traefik && go mod download

# Assemble the plugin source tree under GOPATH.
# Files are removed here because Yaegi (Traefik's Go interpreter) cannot handle:
#   - syscall/unsafe packages (ssrf_dialer.go)
#   - third-party packages that use unsafe (cache_redis, cache_memcached -> go.uber.org/atomic)
#   - build tags (ignored by Yaegi, so cache_redis.go/cache_memcached.go would conflict with cache.go)
# See: https://github.com/traefik/yaegi/issues/1636
RUN mkdir -p /plugins-local/src && \
    cp -r /usr/local/go/src /plugins-local/src/go-stdlib && \
    mkdir -p /plugins-local/src/github.com/crazy-goat/go-mesi && \
    cp -r mesi /plugins-local/src/github.com/crazy-goat/go-mesi/ && \
    rm /plugins-local/src/github.com/crazy-goat/go-mesi/mesi/ssrf_dialer.go && \
    rm -rf /plugins-local/src/github.com/crazy-goat/go-mesi/mesi/cache_redis && \
    rm -rf /plugins-local/src/github.com/crazy-goat/go-mesi/mesi/cache_memcached && \
    rm -rf /plugins-local/src/github.com/crazy-goat/go-mesi/mesi/*_test.go && \
    cp -r middleware /plugins-local/src/github.com/crazy-goat/go-mesi/ && \
    cp servers/traefik/.traefik.yml /plugins-local/src/github.com/crazy-goat/go-mesi/.traefik.yml && \
    mkdir -p /plugins-local/src/github.com/crazy-goat/go-mesi/servers/traefik && \
    cp servers/traefik/go.mod servers/traefik/go.sum servers/traefik/mesi.go servers/traefik/cache.go servers/traefik/cache_memory.go /plugins-local/src/github.com/crazy-goat/go-mesi/servers/traefik/

# Copy third-party dependencies, then remove go.uber.org (uses unsafe)
RUN cd servers/traefik && go list -m -json all | jq -r 'select(.Dir != null and .Path != "github.com/crazy-goat/go-mesi") | "\(.Path) \(.Dir)"' | while read -r modpath moddir; do \
        target="/plugins-local/src/$modpath"; \
        mkdir -p "$target"; \
        cp -r "$moddir"/* "$target/" 2>/dev/null || true; \
    done && \
    rm -rf /plugins-local/src/go.uber.org

# Provide a stub NewSSRFSafeTransport for Yaegi (no dial-time IP blocking).
# The real implementation in ssrf_dialer.go uses syscall.RawConn which Yaegi
# cannot interpret. URL-level SSRF protection (allowed hosts) still works.
RUN printf '%s\n' \
    'package mesi' \
    '' \
    'import "net/http"' \
    '' \
    'func NewSSRFSafeTransport(config EsiParserConfig) *http.Transport {' \
    '	return &http.Transport{}' \
    '}' \
    > /plugins-local/src/github.com/crazy-goat/go-mesi/mesi/ssrf_yaegi.go

# Stage 2: Final Traefik image with plugin
FROM traefik:latest

COPY --from=deps /plugins-local /plugins-local

# Yaegi needs Go stdlib sources to interpret the plugin.
# Symlink them from the plugin-local copy.
RUN mkdir -p /usr/local/go && \
    ln -s /plugins-local/src/go-stdlib /usr/local/go/src

ENV GOPATH=/plugins-local
ENV GOROOT=/usr/local/go
