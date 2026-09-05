# AutoGit release notes template

Status: unreleased template; publication requires release-owner approval

Use this template only after the release evidence checklist has passed. Replace
every bracketed field before publication. Do not include source, prompts,
diffs, credentials, tokens, raw remotes, or disposable-canary credentials.

## [Version] — [Release date]

### Verification

- Release commit: `[full commit SHA]`
- Supported binaries: `[artifact names and checksums]`
- Compatibility: [compatibility manifest](compatibility-manifest.json)
- CI/release evidence: `[redacted run or artifact links]`
- Canary: `[exact disposable identity and cleanup status]`

### Highlights

- `[User-visible change with a linked issue or commit]`

### Upgrade and rollback

- Wire compatibility remains `autogit.event/1` and `autogit.result/1` unless
  this release explicitly records a future-major migration window.
- Confirm the state schema version in the compatibility manifest before
  upgrade. Migrate forward only; reject a future state schema rather than
  downgrading or repairing it implicitly.
- To roll back, disable publication first, preserve the local commit and
  durable intent, then restore the previous owned adapter configuration.

### Known limitations

- Public publication remains explicit and separate from tracking consent.
- An uncertain provider result remains pending/blocked until reconciliation
  proves the exact destination, ref, and commit SHA.

### Security and support

- Report a suspected secret disclosure, unconsented mutation, wrong destination,
  or destructive operation through the release-owner’s private reporting path.
- Include only the redacted repository identity, release commit/platform,
  correlation or job ID, stable reason code, and whether local work remains
  intact. Do not attach raw source or credentials.
