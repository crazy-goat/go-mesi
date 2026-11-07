# FrankenPHP version, pinned. Declared before the first FROM so that the runtime
# stage at the bottom can use it too.
#
# Why this is pinned and why the build stage is not simply moved to Go 1.27:
# an unpinned build dependency lets an upstream release break `ci-ok` on `main`
# without a commit to this repository. frankenphp v1.13.0 (released 2026-10-04)
# declares `go 1.27.0` while this build stage is Go 1.26 and GOTOOLCHAIN=local
# forbids fetching a newer toolchain, so the image build failed for everyone.
# Moving the build stage to Go 1.27 would only postpone that: the next release
# requiring Go 1.28 breaks it again, in the same way, from the same cause.
# Pinning keeps the image build a function of this repository alone.
#
# The cost of a pin is that it ages silently, so every pinned build dependency
# records the Go version it was verified against in *_GO_MIN below, and
# scripts/check-docker-build-pins.sh compares those against this build stage on
# every lint run. Bumping a pin therefore forces a conscious decision about Go.
ARG FRANKENPHP_VERSION=1.12.7

FROM php:8.3-zts-bookworm AS php

# Build-stage toolchain. Its Go version must be >= every *_GO_MIN declared below.
FROM golang:1.26-bookworm AS build

# Pinned build dependencies and the minimum Go each one declares in its go.mod
# at that version (frankenphp and frankenphp/caddy both declare go 1.26.0;
# xcaddy declares go 1.21; the caddy/v2 that frankenphp/caddy pulls in declares
# go 1.26.0). Recorded by building the image at this pin, not guessed.
ARG FRANKENPHP_VERSION
ARG FRANKENPHP_GO_MIN=1.26.0
ARG XCADDY_VERSION=0.4.7
ARG XCADDY_GO_MIN=1.21

COPY --from=php /usr/local /usr/local

RUN ldconfig

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    libxml2-dev \
    libssl-dev \
    libsqlite3-dev \
    libcurl4-openssl-dev \
    libonig-dev \
    libreadline-dev \
    libncurses-dev \
    libargon2-dev \
    zlib1g-dev \
    && rm -rf /var/lib/apt/lists/*

RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@v${XCADDY_VERSION}

WORKDIR /build

COPY . /build/go-mesi

WORKDIR /build/go-mesi

ENV XCADDY_GO_BUILD_FLAGS="-tags=nobadger,nomysql,nopgx,nowatcher"

RUN CGO_ENABLED=1 \
    CGO_CFLAGS="$(php-config --includes)" \
    CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
    xcaddy build \
        --output /usr/local/bin/frankenphp \
        --with github.com/dunglas/frankenphp@v${FRANKENPHP_VERSION} \
        --with github.com/dunglas/frankenphp/caddy@v${FRANKENPHP_VERSION} \
        --with github.com/crazy-goat/go-mesi/servers/caddy=./servers/caddy \
        --with github.com/crazy-goat/go-mesi=.

# Pinned to the same FrankenPHP release the build stage compiled, so the binary
# and the PHP runtime underneath it cannot drift apart. Both tags are Debian
# trixie with PHP 8.3.35 ZTS, so this changes nothing but the version.
FROM dunglas/frankenphp:${FRANKENPHP_VERSION}-php8.3

COPY --from=build /usr/local/bin/frankenphp /usr/local/bin/frankenphp

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/frankenphp"]
CMD ["run", "--config", "/etc/caddy/Caddyfile"]
