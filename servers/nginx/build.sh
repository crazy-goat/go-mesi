#!/usr/bin/env sh

NGINX_VERSION="1.27.4"
rm -rf build
mkdir -p build
mkdir -p build/mesi
cp ../../libgomesi/libgomesi.h build/mesi
cp ngx_http_mesi_module.c config build/mesi

cd build

wget http://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz
tar -xzf nginx-${NGINX_VERSION}.tar.gz
rm nginx-${NGINX_VERSION}.tar.gz
cd nginx-${NGINX_VERSION}

./configure --add-dynamic-module=../mesi --with-ld-opt=-lgomesi
make modules

cp objs/ngx_http_mesi_module.so ../../
cd ../../
rm -rf build