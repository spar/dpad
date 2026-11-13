#!/usr/bin/env bash
# Starts one GitHub Actions runner. The first start registers it with RUNNER_TOKEN (a registration token, which GitHub accepts for one hour)
# and saves the registration in RUNNER_STATE_DIR. Every later start, including after the container is recreated, uses the saved
# registration, so no token is needed again: the runner keeps its own credentials and renews its sessions with GitHub by itself.
set -euo pipefail

: "${RUNNER_REPOSITORY_URL:?Set RUNNER_REPOSITORY_URL to the repository, such as https://github.com/owner/repo.}"
: "${RUNNER_NAME:?Set RUNNER_NAME to a name that no other runner of the repository has.}"
: "${RUNNER_WORK_DIR:?Set RUNNER_WORK_DIR to the work folder, mounted at the same path on the host (see docker-compose.yml).}"
RUNNER_HOME="${RUNNER_HOME:-/home/runner}"
STATE_DIR="${RUNNER_STATE_DIR:-/runner-state}"
STATE_FILES=(.runner .credentials .credentials_rsaparams)

# Folders Docker made for a bind mount belong to root; the runner user needs to write in them.
for dir in "$STATE_DIR" "$RUNNER_WORK_DIR"; do
    mkdir -p "$dir" 2>/dev/null || sudo mkdir -p "$dir"
    [[ -w "$dir" ]] || sudo chown "$(id -u):$(id -g)" "$dir"
done

has_saved_registration() {
    local file
    for file in "${STATE_FILES[@]}"; do [[ -f "$STATE_DIR/$file" ]] || return 1; done
}

cd "$RUNNER_HOME"
if has_saved_registration; then
    echo "Starting runner $RUNNER_NAME with its saved registration."
    for file in "${STATE_FILES[@]}"; do cp -f "$STATE_DIR/$file" "$RUNNER_HOME/$file"; done
else
    if [[ -z "${RUNNER_TOKEN:-}" ]]; then
        echo "Runner $RUNNER_NAME is not registered yet, and RUNNER_TOKEN is empty." >&2
        echo "Get a registration token (it lasts one hour) and start again with RUNNER_TOKEN set. See docs/self-hosted-runner.md." >&2
        exit 1
    fi
    echo "Registering runner $RUNNER_NAME with $RUNNER_REPOSITORY_URL."
    labels=()
    [[ -n "${RUNNER_LABELS:-}" ]] && labels=(--labels "$RUNNER_LABELS")
    # --replace takes over a registration with the same name, such as one whose saved state was lost.
    ./config.sh --unattended --replace --url "$RUNNER_REPOSITORY_URL" --token "$RUNNER_TOKEN" \
        --name "$RUNNER_NAME" --work "$RUNNER_WORK_DIR" "${labels[@]}"
    for file in "${STATE_FILES[@]}"; do cp -f "$RUNNER_HOME/$file" "$STATE_DIR/$file"; done
    echo "Saved the registration of $RUNNER_NAME. RUNNER_TOKEN is not needed from now on."
fi

# Jobs inherit this environment, so the registration token is not passed on to them.
unset RUNNER_TOKEN
exec ./run.sh
