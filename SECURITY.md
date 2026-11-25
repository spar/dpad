# Security policy

## Supported versions

Only the latest release receives security fixes. Upgrade to it before
reporting.

## Reporting a vulnerability

Please report vulnerabilities privately through
[GitHub's private vulnerability reporting](https://github.com/spar/dpad/security/advisories/new),
not in a public issue. Include the dpad version (`dp --version`), your OS and
Docker version, and the steps to reproduce.

When a fix is released, the advisory is published with credit to the reporter
unless you ask otherwise.

## Scanning

CI runs [govulncheck](https://go.dev/doc/security/vuln/) on every push and
weekly, Dependabot keeps Go modules and GitHub Actions current, and CodeQL scans
the code.
