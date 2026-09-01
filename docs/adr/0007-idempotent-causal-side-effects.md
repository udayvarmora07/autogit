# ADR-0007: Durable idempotency, causal replay, and intent-before-effect

- **Status:** Proposed
- **Context:** Stop/idle hooks are delivered at least once, clients can omit
  sequence numbers, sessions can run concurrently, and a process can die after
  `git commit` or `git push` but before it receives or stores the result.
- **Decision:** Persist an event receipt keyed by `event_id` and logical
  `idempotency.key` with a canonical payload digest. Buffer events with missing
  causal predecessors and replay them by causal references/local receipt
  revision. Serialize Git mutations with a durable repository/worktree/branch
  lease. Persist `commit.requested` and `push.requested` before side effects;
  reconcile exact tree/SHA/ref evidence before retrying. A duplicate is a no-op;
  a key collision is quarantined; an unknown side-effect outcome is
  `RECONCILE_REQUIRED`.
- **Consequences:** SQLite schema, retention, lease expiry, and reconciliation
  tests are required. The system may defer a safe action during a causal gap or
  transient provider failure, but it will not silently duplicate or force a
  mutation. Concurrent read-only verification is allowed only against immutable
  candidate snapshots.
- **Alternatives considered:** Timestamp ordering loses events under clock
  skew; process-local locks do not survive crashes; retrying every failed call
  can duplicate commits or pushes; distributed transactions are excessive for
  the local-first product.
