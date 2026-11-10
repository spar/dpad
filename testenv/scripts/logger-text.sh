#!/bin/sh
# Plain-text logger: mixed levels, warnings/errors on stderr.
# Exercises: level highlighting, stderr marker (┃), --grep.
i=0
while :; do
  i=$((i + 1))
  echo "INFO  request id=$i path=/api/orders handled in $((RANDOM % 200))ms"
  [ $((i % 5)) -eq 0 ] && echo "DEBUG cache stats hits=$((RANDOM % 100)) misses=$((RANDOM % 10))"
  [ $((i % 4)) -eq 0 ] && echo "WARN  slow query took $((1000 + RANDOM % 3000))ms table=orders" >&2
  [ $((i % 7)) -eq 0 ] && echo "ERROR failed to connect to db host=postgres attempt=$i" >&2
  sleep 1
done
