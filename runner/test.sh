#!/usr/bin/env bash
# Exercises registration persistence and cleanup without contacting GitHub.
set -euo pipefail

runner_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT

export RUNNER_HOME="$test_dir/home"
export RUNNER_STATE_DIR="$test_dir/state"
export RUNNER_WORK_DIR="$test_dir/work"
export RUNNER_REPOSITORY_URL=https://github.com/example/repository
export RUNNER_NAME=runner-test
export RUNNER_LABELS=dockernext
export RUNNER_TEST_LOG="$test_dir/lifecycle.log"
mkdir -p "$RUNNER_HOME" "$RUNNER_STATE_DIR" "$RUNNER_WORK_DIR"

cat > "$RUNNER_HOME/config.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == *"--url $RUNNER_REPOSITORY_URL"* ]]
[[ "$*" == *"--name $RUNNER_NAME"* ]]
[[ "$*" == *"--work $RUNNER_WORK_DIR"* ]]
[[ "$*" == *"--labels dockernext"* ]]
[[ "$*" == *"--token test-registration-token"* ]]
echo registered >> "$RUNNER_TEST_LOG"
for file in .runner .credentials .credentials_rsaparams; do
    printf 'saved-registration\n' > "$file"
done
SH

cat > "$RUNNER_HOME/run.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ ${RUNNER_TOKEN+x} != x ]]
for file in .runner .credentials .credentials_rsaparams; do
    [[ $(cat "$file") == saved-registration ]]
done
echo started >> "$RUNNER_TEST_LOG"
SH
chmod +x "$RUNNER_HOME/config.sh" "$RUNNER_HOME/run.sh"

if RUNNER_TOKEN= bash "$runner_dir/entrypoint.sh" > "$test_dir/missing-token.log" 2>&1; then
    echo 'Unregistered runner started without a token' >&2
    exit 1
fi
grep -q 'not registered yet' "$test_dir/missing-token.log"
[[ ! -f "$RUNNER_TEST_LOG" ]]

RUNNER_TOKEN=test-registration-token bash "$runner_dir/entrypoint.sh"
for file in .runner .credentials .credentials_rsaparams; do
    [[ $(cat "$RUNNER_STATE_DIR/$file") == saved-registration ]]
    rm "$RUNNER_HOME/$file"
done

RUNNER_TOKEN= bash "$runner_dir/entrypoint.sh"
[[ $(grep -c '^registered$' "$RUNNER_TEST_LOG") == 1 ]]
[[ $(grep -c '^started$' "$RUNNER_TEST_LOG") == 2 ]]

# Recreating a container gives it a fresh runner home but the same state volume.
mkdir "$test_dir/recreated-home"
cp "$RUNNER_HOME/config.sh" "$RUNNER_HOME/run.sh" "$test_dir/recreated-home/"
RUNNER_HOME="$test_dir/recreated-home" RUNNER_TOKEN= bash "$runner_dir/entrypoint.sh"
[[ $(grep -c '^registered$' "$RUNNER_TEST_LOG") == 1 ]]
[[ $(grep -c '^started$' "$RUNNER_TEST_LOG") == 3 ]]

rm "$RUNNER_STATE_DIR/.credentials_rsaparams"
if RUNNER_TOKEN= bash "$runner_dir/entrypoint.sh" > "$test_dir/partial-state.log" 2>&1; then
    echo 'Incomplete saved registration was accepted' >&2
    exit 1
fi
[[ $(grep -c '^started$' "$RUNNER_TEST_LOG") == 3 ]]

mkdir -p "$RUNNER_WORK_DIR/_tool" "$RUNNER_WORK_DIR/_cache" "$RUNNER_HOME/_diag" "$test_dir/other-runner/_cache" "$test_dir/bin"
echo sdk > "$RUNNER_WORK_DIR/_tool/sdk"
echo module > "$RUNNER_WORK_DIR/_cache/module"
mkdir "$RUNNER_WORK_DIR/_cache/readonly-module"
echo source > "$RUNNER_WORK_DIR/_cache/readonly-module/source.go"
chmod 555 "$RUNNER_WORK_DIR/_cache/readonly-module"
echo checkout > "$RUNNER_WORK_DIR/checkout"
echo other > "$test_dir/other-runner/_cache/keep"
touch "$RUNNER_HOME/_diag/old.log" "$RUNNER_HOME/_diag/new.log"
touch -t 202001010000 "$RUNNER_HOME/_diag/old.log"

cat > "$test_dir/bin/docker" <<'SH'
#!/usr/bin/env bash
echo docker-called >> "$RUNNER_TEST_LOG"
echo 'Cleanup must not use the server Docker daemon' >&2
exit 99
SH
chmod +x "$test_dir/bin/docker"
export PATH="$test_dir/bin:$PATH"

RUNNER_CACHE_LIMIT_GB=10 bash "$runner_dir/job-completed.sh"
[[ -f "$RUNNER_WORK_DIR/_tool/sdk" && -f "$RUNNER_WORK_DIR/_cache/module" ]]
[[ ! -e "$RUNNER_HOME/_diag/old.log" && -f "$RUNNER_HOME/_diag/new.log" ]]
RUNNER_CACHE_LIMIT_GB=invalid bash "$runner_dir/job-completed.sh"
[[ -f "$RUNNER_WORK_DIR/_cache/module" ]]
RUNNER_CACHE_LIMIT_GB=0 bash "$runner_dir/job-completed.sh"
[[ ! -e "$RUNNER_WORK_DIR/_tool" && ! -e "$RUNNER_WORK_DIR/_cache" ]]
[[ -f "$RUNNER_WORK_DIR/checkout" && -f "$test_dir/other-runner/_cache/keep" ]]
! grep -q '^docker-called$' "$RUNNER_TEST_LOG"

echo 'Runner lifecycle and isolated cache cleanup checks passed.'
