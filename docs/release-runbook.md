# AutoGit v1 release and rollback runbook

Status: implementation artifact; alpha/beta approval pending  
Last updated: 2026-09-05

This runbook covers the bounded private-alpha and public-beta gates. It does
not authorize a live provider run or replace explicit release-owner approval.

## Release evidence checklist

Before promotion, record the commit, Go version, runner OS/architecture,
command output, and redacted artifact links for each item:

1. Run `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, and
   `go build ./...`.
2. Run the deterministic test-floor command from
   [CI](../.github/workflows/ci.yml) and attach the count.
3. Run the native Linux, macOS, and Windows matrix and retain benchmark,
   p95-gate, and build logs. Cross-build output alone is not native evidence.
4. Run `bash scripts/performance-gate.sh` on each supported native runner and
   retain the no-candidate-hook and 100,000-path p95 values.
5. Run the manually dispatched
   [GitHub canary](../.github/workflows/github-canary.yml) with a dedicated
   token and owner. Confirm generated name, owner, visibility, `main` ref,
   exact commit SHA, and successful cleanup.
6. Obtain product acceptance of the Phase 0 contract, threat invariants, test
   traceability, compatibility boundary, and release decision. Record the
   approver and date in the release record.

A missing, stale, or environment-only artifact leaves its gate open. A green
local test does not authorize public publication.

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

- Verify the release binary and compatibility manifest before installation.
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
