# ADR-0001: Go modular monolith with adapters

- **Status:** Proposed
- **Context:** AutoGit needs a portable local executable, strict concurrency and
  process handling, fast hook startup, and integrations with evolving clients.
  The initial team and deployment scope do not justify distributed services.
- **Decision:** Build one Go executable with domain modules behind typed ports.
  Client adapters translate versioned ingress events and contain no Git
  mutation logic. The core validates, reconciles, and emits durable domain
  events before scheduling side effects. An optional daemon may be added later
  without changing contracts.
- **Consequences:** Releases are simple cross-platform binaries and safety logic
  has one owner. Go contributors must maintain careful OS-specific filesystem
  and locking tests. Adapter APIs still require frequent compatibility testing.
- **Alternatives considered:** Bash retains packaging simplicity but is already
  too coupled for transactions/concurrency; Rust offers stronger memory-safety
  guarantees but higher initial contribution cost; Node/TypeScript simplifies
  some plugin work but adds runtime/packaging and dependency surface; Python is
  fast to prototype but has similar distribution/runtime concerns.
