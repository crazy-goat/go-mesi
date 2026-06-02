FROM golang:1.23-bookworm AS libgomesi-build

WORKDIR /build

COPY libgomesi/ libgomesi/

RUN cd libgomesi && go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go

FROM php:8.3-cli-bookworm AS ext-build

COPY --from=libgomesi-build /build/libgomesi/libgomesi.so /usr/local/lib/
COPY --from=libgomesi-build /build/libgomesi/libgomesi.h /usr/local/include/

RUN ldconfig

RUN apt-get update && apt-get install -y --no-install-recommends \
    autoconf \
    bison \
    re2c \
    libxml2-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

COPY php-ext/ php-ext/

RUN cd php-ext && \
    phpize && \
    ./configure --with-gomesi=/usr/local && \
    make && \
    make install

FROM php:8.3-cli-bookworm

COPY --from=ext-build /usr/local/lib/php/extensions/no-debug-non-zts-20230831/mesi.so /usr/local/lib/php/extensions/no-debug-non-zts-20230831/mesi.so
COPY --from=libgomesi-build /build/libgomesi/libgomesi.so /usr/local/lib/

RUN ldconfig

RUN docker-php-ext-enable mesi

COPY php-ext/tests/ /app/tests/

WORKDIR /app

EXPOSE 8080

COPY php-ext/tests/router.php /app/router.php

CMD ["php", "-S", "0.0.0.0:8080", "/app/router.php"]
