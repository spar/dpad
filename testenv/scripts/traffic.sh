#!/bin/sh
# Hits the bookshop's nginx `web` service: normal pages, a 5MB download, and
# now and then a bot probing for files that aren't there (404s, which nginx
# logs as [error] on stderr).
# Exercises: NET I/O column, `--sort net`, nginx-style log levels in `web`.
i=0
while :; do
  i=$((i + 1))
  wget -qO /dev/null http://web/ || echo "WARN web not reachable"
  wget -qO /dev/null http://web/big.bin
  if [ $((i % 6)) -eq 0 ]; then
    set -- /wp-login.php /.env /admin/config.php /.git/config /phpmyadmin/
    shift $(( (i / 6) % 5 ))
    wget -qO /dev/null "http://web$1" 2>/dev/null
  fi
  sleep 1
done
