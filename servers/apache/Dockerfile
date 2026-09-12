FROM --platform=linux/amd64 ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    apache2 \
    apache2-dev \
    libapr1-dev \
    libaprutil1-dev \
    curl \
    wget \
    && rm -rf /var/lib/apt/lists/*

ARG GO_VERSION=1.23.0
ARG GO_SHA256=905a297f19ead44780548933e0ff1a1b86e8327bb459e92f9c0012569f76f5e3
ARG TARGETARCH

RUN set -eux; \
    arch="${TARGETARCH:-amd64}"; \
    url="https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz"; \
    curl -fsSL -o /tmp/go.tgz "$url"; \
    echo "${GO_SHA256}  /tmp/go.tgz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm /tmp/go.tgz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/root/go"
ENV GODEBUG=netdns=cgo
ENV GOARCH=amd64

WORKDIR /build

COPY go.mod go.sum ./
COPY mesi/ ./mesi/
COPY libgomesi/ ./libgomesi/
RUN cd libgomesi && \
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go cache_config.go && \
    cp libgomesi.so /usr/lib/ && \
    cp libgomesi.h /usr/include/

COPY servers/apache/mod_mesi.c /build/mod_mesi.c
RUN apxs -c -i mod_mesi.c

RUN a2dismod mpm_event && a2enmod mpm_prefork headers proxy proxy_http

COPY servers/apache/httpd.conf /etc/apache2/sites-available/mesi.conf
RUN a2dissite 000-default && a2ensite mesi

COPY servers/apache/tests/ /var/www/html/

EXPOSE 80

CMD ["apache2ctl", "-D", "FOREGROUND"]
