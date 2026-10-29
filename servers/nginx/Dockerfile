# amd64-only build, needs the platform pinned
# hadolint ignore=DL3029
FROM --platform=linux/amd64 ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    build-essential \
    libpcre3-dev \
    libssl-dev \
    zlib1g-dev \
    wget \
    curl \
    && rm -rf /var/lib/apt/lists/*

ARG GO_VERSION=1.26.10
ARG GO_SHA256=ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5
RUN wget -q -O /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" && \
    printf '%s  /tmp/go.tgz\n' "${GO_SHA256}" > /tmp/go.sha256 && \
    sha256sum -c /tmp/go.sha256 && \
    tar -C /usr/local -xzf /tmp/go.tgz && \
    rm /tmp/go.tgz /tmp/go.sha256

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/root/go"
ENV GODEBUG=netdns=cgo
ENV GOARCH=amd64
ENV GOTOOLCHAIN=local

WORKDIR /build

COPY go.mod go.sum ./
COPY mesi/ ./mesi/
COPY libgomesi/ ./libgomesi/
COPY servers/nginx/ngx_http_mesi_module.c servers/nginx/config ./servers/nginx/
RUN cd libgomesi && \
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go cache_config.go && \
    cp libgomesi.so /usr/lib/ && \
    cp libgomesi.h /usr/include/

ARG NGINX_VERSION=1.27.4
RUN wget "http://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz" -O /tmp/nginx.tar.gz && \
    mkdir -p /tmp/nginx-src && \
    tar -xzf /tmp/nginx.tar.gz -C /tmp/nginx-src && \
    cd /tmp/nginx-src/nginx-${NGINX_VERSION} && \
    mkdir -p /tmp/mesi && \
    cp /build/libgomesi/libgomesi.h /tmp/mesi/ && \
    cp /build/servers/nginx/ngx_http_mesi_module.c /tmp/mesi/ && \
    cp /build/servers/nginx/config /tmp/mesi/ && \
    ./configure --add-dynamic-module=/tmp/mesi && \
    make -j"$(nproc)" modules && \
    mkdir -p /usr/lib/nginx/modules && \
    cp objs/ngx_http_mesi_module.so /usr/lib/nginx/modules/ && \
    make install && \
    rm -rf /tmp/nginx-src /tmp/nginx.tar.gz /tmp/mesi

COPY servers/nginx/nginx.conf /usr/local/nginx/conf/nginx.conf
COPY servers/nginx/tests/ /usr/share/nginx/html/

EXPOSE 8080

CMD ["/usr/local/nginx/sbin/nginx", "-g", "daemon off;"]
