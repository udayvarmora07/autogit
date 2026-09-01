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
safe local operations (`install`, `doctor`, `enable`, `disable`, `status`,
`plan`, `hook`, `logs`, `uninstall`, and `config explain`); `verify`, `sync`,
and `retry` return an explicit unsupported result. The foundation currently
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
canonical-URL binding, and a production bounded process runner are present as
tested libraries. The CLI exposes safe local policy, hook, status, plan, logs,
doctor, and install/uninstall slices; `verify`, `sync`, and `retry` return an
explicit unsupported result, and the complete CLI publication/provider
workflow remains unwired. No command in this foundation contacts GitHub or
modifies a user repository implicitly; provider tests/canaries are not live.

### Verification and remaining release gates

Fresh repository-wide `go test ./...`, `go vet ./...`, `go build ./...`, and
Linux arm64, Darwin arm64, and Windows amd64 cross-build smoke checks pass
locally. Native macOS and Windows CI has not yet been observed. The repository
does not contain the prototype shell scripts used for the documented 177-case
baseline, and the full `>=609` release-test target, complete CLI workflow,
provider canary, and publication/recovery release gates remain open. The CI
workflow defines the intended native matrix but does not itself constitute
proof that those release gates have passed.
