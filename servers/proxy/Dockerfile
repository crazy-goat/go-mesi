FROM golang:alpine AS build
COPY . /tmp/mesi-proxy
WORKDIR /tmp/mesi-proxy
ENV CGO_ENABLED=0
ENV GOOS=linux
ARG TARGETARCH
RUN go mod tidy && go mod download && go build -o mesi-proxy .

FROM alpine
COPY --from=build --chmod=755 /tmp/mesi-proxy/mesi-proxy /usr/bin/mesi-proxy

EXPOSE 8080

ENTRYPOINT ["/usr/bin/mesi-proxy"]