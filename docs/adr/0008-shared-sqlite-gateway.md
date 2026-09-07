# ADR-0008: One shared SQLite gateway

- **Status:** Accepted for Phase 0
- **Context:** Event receipts and workflow jobs share one `state.db`, but
  independent openers previously owned different pragma and migration paths.
  That made WAL behavior, schema upgrades, and connection concurrency depend on
  which repository opened the file first.
- **Decision:** `internal/db` is the sole production opener and migration
  owner. It pins the embedded SQLite engine at or above 3.51.3, configures one
  in-process connection, WAL, busy timeout, foreign keys, synchronous mode, and
  checkpointing, then migrates both event and workflow tables in one bounded
  transaction. Read-only diagnostics use a separate no-create, no-migration
  opener from the same package. Context-aware state and event openers pass the
  caller's cancellation budget through this gateway.
- **Consequences:** State and event repositories depend on the gateway rather
  than importing the SQLite driver. Legacy databases are upgraded together;
  newer schema versions fail closed. Cross-process contention is retried only
  within the opener deadline.
