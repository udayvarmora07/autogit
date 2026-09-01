# ADR-0004: Safe branch workflow by default

- **Status:** Proposed (Phase 1; not implemented by the Phase 0 hook contract)
- **Context:** Direct pushes to a default branch can publish code before remote
  CI and protected-branch checks run. Solo developers may still prefer direct
  verified commits for small projects.
- **Decision:** Defer branch/PR policy to Phase 1. Phase 0 recognizes the
  existing `yes`, `yes public`, `yes private`, `yes local`, and optional `fast`
  policy markers only. Phase 0 never force-pushes, deletes a repository, or
  publishes failed verification. When branch policy is added, `safe` should be
  the default, `solo` an explicit direct-push mode, `local` no-network, and
  `checkpoint` unfinished local work.
- **Consequences:** Phase 0 remains compatible with the installed hooks and
  does not promise branch or PR behavior. Phase 1 will need branch identity in
  push-job idempotency and reconciliation before enabling these modes.
- **Alternatives considered:** Always direct-push is simpler but weakens remote
  quality gates. Always create PRs is excessive for local-only/small experiments.
