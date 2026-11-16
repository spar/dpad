# Shell session for cli.png. Argument: path to the dockernext binary.
bin="$(mktemp -d)"
ln -s "$1" "$bin/dn"
export PATH="$bin:$PATH"
p() { printf '\033[36m$\033[0m \033[1m%s\033[0m\n' "$*"; "$@"; echo; }
clear
p dn ps -a --sort status
p dn logs -n 4 dn-demo-logger-text-1 dn-demo-logger-json-1 dn-demo-logger-logfmt-1
rm -r "$bin"
exec sleep 30
