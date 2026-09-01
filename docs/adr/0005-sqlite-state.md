# ADR-0005: SQLite durable local state

- **Status:** Proposed
- **Context:** Hook delivery is at least once; processes may crash between Git
  or provider side effects and result persistence. Multiple clients may target
  one repository. Timestamp/gate files cannot provide transactional recovery.
- **Decision:** Store normalized ingress receipts (event ID, idempotency key,
  canonical digest, and disposition), durable domain events/outbox records,
  state revisions, causal gaps, candidate metadata, leases, job intent, retry
  state, and redacted audit events in a local SQLite database under the platform
  application-data directory. Receipt insertion and state transition are
  transactional; side-effect intent is written before Git/provider execution.
  Repository policy remains in a reviewable project/user configuration layer.
- **Consequences:** ACID transactions and migrations support idempotency, causal
  replay, and recovery. AutoGit must select and audit a driver, manage
  corruption/backups, secure file permissions, reconcile effects after crashes,
  and avoid placing source/prompt content in the DB. Legacy marker/gate files
  remain compatibility inputs only; they are not the recovery database.
- **Alternatives considered:** Atomic JSON is simple but difficult for concurrent
  indexes and migrations; an always-running server adds lifecycle complexity;
  a remote database violates local-first/privacy goals.
