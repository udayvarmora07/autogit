# ADR-0011: Typed GitHub REST transport and explicit provider identity

Status: accepted for the local provider boundary; live provider canary remains
a release-gate requirement.

## Context

The legacy `gh` adapter is useful for local bootstrap, but ambient `gh` account
state and `GH_TOKEN`/`GITHUB_TOKEN` can silently target the wrong host or owner.
Provider responses also need bounded, redacted handling and stable error
categories so durable reconciliation does not depend on CLI text.

## Decision

AutoGit provides a standard-library-only typed REST boundary in
`internal/provider/github_rest.go`.

- The API version, HTTPS base URL, host, authenticated account, and owner are
  explicit constructor inputs.
- The transport pins the current `2026-03-10` REST version; changing it is a
  compatibility event requiring an upstream breaking-change review.
- The transport accepts an explicit in-memory token source only. It never reads
  `GH_TOKEN`, `GITHUB_TOKEN`, `gh` config, or other ambient credentials.
- Every request uses direct HTTP, bounded response reads, GitHub request IDs,
  conditional `ETag` reads, pagination links, retry/rate-limit metadata, and
  redacted typed status errors.
- Repository mutations verify the authenticated account and configured owner;
  repository identity and visibility are checked before success is reported.
- GitHub App installation tokens use an in-memory RSA key, repository IDs, and
  permission scope. Tokens refresh before expiry and are never written to
  AutoGit state.
- Enterprise hosts must negotiate a version inside the configured support
  window. Public `github.com` does not require an Enterprise version header.
  The exact window and release evidence are maintained in
  [provider support policy](../provider-support.md).
- Check Runs are an optional, bounded projection tied to an exact head SHA.
  They do not replace AutoGit's durable evidence or consent model.
- The existing `gh` adapter remains an optional bootstrap path until the
  disposable REST canary and provider-selection UX are complete.

## Consequences

The provider boundary is deterministic and testable without a live account,
and an ambient account cannot silently widen a destination. A REST transport
does not itself push Git objects, so exact local Git push behavior remains in
the existing `Pusher` port. Live GitHub canary evidence, App permission review,
and broader Enterprise version coverage remain required before Phase 2 exit.
