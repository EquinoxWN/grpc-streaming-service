#!/usr/bin/env bash
# Starts ticker-server on loopback, calls the unary, server-streaming and bidirectional endpoints
# with tickerctl, and always stops the server.
set -euo pipefail
cd "$(dirname "$0")/.."
EXE=; [ -x .tmp/bin/ticker-server.exe ] && EXE=.exe
ADDR=${ADDR:-127.0.0.1:50551}

.tmp/bin/ticker-server$EXE -addr "$ADDR" -seed 1 -step 50ms 2> .tmp/demo-server.log &
PID=$!
trap 'kill $PID 2>/dev/null || true; wait $PID 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
  .tmp/bin/tickerctl$EXE -addr "$ADDR" -timeout 1s quote ACME > /dev/null 2>&1 && break
  sleep 0.1
done

run() { echo "\$ tickerctl $*"; .tmp/bin/tickerctl$EXE -addr "$ADDR" "$@"; }
run quote ACME
run stream -symbols ACME,FLUX -interval-ms 100 -n 6
run watch -add ACME,BLUE -then-add CRNA -then-drop ACME -interval-ms 100 -n 4
echo "\$ tickerctl quote NOPE"
.tmp/bin/tickerctl$EXE -addr "$ADDR" quote NOPE 2>&1 || true
echo "\$ tickerctl -timeout 300ms stream -symbols HALO -n 0   # no limit: the client deadline ends it"
.tmp/bin/tickerctl$EXE -addr "$ADDR" -timeout 300ms stream -symbols HALO -interval-ms 100 -n 0 2>&1 || true
echo "RESULT demo: unary, server-streaming and bidirectional calls answered over loopback TCP"
