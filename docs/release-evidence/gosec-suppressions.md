# Gosec suppression register

Recorded: 2026-09-07
Owner for all entries: Uday Varmora
Review trigger: remove the suppression if the named boundary, input source, or
Git compatibility requirement changes; otherwise review on every dependency
or security-tool upgrade.

These are the 22 line-scoped suppressions used by the pinned gosec job. They
are false positives at deliberately bounded compatibility or security
boundaries, not global rule exclusions.

| Rule | Location / boundary | Rationale | Linked test evidence |
| --- | --- | --- | --- |
| G101 (3) | `internal/security/security.go`, stable `Reason*` constants | The strings are redaction reason codes, never credentials. | `internal/security/security_test.go`: `TestScanFindsSecretsWithoutReturningSecretValue`, `TestRedactRemovesCredentialsURLsAndControlCharacters` |
| G204 | `internal/process/process.go`, `exec.Command` | The owning caller validates the executable and argv before the bounded runner executes them. | `internal/process/process_test.go`: deadline, output-limit, and concurrent-stream tests; process-tree tests on Unix/Windows |
| G505/G401 | `internal/historyscan/historyscan.go`, SHA-1 compatibility paths | SHA-1 is required to inspect legacy Git object IDs; it is not used as a password or security digest. | `internal/historyscan/historyscan_test.go`: `TestHistoryScanFindsSecretRemovedFromCandidate`, `TestHistoryScanUsesControlledReadOnlyGitCommands`, `TestHistoryScanFailsClosedOnLimitsOutputAndObjectSubstitution` |
| G304 | `internal/verification/policy.go`, `executableFingerprint` | The verifier selects the path from its trusted, identity-checked executable boundary. | `internal/verification/policy_test.go`: executable rejection/replacement and evidence-binding tests |
| G304 | `internal/staging/staging.go`, observed-file open | The path is built from a validated repository root and validated relative path, with replacement checks immediately before opening. | `internal/staging/staging_test.go`: symlink, component-swap, and replacement-during-read tests |
| G304 (3) | `internal/repository/observation.go`, Git index and repository-file reads | Git metadata and observed files remain inside the canonical repository boundary and are checked for safe kind/replacement. | `internal/repository/observation_test.go`: path-escape, symlink, replacement, linked-worktree, and baseline-capture tests |
| G304 (2) | `internal/repository/initialize.go`, fixed `.gitignore`/`README.md` children | Initialization reads only fixed children of the canonical root after bounded metadata checks. | `internal/repository/initialize_test.go`: hygiene failure, concurrent-change, and initialization tests |
| G304 (3) | `internal/install/install.go`, client config reads/creates and directory sync | Config paths are constrained to approved roots and checked for ownership/symlinks before use. | `internal/install/install_test.go`: outside-root, symlink-escape, atomic backup, changed-config, and parent-swap tests |
| G304 (2) | `internal/install/client_install.go`, client config reads | The cleaned paths are constrained to caller-approved client configuration roots. | `internal/install/client_install_test.go`: schema, idempotence, unsupported-client, and preservation tests |
| G304 (2) | `internal/gittransaction/transaction.go`, Git index reads | The index path comes from validated Git state and is used only within the canonical repository transaction boundary. | `internal/gittransaction/transaction_test.go`: HEAD/index-change, unsafe-input, exact-snapshot, and concurrent-index tests |
| G304 | `internal/db/database.go`, state-file creation | The absolute path is canonicalized and its existing ancestors are verified before exclusive creation. | `internal/db/database_test.go`: symlinked state/WAL rejection, cancellation, read-only, and serialized-open tests |
| G703 | `internal/repository/repository.go`, linked-worktree `commondir` read | Git-resolved metadata is constrained and validated before the linked-worktree path is accepted. | `internal/repository/repository_test.go`: canonical identity, hardened linked-worktree discovery, and cancellation tests |
