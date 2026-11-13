# Same pinned base and registration entrypoint as spar/expense-tracker/runner.
FROM ghcr.io/actions/actions-runner:2.338.2

USER root

# Go's race detector uses CGO, so CI needs a C compiler and libc headers.
# actions/setup-go installs the requested SDK into each runner's tool cache.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /usr/local/bin/runner-entrypoint.sh
COPY job-completed.sh /usr/local/bin/runner-job-completed.sh
ENV ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/usr/local/bin/runner-job-completed.sh

RUN chmod 755 /usr/local/bin/runner-entrypoint.sh /usr/local/bin/runner-job-completed.sh \
    && mkdir -p /runner-state && chown runner:runner /runner-state

USER runner
ENTRYPOINT ["/usr/local/bin/runner-entrypoint.sh"]
