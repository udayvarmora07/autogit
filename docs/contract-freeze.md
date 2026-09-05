# AutoGit v1 contract freeze record

Status: implementation baseline recorded; product acceptance review pending  
Last updated: 2026-09-05

This record resolves terminology and compatibility assumptions used by the Go
v1 implementation. It is evidence for Phase 0 review; it does not claim that
the Phase 0 exit gate or a release phase has passed.

## Canonical terminology

| Term | Frozen meaning |
| --- | --- |
| Ingress event | An untrusted, at-least-once observation from an adapter. It can request reconciliation but never authorizes a Git/provider side effect. |
| Domain fact | A core-owned, validated event emitted after reconciliation or a side-effect outcome. Durable projections rebuild only from domain facts. |
| Tracking consent | The durable policy decision that permits AutoGit to manage a repository. `no` disables tracking; `local` permits local work only; `yes` permits the explicitly configured provider policy. |
| Public consent | A separate explicit approval required for `public` visibility. Tracking consent never implies public consent. |
| Baseline | The bounded session-start observation of `HEAD`, the shared index, status, and pre-existing path evidence. |
| Owned candidate | The immutable set of current changes attributable to a session after baseline comparison. Ambiguous pre-existing changes are blocked or excluded. |
| Verification evidence | A digest-bound result for one candidate, base, policy, guard, and trusted verifier set. |
| Commit intent | Durable identity persisted before local Git mutation: repository, parent, tree, message, candidate, and policy evidence. |
| Push intent | Durable identity persisted before provider mutation: destination, ref, exact commit SHA, and remote digest. |
| Writer lease | A durable per-repository/worktree/ref exclusion record protecting shared Git or provider mutation. |

The terms “stop”, “idle”, “completion claim”, “candidate”, “commit”, and
“push” are not interchangeable. A client stop or completion claim is ingress
evidence; only the core can promote it to a domain completion candidate, and a
verified workflow is still required before a commit.

## v1 compatibility boundary

- The supported event namespace is `autogit.event/1`; the result namespace is
  `autogit.result/1`.
- v1 accepts the current major only. There is no released v0 major, so v1 does
  not claim a previous-major compatibility window.
- Within major 1, optional additions belong under `extensions`; changing a
  required field, removing an enum value, or changing field meaning requires a
  new major. Unknown core envelope and payload fields remain rejected.
- Adapter manifests advertise the supported major explicitly. Current
  manifests advertise only `autogit.event/1` and must safely degrade missing
  client capabilities rather than inventing them.
- State schema migrations are independent of the wire major. The current
  durable state schema is v7; older supported local schemas migrate forward,
  while a future schema is rejected rather than partially downgraded.
- A future v2 release must publish a separate migration window before v1 is
  retired. Until that decision is recorded, v1 remains the only supported wire
  major and no v2 input is accepted.

## Non-negotiable invariants

1. No mutation occurs outside a canonical approved repository/worktree.
2. No Git/provider mutation occurs without tracking consent; public publication
   additionally requires public consent and a preflight summary.
3. Only owned, unambiguous candidate paths enter a commit; user index and
   unrelated work remain untouched.
4. Candidate, base, policy, verifier, guard, message, and repository-state
   changes invalidate dependent evidence.
5. Durable intent precedes every Git/provider side effect, and recovery proves
   the exact tree/SHA/ref before accepting an unknown outcome.
6. No force-push, all-ref, delete, automatic reset/rebase, repository removal,
   or implicit destination attachment is permitted.
7. Default state, logs, results, and events contain bounded redacted evidence,
   not prompts, source, diffs, credentials, tokens, or raw remote URLs.
8. A missing capability or uncertain outcome degrades to a visible
   checkpoint/pending/blocked result; it never becomes implicit completion.

## Review evidence index

The implementation evidence is distributed as follows:

- event/schema, receipt, causal replay, and redaction: `internal/events` and
  `internal/lifecycle` tests;
- repository identity, baseline, ownership, path/race handling, and restart
  evidence: `internal/repository`, `internal/session`, and `internal/staging`;
- candidate security, trusted verification, and message composition:
  `internal/security`, `internal/verification`, `internal/commit`, and
  `internal/workflow`;
- durable intent, lease, crash recovery, provider identity, and publication:
  `internal/coordinator`, `internal/gittransaction`, `internal/provider`, and
  `internal/publication`;
- adapter/install contracts and CLI operations: `internal/adapters`,
  `internal/install`, and `cmd/autogit` tests;
- requirement-to-suite mapping and release gates: `docs/test-strategy.md`.

The remaining review/release gates are recorded in
[`docs/implementation-plan.md`](implementation-plan.md) and are not silently
closed by this contract record: automatic message/verifier selection for
installed hooks, randomized crash schedules, native macOS/Windows execution,
the disposable provider canary, and alpha/beta promotion.
