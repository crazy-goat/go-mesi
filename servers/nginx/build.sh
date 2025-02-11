#!/usr/bin/env sh

NGINX_VERSION="${1:-1.27.4}"
BUILD_DIR="build"
DOWNLOAD_DIR="/tmp/nginx_mesi"
SRC_DIR="$BUILD_DIR/nginx-${NGINX_VERSION}"
NGINX_TAR="$DOWNLOAD_DIR/nginx-${NGINX_VERSION}.tar.gz"
MESI_DIR="$BUILD_DIR/mesi"
BUILD_MODULE_DIR="$BUILD_DIR/nginx/modules"
rm -rf "$BUILD_DIR"
mkdir -p "$DOWNLOAD_DIR" "$MESI_DIR"
mkdir -p "$BUILD_MODULE_DIR"


cp ../../libgomesi/libgomesi.h "$MESI_DIR"
cp ngx_http_mesi_module.c config "$MESI_DIR"

if [ ! -f "$NGINX_TAR" ]; then
    wget "http://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz" -O "$NGINX_TAR"
fi

mkdir -p "$SRC_DIR"
tar -xzf "$NGINX_TAR" -C "$BUILD_DIR"
mv "$BUILD_DIR/nginx-${NGINX_VERSION}" "$SRC_DIR"

cd "$SRC_DIR"
./configure --add-dynamic-module=../mesi
make -j8

cp objs/ngx_http_mesi_module.so ../../"$BUILD_MODULE_DIR"
cp objs/nginx "../../$BUILD_DIR/nginx/"
cp conf/mime.types "../../$BUILD_DIR/nginx/"

cd ../../
rm -rf "$SRC_DIR" "$MESI_DIR"
