FROM golang:1.23-alpine AS build
COPY . /tmp/example-server
WORKDIR /tmp/example-server
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
RUN go build test-server.go

FROM alpine:3.20
COPY --from=build --chmod=755 /tmp/example-server/test-server /usr/bin

ENTRYPOINT ["/usr/bin/test-server"]
