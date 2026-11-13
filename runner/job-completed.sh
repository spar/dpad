#!/usr/bin/env bash
# Adapted from Expense Tracker's runner hook. Only this runner's caches and
# week-old diagnostic logs are touched; the server's Docker is not used.
# Cleanup must not turn a successful job into a failure.
set -uo pipefail

limit_gb="${RUNNER_CACHE_LIMIT_GB:-10}"
runner_home="${RUNNER_HOME:-/home/runner}"

# Paths come from this runner's work directory, so cleanup cannot remove a
# shared SDK or another runner's files. Keep these caches between normal jobs.
if [[ -n "${RUNNER_WORK_DIR:-}" && "$RUNNER_WORK_DIR" != "/" ]]; then
    caches=()
    for dir in "$RUNNER_WORK_DIR/_tool" "$RUNNER_WORK_DIR/_cache"; do
        [[ -d "$dir" ]] && caches+=("$dir")
    done
    if ((${#caches[@]} > 0)) && [[ "$limit_gb" =~ ^[0-9]+$ ]]; then
        used_kb=$(timeout 120 du -sk "${caches[@]}" 2>/dev/null | awk '{ total += $1 } END { print total + 0 }')
        if awk -v used="$used_kb" -v limit="$limit_gb" 'BEGIN { exit !(used > limit * 1024 * 1024) }'; then
            echo "Runner caches use $((used_kb / 1024)) MB, over the limit of ${limit_gb} GB; clearing them."
            # Go normally makes downloaded module directories read-only.
            chmod -R u+w "${caches[@]}" 2>/dev/null
            rm -rf "${caches[@]}"
        fi
    fi
fi

find "$runner_home/_diag" -type f -mtime +7 -delete 2>/dev/null
exit 0
