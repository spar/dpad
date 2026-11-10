#!/bin/sh
# logfmt logger (key=value), as emitted by Go/logrus/slog services.
# Exercises: logfmt level= detection.
i=0
levels="info info info debug warn info error"
while :; do
  i=$((i + 1))
  n=$(( (i % 7) + 1 ))
  lvl=$(echo $levels | cut -d' ' -f$n)
  echo "time=$(date -u +%Y-%m-%dT%H:%M:%SZ) level=$lvl msg=\"job processed\" job_id=$i duration_ms=$((RANDOM % 900))"
  sleep 1.5
done
