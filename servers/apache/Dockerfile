FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    apache2 \
    apache2-dev \
    libapr1-dev \
    libaprutil1-dev \
    golang-go \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

COPY libgomesi/ /build/libgomesi/
RUN cd /build/libgomesi && \
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go && \
    cp libgomesi.so /usr/lib/ && \
    cp libgomesi.h /usr/include/

COPY servers/apache/mod_mesi.c /build/mod_mesi.c
RUN apxs -c -i mod_mesi.c

RUN a2enmod headers proxy proxy_http

COPY servers/apache/httpd.conf /etc/apache2/sites-available/mesi.conf
RUN a2dissite 000-default && a2ensite mesi

COPY servers/apache/tests/ /var/www/html/

EXPOSE 80

CMD ["apache2ctl", "-D", "FOREGROUND"]
