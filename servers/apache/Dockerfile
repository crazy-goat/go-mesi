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

RUN wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz && \
    rm go1.23.0.linux-amd64.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/root/go"
ENV GODEBUG=netdns=cgo
ENV GOARCH=amd64

WORKDIR /build

COPY go.mod go.sum ./
COPY mesi/ ./mesi/
COPY libgomesi/ ./libgomesi/
RUN cd libgomesi && \
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go && \
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
