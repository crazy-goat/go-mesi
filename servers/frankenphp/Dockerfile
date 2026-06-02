FROM php:8.3-zts-bookworm AS php

FROM golang:1.26-bookworm AS build

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

RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

WORKDIR /build

COPY . /build/go-mesi

WORKDIR /build/go-mesi

ENV XCADDY_GO_BUILD_FLAGS="-tags=nobadger,nomysql,nopgx,nowatcher"

RUN CGO_ENABLED=1 \
    CGO_CFLAGS="$(php-config --includes)" \
    CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
    xcaddy build \
        --output /usr/local/bin/frankenphp \
        --with github.com/dunglas/frankenphp \
        --with github.com/dunglas/frankenphp/caddy \
        --with github.com/crazy-goat/go-mesi/servers/caddy=./servers/caddy \
        --with github.com/crazy-goat/go-mesi=.

FROM dunglas/frankenphp:1-php8.3

COPY --from=build /usr/local/bin/frankenphp /usr/local/bin/frankenphp

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/frankenphp"]
CMD ["run", "--config", "/etc/caddy/Caddyfile"]
