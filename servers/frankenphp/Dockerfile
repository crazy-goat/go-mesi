FROM php:8.3-cli-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://go.dev/dl/go1.26.0.linux-amd64.tar.gz | tar -C /usr/local -xz

ENV PATH=$PATH:/usr/local/go/bin:/root/go/bin

RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

WORKDIR /build

COPY . /build/go-mesi

WORKDIR /build/go-mesi

RUN CGO_ENABLED=1 \
    xcaddy build \
        --output /usr/local/bin/frankenphp \
        --with github.com/dunglas/frankenphp \
        --with github.com/crazy-goat/go-mesi/servers/caddy=./servers/caddy

FROM dunglas/frankenphp:latest

COPY --from=build /usr/local/bin/frankenphp /usr/local/bin/frankenphp

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/frankenphp"]
CMD ["run", "--config", "/etc/caddy/Caddyfile"]
