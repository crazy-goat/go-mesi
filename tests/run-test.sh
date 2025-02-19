#!/usr/bin/env sh

./test-server &
export SERVER_PID=$!
./e2e fixtures
kill $SERVER_PID