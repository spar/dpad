# Shell session for cli.png. Argument: path to the dpad binary.
bin="$(mktemp -d)"
ln -s "$1" "$bin/dp"
export PATH="$bin:$PATH"
p() { printf '\033[36m$\033[0m \033[1m%s\033[0m\n' "$*"; "$@"; echo; }
clear
p dp ps -a --sort status
p dp logs -n 4 logger-text logger-json logger-logfmt
rm -r "$bin"
exec sleep 30
