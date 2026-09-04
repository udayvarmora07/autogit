# AutoGit

AutoGit is a consent-based Git automation tool for AI-assisted development.
It preserves useful work automatically while preventing unrelated, unverified,
or sensitive changes from being published.

This directory contains the AutoGit v1 design. The currently installed Bash
prototype remains under `~/.agents/hooks` and is treated as a behavioral
reference until the v1 engine can replace it safely.

## Phase 0 status

Phase 0 defines the product and safety contract before implementation:

- [Product requirements](docs/product-requirements.md)
- [Threat model](docs/threat-model.md)
- [Canonical event contract](docs/event-contract.md)
- [Lifecycle and publication policy](docs/lifecycle.md)
- [Test strategy and release gates](docs/test-strategy.md)
- [Implementation plan](docs/implementation-plan.md)
- [Architecture decisions](docs/adr/README.md)

The implementation target is a Go modular monolith with versioned adapters for
Codex, Claude Code, Cursor, Gemini CLI, OpenCode, and CommandCode. AutoGit uses
the system `git` executable and initially uses GitHub CLI (`gh`) for GitHub
authentication and repository operations.

## Product promise

> AutoGit never publishes a change merely because an AI response stopped. It
> publishes only a consented, attributable change set after its configured
> safety and verification policy has passed.

## Implementation status (local v1 foundation)

`go run ./cmd/autogit` provides the versioned hook result contract and the
safe local operations (`install`, `doctor`, `enable`, `disable`, `init`, `status`,
`plan`, `hook`, `logs`, `uninstall`, `config explain`, baseline-capturing
`sync` (plus explicit clean-session `sync --complete`), clean-session read-only
`verify`, explicit private `publish`, guarded `retry`, and explicit
`remote create`). `verify` requires explicit
repository/session/path/message/verifier inputs and never commits or updates
refs. `sync --complete` creates only an AutoGit-owned local commit ref after
trusted verification; `sync --complete --all-owned` can resume a hook-captured
session through source-free baseline fingerprints without persisting raw paths
or source bytes. The foundation currently
includes strict bounded event decoding
(including duplicate-key and replay/conflict handling), permission-restricted
SQLite receipts and causal buffering, policy defaults, canonical repository
identity, bounded argument-array process execution, exact SHA/ref push
argument construction, Conventional Commit validation, and redacted security
findings.

Candidate ownership/index primitives, bounded verification, read-only history
scanning, lifecycle projection, durable git commit intent/reconciliation,
diagnostics, six adapter translators, an adapter contract matrix, owned config
installation, a pure local public preflight package, provider remote-alias to
canonical-URL binding, a consent-gated repository initializer, and a
production bounded process runner are present as tested libraries. The session
package provides both an in-memory start/complete handoff and a source-free
cross-process restart handoff into the verified local workflow, while the CLI
hook captures trusted session-start baselines without exposing their contents.
The CLI private
publication path is explicit and exact-SHA based; public publication returns a
bounded preflight report until all evidence is available. Fully automatic
lifecycle-driven commit orchestration remains open. No command contacts GitHub or modifies a user
repository implicitly; provider tests/canaries are not live.

Initialization is explicit. For a local project use
`autogit init --repo DIR --local --branch main`; remote tracking requires
`--provider github --owner OWNER --name NAME` and defaults to private. Use
`--public-consent --visibility public` only when public tracking is intended.
Add `--dry-run` to inspect the canonical root, branch, policy, and hygiene
changes without creating state or Git metadata.
For a hook-captured session, `sync --complete --all-owned` resumes ownership
from the source-free baseline manifest; explicit `--path` remains available
when the caller wants to limit the candidate.
After initialization, `autogit remote create` is the separate resumable step
that creates and binds the hosted destination.

`autogit plan --repo DIR` is read-only and reports the observed `HEAD`, shared
index/status/path digests, changed-path count, and consent/provider checks. It
does not stage, commit, move refs, or alter the shared index.
`autogit config explain` is also state-free and can inspect a verifier file
without initializing AutoGit storage.

### Verification and remaining release gates

Fresh repository-wide `go test ./...`, `go vet ./...`, `go build ./...`, and
Linux arm64, Darwin arm64, and Windows amd64 cross-build smoke checks pass
locally. Native macOS and Windows CI has not yet been observed. The repository
does not contain the prototype shell scripts used for the documented 177-case
baseline, and the full `>=609` release-test target, complete CLI workflow,
provider canary, and publication/recovery release gates remain open. The CI
workflow defines the intended native matrix but does not itself constitute
proof that those release gates have passed.
