# ADR-0006: Separate adapter ingress from durable domain events

- **Status:** Proposed
- **Context:** Codex, Claude Code, Cursor, Gemini CLI, OpenCode, and
  CommandCode expose different hook payloads. Adapter claims about completion,
  changed files, or queue state may be missing, duplicated, or wrong, while
  Git/provider side effects require independently verified facts.
- **Decision:** Use one versioned envelope with an explicit `event_class`.
  Adapter observations are `ingress` events and are never authoritative or
  replayed as state. The core validates, deduplicates, reconciles the local
  repository, and emits `domain` events for durable state and side-effect
  intents. Ingress-only paths/cwds are ephemeral and stripped before durable
  storage.
- **Consequences:** The boundary is slightly more verbose and adapters need
  capability manifests. It prevents a model stop or client completion claim
  from being mistaken for verified task completion and permits a future daemon
  without changing adapter behavior.
- **Alternatives considered:** Treating adapter events as domain facts is
  simpler but unsafe under missing queue state, out-of-order delivery, and
  process crashes. Separate schemas would duplicate transport and compatibility
  logic, so one envelope with an explicit class is preferred.
