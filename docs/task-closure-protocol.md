# Task closure protocol and agent-loop setup

Status: recommended operating model, 2026-09-10

## Decision

Use a two-ledger task model with a depth-first, evidence-first loop:

1. Keep the existing checklist as the release-acceptance ledger.
2. Track implementation verification separately from release acceptance.
3. Count package-level work and checklist rows as different metrics.
4. Require every loop iteration to end in one of three observable outcomes:
   an implementation transition, an acceptance transition, or a precise blocker.
5. Use pull-request checks for normal work and a protected release environment
   for the external approvals and provenance that cannot be created by a local
   agent.

This is the best fit for AutoGit because it preserves the existing fail-closed
release policy while preventing completed local implementation from being
reported as unfinished engineering.

## Root cause of the persistent “26 open” count

The number is accurate for the current representation, but it is easy to
misread:

- `todo.md` contains 96 checklist rows: 70 checked and 26 unchecked.
- The user-assigned batch names 10 work packages: P4-01 through P4-08,
  P5-01, and P5-02.
- P5-01 is represented by two independently checkable rows, so those 10
  packages occupy 11 checklist rows.
- The Phase 4/5 matrix records local implementation evidence for the package
  scope, but its acceptance column remains open where the plan requires an
  exact tag, provider authorization, signing/provenance, or named review.
- Because the tracker says a row may be checked only after its promotion rule
  is satisfied, repeating local tests cannot reduce the count. The loop is
  proving the same implementation state again without acquiring the missing
  external state.

Therefore, “26 tasks remain” is not the precise statement. The precise
statement is: **26 release-acceptance rows are unchecked; the local
implementation subset is substantially further along.**

## Why this setup follows the research

The relevant research converges on making the agent’s environment, feedback,
and acceptance criteria explicit:

