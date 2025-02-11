#!/usr/bin/env sh
cp nginx.conf build/nginx/
cd build/nginx || exit

GODEBUG=gctrace=1  ./nginx -p . -c nginx.conf -g "daemon off;"
