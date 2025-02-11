#!/usr/bin/env sh

cd build/nginx || exit
GODEBUG=gctrace=1  ./nginx -p . -c ../../nginx.conf -g "daemon off;"
