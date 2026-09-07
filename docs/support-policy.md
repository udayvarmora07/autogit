# Support policy

AutoGit v1 supports the latest tagged v1 release and the current `main`
branch. Support is local-first: a support report must contain only the
release commit, platform, redacted repository identity, operation/correlation
ID, stable reason code, observed outcome category, and whether local work is
intact.

Do not attach prompts, transcripts, source, diffs, raw paths, credentials,
remote URLs, or provider tokens. Provider and adapter behavior is supported
only for the versions listed in [`compatibility-manifest.json`](compatibility-manifest.json).
Observation-only adapters do not authorize mutation.

For suspected security issues, use the private process in [SECURITY.md](../SECURITY.md).
For operational failures, preserve the local commit and durable state, disable
publication if necessary, and follow the [release runbook](release-runbook.md).

The maintainer reviews support reports within seven days. Critical safety
reports are triaged as release blockers; no support response may ask a user to
reset, force-push, delete a repository, or disclose a secret as a first step.
