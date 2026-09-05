# AutoGit v1 release and rollback runbook

Status: implementation artifact; alpha/beta approval pending  
Last updated: 2026-09-05

This runbook covers the bounded private-alpha and public-beta gates. It does
not authorize a live provider run or replace explicit release-owner approval.

## Current evidence snapshot

[CI run 33972129362](https://github.com/udayvarmora07/autogit/actions/runs/33972129362)
completed successfully for commit `ad0e05d79c6eda3b602ca5f98e55841683e1b3e6`.
Its native Linux, macOS, and Windows jobs passed tests, builds, deterministic
test-floor checks, benchmark sampling, and p95 gates; security analysis and
all three cross-build jobs also passed. This closes the native-OS gate only.
Phase 0 acceptance, the live disposable-provider canary, and alpha/beta
promotion remain pending.

## Release evidence checklist

Before promotion, record the commit, Go version, runner OS/architecture,
command output, and redacted artifact links for each item:

1. Run `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, and
   `go build ./...`.
2. Run `bash scripts/release-build.sh --output dist/first` twice with separate
   output directories and byte-compare matching binaries plus `SHA256SUMS`.
   CI performs this check for all six supported release targets; signing
   remains a separate release-owner step using an approved key.
3. Run the deterministic test-floor command from
   [CI](../.github/workflows/ci.yml) and attach the count.
4. [Recorded] The native Linux, macOS, and Windows matrix passed in
   [CI run 33972129362](https://github.com/udayvarmora07/autogit/actions/runs/33972129362),
   including benchmark, p95-gate, and build steps. Cross-build output alone is
   not native evidence.
5. The same run passed the native p95 gates. Retain the run logs with the
   release record; this evidence does not by itself approve alpha or beta.
6. Run the manually dispatched
   [GitHub canary](../.github/workflows/github-canary.yml) with the dedicated
   `AUTOGIT_CANARY_TOKEN` secret and owner. Confirm generated name, owner,
   visibility, `main` ref, exact commit SHA, and successful cleanup. A personal
   token or ambient `GH_TOKEN` is not acceptable.
7. Obtain product acceptance of the Phase 0 contract, threat invariants, test
   traceability, compatibility boundary, and release decision. Record the
   approver and date in the release record.

A missing, stale, or environment-only artifact leaves its gate open. A green
local test does not authorize public publication.

### Gate audit

- Phase 0 remains open: the contract-freeze record and normative documents are
  awaiting product acceptance, despite passing traceability checks.
- The disposable canary remains open: its local token-boundary test passes and
  cleanup safeguards are implemented, but there is no live run or cleanup
  artifact. It requires only the dedicated `AUTOGIT_CANARY_TOKEN`.
- Private alpha remains open: native CI and local reliability evidence pass,
  but the bounded cohort and release-owner approval are not recorded.
- Public beta remains open: it depends on those unresolved gates and has no
  live provider/public-release evidence.

## Private-alpha rollout

1. Use a dedicated disposable or explicitly enrolled private repository cohort;
   never use a developer's personal project as a fixture.
2. Keep tracking `local` or `private` by default. Public visibility requires
   separate policy and command consent plus the complete preflight report.
3. Install only owned adapter entries and save the pre-upgrade config backup.
   Run `install --list` and `doctor` before enabling a client.
4. Exercise local commit, offline push, retry, crash recovery, and uninstall
   before enrolling another repository.
5. Record incidents by repository identity, job ID, reason code, and recovery
   action; never record prompts, source, diffs, tokens, or raw URLs.

## Incident and rollback procedure

1. Stop publication by selecting `disable` or a local-only policy. Do not
   delete a repository, force-push, reset, or rewrite shared history.
2. Preserve the local commit and durable intent. Use `status`, `logs`, and
   `retry` only after confirming repository, destination, ref, and commit SHA.
3. Leave stale or failed provider jobs pending or blocked and reconcile the
   exact durable identity. Never create a replacement from ambiguous intent.
4. Restore the previous owned adapter configuration from its backup, or run
   `uninstall` with the same explicit client/path/root scope.
5. Capture the redacted error category, state revision, and recovery result.
   Escalate provider identity mismatches for manual review; never use a broad
   repository-delete pattern.

## Upgrade and compatibility procedure

- Verify the release binary and [compatibility manifest](compatibility-manifest.json)
  before installation.
- Keep wire major `autogit.event/1` and result major `autogit.result/1` stable;
  unknown future majors fail closed.
- Migrate the state store only through the bounded schema migration and back
  up the protected state directory before an upgrade.
- If migration or health checks fail, stop mutation and report `doctor` as
  unavailable rather than repairing state implicitly.
- Resume only after verifying policy revision, repository identity, pending
  intents, and adapter ownership.

## Support handoff

Every alpha/beta incident handoff includes release commit and platform,
repository identity, job/event and correlation IDs, stable reason code,
whether local work remains intact, the next safe command or human decision,
and cleanup status for any disposable provider resource. Support artifacts are
metadata-only and remain redacted.

## Security response and support triage

1. Classify reports as consent/ownership, verification/security scan, provider
   destination, durability/recovery, privacy/redaction, or release artifact.
   Treat an alleged secret disclosure, unconsented mutation, wrong destination,
   force/ref deletion, or cleanup mismatch as security-critical.
2. Stop further publication by selecting `disable` or a local-only policy.
   Preserve durable state and local commits; do not erase logs, reset history,
   delete a repository, or retry into an ambiguous destination.
3. Record only redacted metadata: release commit/platform, repository identity,
   correlation/job ID, reason code, observed outcome category, and whether
   local work remains intact. Never collect a token, prompt, source, diff, or
   raw remote URL in the incident record.
4. The release owner selects the private reporting channel and decides whether
   to pause a cohort, revoke a release artifact, or publish a security notice.
   The repository has no default public disclosure deadline or key authority.
5. Use [the release-notes template](release-notes.md) only after the response,
   compatibility, migration, and rollback outcomes are reviewed.
