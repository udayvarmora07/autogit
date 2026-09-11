# Phase 5 rollback and disclosure implementation evidence

Status: local implementation evidence; hosted release acceptance remains open

## Scope

This evidence covers the locally actionable part of P5-07:

- private vulnerability reporting through GitHub Security Advisories;
- seven-day acknowledgement and fourteen-day classification targets;
- severity-based patch or mitigation coordination;
- publication stop, candidate quarantine, and rollback to the last approved
  release;
- keyless signing recovery as an explicit protected-environment decision;
- a deterministic technical drill that proves tampered release evidence is
  rejected and local history is preserved.

The drill is intentionally offline. It does not create, delete, or modify a
GitHub Release, tag, repository, signing identity, or user repository. It does
not replace the exact-tag hosted attestation and release-environment review.

## Technical drill

Run:

```sh
bash scripts/release-rollback-drill.sh \
  --output "$RUNNER_TEMP/autogit-release-rollback-drill.json"
```

The command creates disposable artifacts under a private temporary directory,
verifies a known-good checksum, mutates the candidate artifact, confirms the
checksum failure, stops publication, quarantines the candidate, records a
rollback to the previous approved release, and emits only fixed labels and
boolean/status fields. It refuses to overwrite an existing evidence file.

The output schema is `autogit.release-rollback-drill/1`. A passing result must
contain `status: "passed"`, `sensitive_data_recorded: false`, and passed
checks for candidate rejection, publication stop, quarantine, channel-pointer
rollback, and local-history preservation.

## Keyless signing identity recovery

AutoGit does not store a signing private key. If a release workflow or its
trust configuration is suspected of compromise, the release owner must:

1. freeze the protected `release` environment and stop all release-tag pushes;
2. preserve the failed workflow logs, artifact digests, tag, source commit,
   and attestation subjects without copying secrets or raw paths;
3. quarantine the affected release and mark its tag/artifacts untrusted;
4. review and narrow the workflow, repository, environment, and OIDC trust
   conditions before re-enabling publication;
5. require a fresh protected-environment approval and a new exact tag/commit;
6. run consumer verification against the replacement artifacts before any
   channel or cohort rollout.

Because the identity is keyless, “recovery” means recovering the trusted
workflow/environment boundary and issuing a fresh attestation; it does not
mean exporting or rotating a local private signing key.

## Tabletop acceptance record

The release owner records the incident class, affected tag/commit, publication
freeze time, quarantine result, rollback target, environment/trust decision,
replacement verification result, and reviewer/date. Evidence must remain
redacted: no credentials, prompts, source, diffs, raw URLs, or local paths.

This document and the executable drill establish the implementation contract.
P5-07 remains a release-acceptance row until a named reviewer records the
tabletop/technical result against the exact tagged release environment.
