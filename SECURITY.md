# Security policy

AutoGit treats untrusted hooks, repositories, verifiers, provider responses,
and configuration as security boundaries. Please do not disclose a suspected
credential leak, unconsented mutation, wrong-repository/ref/visibility effect,
destructive operation, or release-artifact compromise in a public issue.

Report vulnerabilities through a private GitHub Security Advisory for
`udayvarmora07/autogit`. Include the affected release or commit, operating
system/architecture, stable error code, and a minimal reproduction. Do not
include source, prompts, diffs, credentials, raw URLs, or personal data.

Supported security versions are the latest tagged `v1.x` release and the
current `main` branch. The maintainer will acknowledge a report within seven
days, classify it within fourteen days, and coordinate a patch or mitigation
according to severity. These targets are support commitments, not a promise
that every report is accepted as a vulnerability.

Until a release is explicitly signed and published, binaries built from this
repository are development artifacts and must not be trusted as release
provenance.
