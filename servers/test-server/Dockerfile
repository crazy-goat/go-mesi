FROM golang:1.26-alpine AS build
ENV GOTOOLCHAIN=local
COPY . /tmp/example-server
WORKDIR /tmp/example-server
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
RUN go build test-server.go

FROM alpine:3.20
# No --chmod: the flag is BuildKit-only and `go build` already emits 0755, so
# COPY preserves an executable mode. This keeps the image buildable with the
# legacy builder (docker build, DOCKER_BUILDKIT=0, docker compose).
COPY --from=build /tmp/example-server/test-server /usr/bin

ENTRYPOINT ["/usr/bin/test-server"]
