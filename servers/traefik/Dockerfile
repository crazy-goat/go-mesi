FROM golang:1.23-alpine AS deps

RUN apk add --no-cache git jq

WORKDIR /src

COPY go.mod go.sum ./
COPY mesi/ ./mesi/
COPY middleware/ ./middleware/
COPY libgomesi/ ./libgomesi/
COPY servers/traefik/go.mod servers/traefik/go.sum ./servers/traefik/

RUN cd servers/traefik && go mod download

RUN mkdir -p /plugins-local/src/github.com/crazy-goat/go-mesi && \
    cp -r mesi /plugins-local/src/github.com/crazy-goat/go-mesi/ && \
    cp -r middleware /plugins-local/src/github.com/crazy-goat/go-mesi/ && \
    cp -r libgomesi /plugins-local/src/github.com/crazy-goat/go-mesi/ && \
    mkdir -p /plugins-local/src/github.com/crazy-goat/go-mesi/servers/traefik && \
    cp servers/traefik/go.mod servers/traefik/go.sum /plugins-local/src/github.com/crazy-goat/go-mesi/servers/traefik/ && \
    cp servers/traefik/*.go /plugins-local/src/github.com/crazy-goat/go-mesi/servers/traefik/

RUN cd servers/traefik && go list -m -json all | jq -r 'select(.Dir != null and .Path != "github.com/crazy-goat/go-mesi") | "\(.Path) \(.Dir)"' | while read -r modpath moddir; do \
        target="/plugins-local/src/$modpath"; \
        mkdir -p "$target"; \
        cp -r "$moddir"/* "$target/" 2>/dev/null || true; \
    done

FROM traefik:latest

USER root

COPY --from=deps /plugins-local /plugins-local

ENV GOPATH=/plugins-local

USER traefik