- The SWE-bench task formulation shows that real issue resolution spans
  multiple files, execution, and repository-level acceptance, so a task
  should not be closed from a model narrative alone. See the
  [SWE-bench paper](https://arxiv.org/abs/2310.06770).
- SWE-agent’s results emphasize that the agent-computer interface and the
  edit/test feedback loop materially affect performance. See the
  [SWE-agent paper](https://arxiv.org/abs/2405.15793).
- OpenAI’s [harness-engineering guidance](https://openai.com/index/harness-engineering/)
  describes underspecified environments as a major source of lost progress
  and recommends making missing capabilities legible and enforceable, with
  depth-first work as the practical approach.
- GitHub’s [protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merging-pull-requests/managing-protected-branches/about-protected-branches)
  and [required status checks](https://docs.github.com/en/pull-requests/reference/status-checks)
  provide the normal implementation-to-review transition. Direct pushes to
  an unprotected default branch do not create that review transition.
- GitHub [environments and deployment protection rules](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
  provide an explicit approval boundary for release operations, including
  required reviewers and branch restrictions.
- GitHub [artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations)
  and the [SLSA build-provenance specification](https://github.com/slsa-framework/slsa/blob/main/spec/build-provenance.md)
  support binding an artifact to its source commit and build context rather
  than treating a passing local command as release identity.
- NIST SSDF recommends securely archiving release files and supporting
  integrity/provenance data; see [SP 800-218](https://nvlpubs.nist.gov/nistpubs/specialpublications/nist.sp.800-218.pdf).

The conclusion is an engineering recommendation: adding another autonomous
agent framework would not fix this repository’s current problem. The missing
capability is a durable task state transition with authority-aware gates, so
the existing Codex/tool/CI setup should be made more explicit first.

## The state model

Do not encode all meaning in one checkbox. Each work item needs two related
states.

| Dimension | States | Meaning |
| --- | --- | --- |
| Implementation | `open` → `in_progress` → `verified` | The code/config/docs exist and the required local or hosted implementation checks passed. |
| Acceptance | `not_ready` → `pending_review` → `accepted` | The plan’s promotion evidence, exact identity, required platforms, and named authority are recorded. |
| Exception | `blocked` | A named dependency, credential, platform, reviewer, or external event is missing; the blocker is recorded with an owner and next action. |

The user’s “complete 10 tasks” request must state which terminal condition is
intended:

- **Implementation batch complete:** all 10 package IDs are `verified`.
- **Release batch complete:** all relevant checklist rows are `accepted`.
- **Blocked:** the implementation is verified but acceptance cannot advance
  without external authority.

These are different outcomes. The first can be completed by the engineering
loop; the second cannot be honestly completed without the release owner,
provider, tag, or reviewer named by the plan.

A durable task record should contain at least:

```yaml
id: P4-01
package: phase-4-quality
row_ids: [P4-01]
implementation_status: verified
implementation_evidence:
  - docs/release-evidence/phase-4-quality.md
  - docs/release-evidence/phase-4-quality.json
  - hosted_run: 34388511108
  - commit: 48177501564cda9aa5507ab789c71ce3f285a0d5
acceptance_status: pending_review
acceptance_requirements:
  - named reviewer
owner: release-owner
next_action: record independent review against the exact snapshot
```

The important fields are the stable ID, row mapping, exact evidence identity,
acceptance status, owner, and next action. A free-form “done” note is not a
state transition.

## The chosen loop

Run one package at a time, depth-first:

1. **Select one stable ID.** Define whether the batch target is a package or a
   checklist row before starting.
2. **Read the contract.** Extract the implementation-plan acceptance
   conditions, dependencies, required platforms, and external authority.
3. **Write the closure contract.** Name expected files, commands, evidence
   outputs, and the exact state transition that will be allowed.
4. **Baseline once.** Check the current commit, working tree, existing evidence
   identity, and relevant tests. Do not rerun expensive work whose inputs have
   not changed.
5. **Implement the smallest vertical slice.** Keep the change reviewable and
   preserve user-owned work.
6. **Verify in layers.** Run focused tests first, then the project’s required
   verification, then inspect the diff and generated evidence.
7. **Reconcile evidence.** Check that evidence belongs to the exact commit and
   that it satisfies each acceptance condition, not merely a similar one.
8. **Transition the state.** Update the implementation ledger or acceptance
   ledger and link the evidence. If the transition is impossible, record the
   exact blocker instead.
9. **Commit through the repository hook.** The verification hook must pass
   before the auto-commit metadata is produced.
10. **Recount from source.** Report package count, row count, implementation
    status, acceptance status, and blockers separately before selecting the
    next item.

The loop has a hard anti-spin rule: if an iteration produces no code/evidence
change and no state transition, stop repeating it and escalate the missing
authority or change the task selection. Passing the same local test again is
not progress.

## Recommended repository and CI setup

Use the current repository as the execution harness, with four explicit
controls:

1. **Machine-readable task ledger.** Add stable package/row IDs, the two
   statuses, evidence references, owner, reviewer, and blocker fields. Generate
   the human summary in `todo.md` from or against this ledger so the count
   cannot silently drift.
2. **PR-only implementation flow.** Protect `main`, require the quality and
   evidence checks, and require an independent review for rows that claim
   acceptance. This makes implementation review an actual transition rather
   than an unrecorded direct push. The documented single-maintainer
   private-alpha exception may substitute named release-owner approval for
   this reviewer field; that approval must not be described as independent and
   the exception does not apply to public beta or GA.
3. **Release-only protected environment.** Permit the tag-gated release job to
   proceed only after the exact commit/version checks pass and a release
   reviewer approves the environment. Keep the canary credential in that
   environment and use a dedicated least-privilege token.
4. **Evidence-first reporting.** Every completion message must include the
   stable ID, old/new status, exact commit, test/CI run, evidence path, and any
   remaining external gate. The headline should say “31 unchecked acceptance
   rows,” not “26 unfinished tasks.”

The current implementation ledger for the assigned Phase 4/5 batch is
[`phase-4-5-implementation-ledger.json`](release-evidence/phase-4-5-implementation-ledger.json).
It is deliberately separate from `todo.md`: implementation may be `verified`
while release acceptance remains `pending_review`.

For this repository, use one primary orchestrator and at most one independent
reviewer per package. Parallel agents may work in isolated worktrees for
independent experiments, but they should not mutate the same tracker or branch.
The coordination cost and conflicting state transitions would make the current
problem worse.

## Immediate interpretation for the current batch

The current evidence supports this report:

- The 10 requested package areas have local implementation/evidence work.
- The acceptance matrix is the correct place to see which package-specific
  gates remain.
- The 26-row number will not decrease until either the strict acceptance gates
  are supplied or the project adopts the two-ledger policy and records the
  implementation rows as verified in a separate implementation ledger.
- It would be incorrect to check the release rows merely to make the number
  smaller. That would erase the distinction the release plan is designed to
  protect.

The next smallest useful action is to adopt the two-ledger state model and
decide whether “complete 10 tasks” means `verified` implementation or formal
`accepted` release work. If it means formal acceptance, the next actions are
not more local test loops: create the exact release tag, obtain the protected
environment approval, provide the dedicated canary/provider authority, and
record named product/security/release review.

## Sources

The links below are the primary or authoritative sources used for the
recommendation. Repository-specific facts come from `todo.md`,
`docs/implementation-plan.md`, and
`docs/release-evidence/phase-4-5-acceptance-matrix.md` at the current checkout.

- [SWE-bench](https://arxiv.org/abs/2310.06770)
- [SWE-agent](https://arxiv.org/abs/2405.15793)
- [OpenAI: Harness engineering](https://openai.com/index/harness-engineering/)
- [GitHub: Protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merging-pull-requests/managing-protected-branches/about-protected-branches)
- [GitHub: Status checks](https://docs.github.com/en/pull-requests/reference/status-checks)
- [GitHub: Deployments and environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
- [GitHub: Artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations)
- [SLSA: Build provenance](https://github.com/slsa-framework/slsa/blob/main/spec/build-provenance.md)
- [NIST SP 800-218: Secure Software Development Framework](https://nvlpubs.nist.gov/nistpubs/specialpublications/nist.sp.800-218.pdf)
