# Changelog

All notable changes will be documented here. The project is unreleased; no
tagged binary or package channel is currently endorsed.

## Unreleased

- Added named presubmit, core, race, integration, soak, fuzz, canary, and
  release verification suites.
- Added built-artifact smoke and final-state scenario evaluation.
- Added compatibility-window validation and machine-readable evidence tooling.
- Added repository governance, security reporting, support, and contribution
  policies.
- Added deterministic winget manifest generation (three-file 1.12.0
  portable-installer layout) to the release package metadata, with an
  attestation-time file-count check and publication-time byte comparison.
- Hardened the publication boundary: every configured push URL is enumerated
  before an exact-SHA push, TLS verification and helper transports are pinned
  in the hardened git argv, and commit validation denies invented
  issue-closure and provenance trailers.
- Bounded the external process wait when a detached descendant holds the
  output pipes, and recorded the process-bounded containment limitation in
  the isolation capability and ADR-0010.
- Confined configuration installs whose target parent does not exist yet to
  the approved root.
