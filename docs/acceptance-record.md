# Phase 0 contract acceptance record

Status: Accepted for the implementation baseline; release posture remains
NO-GO until the remaining safety, native-matrix, and release gates pass.

Accepted by: Uday Varmora
Accepted on: 2026-09-07

## Scope

This record accepts the normative implementation baseline and terminology used
by the following documents. It does not approve a private alpha, public beta,
live provider canary, or release artifact.

| Accepted area | Normative document | Evidence boundary |
| --- | --- | --- |
| Product requirements | [product requirements](product-requirements.md) | Must-level requirements and traceability checks |
| Lifecycle state and transitions | [lifecycle](lifecycle.md) | Lifecycle reducer and replay tests |
| Wire/event contract | [event contract](event-contract.md) | JSON schema, decode, adapter, receipt, and causal replay tests |
| Threat model and invariants | [threat model](threat-model.md) | Security, ownership, process, and provider-boundary tests |
| Compatibility window | [contract freeze](contract-freeze.md) and [compatibility manifest](compatibility-manifest.json) | Versioned event/result/state compatibility checks |
| Release terminology | [contract freeze](contract-freeze.md) and [release runbook](release-runbook.md) | Evidence/status language and explicit NO-GO gates |

The acceptance is limited to the stated baseline. A document or command must
not infer phase promotion from this record; promotion still requires the
phase-specific evidence listed in [todo.md](../todo.md).

## Private-alpha release-owner exception

For this single-maintainer repository, the owner may serve as the named
release reviewer for the bounded private-alpha candidate. This is an explicit
owner approval and must not be described as independent review. It does not
waive the exact-tag, provider-canary, signed-provenance, reproducibility, or
consumer-verification requirements, and it does not apply to public beta or
GA. Public beta and GA still require independent security/release review.

Decision owner: Uday Varmora (`@udayvarmora07`)

Decision date: 2026-09-10

## License decision

The project license is Apache License 2.0. The repository `LICENSE` file and
the license statement in `README.md` agree on that decision.

Accepted by: Uday Varmora (`@udayvarmora07`)

Accepted on: 2026-09-10

## Governance and security-contact review

The owner reviewed the repository governance baseline for the private-alpha
scope. `SECURITY.md` selects private GitHub Security Advisories as the security
contact, `docs/support-policy.md` defines supported versions and response
handling, `.github/CODEOWNERS` names the maintainer, and the contribution,
conduct, and changelog documents are present and linked from the README.

Decision: accepted for the bounded private-alpha scope. This decision does not
approve public beta or GA and does not replace the exact-tag release evidence.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-10

## Dependency and workflow policy review

The owner reviewed the dependency and workflow controls for the bounded
private-alpha scope. The review covered `docs/dependency-policy.md`,
`docs/dependency-licenses.json`, `.github/dependabot.yml`, all five files in
`.github/workflows/`, `scripts/check-dependencies.sh`, and the
[`gosec` suppression register](release-evidence/gosec-suppressions.md).

The reviewed baseline has a documented SPDX allowlist, direct license
inventory, `go mod verify`, weekly Dependabot proposals capped at five open
pull requests per ecosystem, high-severity dependency-review blocking,
CodeQL and OpenSSF Scorecard jobs, least-privilege permissions, and full-SHA
action pinning. Open Dependabot proposals remain subject to the policy job and
deliberate maintainer review; no repository workflow performs automatic
merges. The current action-update proposals are tracked in pull requests
[#2](https://github.com/udayvarmora07/autogit/pull/2),
[#3](https://github.com/udayvarmora07/autogit/pull/3),
[#4](https://github.com/udayvarmora07/autogit/pull/4), and
[#5](https://github.com/udayvarmora07/autogit/pull/5).

The policy exceptions are justified and bounded: the 28 `gosec` suppressions
are line-scoped false positives at documented compatibility or security
boundaries, each has linked tests, and the register defines review triggers.
The workflow security review found no exploitable external-attacker path in
the reviewed triggers, permissions, secrets, or action references.

Decision: accepted for the bounded private-alpha scope. This decision does
not approve public beta or GA and does not permit unreviewed dependency or
workflow updates.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-10
