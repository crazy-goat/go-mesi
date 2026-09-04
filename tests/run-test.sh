#!/usr/bin/env sh
./test-server &
SERVER_PID=$!
./e2e fixtures
status=$?
kill $SERVER_PID
exit $status
