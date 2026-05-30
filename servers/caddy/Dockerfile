FROM golang:1.26-alpine AS build

RUN apk add --no-cache git

RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

WORKDIR /build

COPY . /build/go-mesi

WORKDIR /build/go-mesi

RUN xcaddy build \
    --with github.com/crazy-goat/go-mesi/servers/caddy=./servers/caddy \
    --with github.com/crazy-goat/go-mesi=.

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /build/go-mesi/caddy /usr/bin/caddy
EXPOSE 8080
ENTRYPOINT ["/usr/bin/caddy"]
CMD ["run", "--config", "/etc/caddy/Caddyfile"]
