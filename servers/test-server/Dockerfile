FROM golang:alpine AS build
COPY . /tmp/example-server
WORKDIR /tmp/example-server
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
RUN go mod tidy && go mod download && go build test-server.go

FROM alpine
COPY --from=build --chmod=777 /tmp/example-server/test-server /usr/bin

ENTRYPOINT ["/usr/bin/test-server"]