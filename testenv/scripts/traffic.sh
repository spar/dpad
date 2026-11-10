#!/bin/sh
# Hits the nginx `web` service: normal pages, a 5MB download, and 404s
# (which nginx logs as [error] on stderr).
# Exercises: NET I/O column, `--sort net`, nginx-style log levels in `web`.
while :; do
  wget -qO /dev/null http://web/ || echo "WARN web not reachable"
  wget -qO /dev/null http://web/big.bin
  wget -qO /dev/null "http://web/missing-$((RANDOM % 1000))" 2>/dev/null
  sleep 1
done
