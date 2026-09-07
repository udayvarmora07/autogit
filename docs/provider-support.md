# GitHub provider support policy

Status: local provider boundary accepted; live provider and permission review
remain release-gate evidence.

This policy is deliberately narrower than “GitHub-compatible.” AutoGit binds
every operation to an explicit API base URL, host, authenticated account, and
owner. It never uses `GH_TOKEN`, `GITHUB_TOKEN`, `gh` account state, or a
credential helper to choose a destination.

## GitHub REST API

The typed transport pins `X-GitHub-Api-Version: 2026-03-10`, the current
version on 2026-09-07. GitHub documents the older `2022-11-28` version as
supported through 2028-03-10, but AutoGit does not silently fall back; a future
upgrade requires reviewing the upstream breaking-change notice and refreshing
the contract fixtures.

The public API root is `https://api.github.com/`. A custom root is accepted
only when it is explicit, has no query or fragment, and is HTTPS unless a
loopback-only test opts into HTTP.

## GitHub Enterprise Server

Enterprise support is capability-negotiated, not inferred from the hostname.
The server must return an installed version from the metadata endpoint, and
the major version must be in the configured inclusive window. The default
window is major versions 3 through 4; unknown, missing, malformed, and
out-of-window versions fail closed. Minor-version compatibility remains an
explicit release-matrix item and is not implied by the major window.

The API root, host, account, owner, and repository identity remain bound to
the same provider instance. Enterprise repository URLs are not interchangeable
with public GitHub URLs.

## GitHub App installation tokens

Automation uses an in-memory RSA key and short-lived installation tokens. The
token request must name one to 500 repository IDs and at least one explicit
`read` or `write` permission; omitting either scope is rejected because GitHub
otherwise grants the installation-wide default scope. Tokens are refreshed
when they are within five minutes of expiry and can be cleared after a
revocation response. Token material and key bytes are never written to
AutoGit state.

Check Run projection requires a credential with the provider's Checks write
permission. It is optional, bounded, redacted, tied to one exact head SHA and
evidence ID, and never becomes AutoGit's durable source of truth.

## Evidence required for promotion

Local `httptest` contract tests prove transport bounds, identity binding,
pagination, reconciliation, token refresh, Enterprise negotiation, and Check
Run redaction. Promotion additionally requires a disposable private GitHub
canary, exact permission review, native artifact execution, and cleanup
evidence retained for the exact candidate.

