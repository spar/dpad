#!/bin/sh
# Writes and reads a 64MB file every few seconds.
# Exercises: BLOCK I/O column and `--sort io`.
while :; do
  dd if=/dev/zero of=/data/blob bs=1M count=64 conv=fsync 2>/dev/null
  cat /data/blob > /dev/null
  rm -f /data/blob
  echo "INFO wrote and read 64MB"
  sleep 3
done
