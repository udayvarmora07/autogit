# ADR-0004: Safe branch workflow by default

- **Status:** Proposed; local/publication preflight implemented, hosted branch/PR orchestration remains open
- **Context:** Direct pushes to a default branch can publish code before remote
  CI and protected-branch checks run. Solo developers may still prefer direct
  verified commits for small projects.
- **Decision:** `safe` is the default workflow policy, `solo` is an explicit
  direct-push policy, `local` forbids provider operations, and `checkpoint`
  retains unfinished local work. The local workflow and public preflight bind
  publication to an explicit ref and require feature-branch approval for safe
  public publication; protected-branch/status-check evidence is fail-closed.
  AutoGit never force-pushes, deletes a repository, or publishes failed
  verification. Creating a hosted branch or pull request remains a separate
  provider/release capability and is not inferred by the local hook.
- **Consequences:** Existing hooks remain compatible with conservative local
  behavior, while explicit CLI publication can report safe/solo decisions
  before provider access. Future branch/PR orchestration must preserve branch
  identity in push-job idempotency and reconciliation before being enabled.
- **Alternatives considered:** Always direct-push is simpler but weakens remote
  quality gates. Always create PRs is excessive for local-only/small experiments.
