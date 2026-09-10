# Phase 4/5 acceptance matrix

Snapshot: `6fc197b281065a03dccb8bad68984222bc8c786d`
Local manifest: [phase-4-quality.json](phase-4-quality.json)
Hosted matrix: [run 34481766917](https://github.com/udayvarmora07/autogit/actions/runs/34481766917)
Hosted result: 20/20 jobs passed, including native Linux/macOS/Windows tests,
six native artifact targets, three soak targets, fuzz, scenario evaluation,
performance gates, reproducible builds, security, and dependency policy.

This matrix separates implementation evidence from the tracker’s promotion
rule. A row stays unchecked when the evidence is present but the plan still
requires a release owner, provider authorization, exact tag, or named review.

For the bounded private-alpha candidate, the documented single-maintainer
exception permits named release-owner approval in place of independent review.
This is an owner decision, not independent evidence, and it does not apply to
public beta or GA.

| Package | Evidence at the exact snapshot | Acceptance status |
| --- | --- | --- |
| P4-01 | Manifest records the presubmit, core, race, integration, soak, fuzz, canary, and release tiers; the hosted matrix exercises the bounded native quality path. | Implementation evidenced; named review still required by the promotion rule. |
| P4-02 | Shell syntax/ShellCheck and injected failure-safety tests are recorded in the manifest and passed in hosted security/policy checks. | Implementation evidenced; named review still required by the promotion rule. |
| P4-03 | Six native artifact jobs passed in the hosted matrix, with system-Git floor and built-artifact smoke checks. | Native implementation evidence complete; release acceptance remains open. |
| P4-04 | The hosted fuzz job passed the 100,000-input floor for each target; the local manifest records the fuzz suite. | Implementation evidenced; named review still required by the promotion rule. |
| P4-05 | Chaos, recovery, permission-loss, clock, lock, signal, network-stall, and partial-response tests are covered by the implementation and quality suites. | Implementation evidenced; named review still required by the promotion rule. |
| P4-06 | Native scenario jobs passed and retained bounded traces for the six registered clients. | Native implementation evidence complete; release acceptance remains open. |
| P4-07 | Compatibility checks and expiry automation are recorded in the manifest; the compatibility contract has no due or expired window. | Implementation evidenced; named review still required by the promotion rule. |
| P4-08 | The manifest binds requirements, threats, controls, suites, commit, and artifact hashes. | Open: `tag_verified` is false because no exact release tag exists. |
| P5-01 | LICENSE, SECURITY.md, CONTRIBUTING.md, CODE_OF_CONDUCT.md, CODEOWNERS, support policy, and CHANGELOG.md are present. | Accepted for the bounded private-alpha scope by the named release owner; public beta/GA review remains separate. |
| P5-02 | Dependabot, dependency review, CodeQL, Scorecard, pinned-action enforcement, module verification, and license policy are present and passed. | Accepted for the bounded private-alpha scope by the named release owner; automated-update review and policy exceptions are recorded. Public beta/GA review remains separate. |

The following evidence is intentionally not claimed: a live provider canary
with the dedicated token, a release-environment approval, keyless hosted
attestations, independent release verification, or a named security/release
review. The last canary attempt (`34324968472`) stopped before mutation because
`AUTOGIT_CANARY_TOKEN` was empty.
