# Security audit record — 2026-10-16

Status: local adversarial audit; five findings fixed at their exact commits,
one open finding tracked as P1-11. Release posture is unchanged (NO-GO).

## Scope and method

Four independent, read-only adversarial hunts covered the filesystem/state
boundary (securefs, state, install, repository path handling, db), the process
and verification boundary (process, verification, security, policy), the
git/provider transport (gitport, gittransaction, staging, historyscan, session,
provider, publication), and the ingress/persistence/surface boundary (events,
db, session, lifecycle, mcp, cli, telemetry). Every surviving finding was then
reproduced or disproved by the lead auditor with the system Git, before any
fix. Fixes landed with regression tests and the full local core suite.

## Fixed findings

| Finding | Severity | Fix commit | Evidence |
| --- | --- | --- | --- |
| `git push` delivers to every configured push URL while the exact-destination binding check validated only the first (`git remote get-url --push`) | Critical | `f41bff3` | Real-git regression test configures a canonical first push URL plus an attacker bare repository; the push now fails closed before delivery |
| Repository-local `http.sslVerify=false`, including a URL-scoped `http.https://github.com/.sslVerify` entry that outranks a generic command-line pin, could disable TLS verification for the canonical push | High | `f634ee2` | Empirically verified Git config precedence; hardened argv now pins generic and URL-scoped `sslVerify=true` plus `protocol.ext.allow=never` and `protocol.file.allow=never` |
| `commit.Validate` denied only two trailer spellings, so an untrusted completion reason could add `Closes #12` or `Reviewed-by:` provenance under auto-complete policy (TM-015) | Medium | `b24d604` | Validator denies the hyphenated `*-by:` trailer shape and issue-closure keywords with a concrete reference; prose without references still validates |
| A verifier could `setsid` a helper that escapes process-group and process-tree cleanup while holding the output pipes, so `process.Run` could never return and descendant cleanup was overstated | Medium | `71303a0` | `cmd.WaitDelay` force-closes the stream and reports `exec.ErrWaitDelay`; the limitation is recorded in ADR-0010, the isolation capability, and the P1-04 evidence; Linux regression test covers the setsid pipe holder |
| `install.Plan` skipped the Apply-time confinement comparison when the target parent did not exist, so a symlink planted at the missing parent could redirect the config write outside the approved root | Low | `4ef2c1d` | Plan records the nearest existing ancestor and its canonical form; regression tests cover the swap and normal nested first install |

## Open finding

`internal/repository/repository.go` accepts any `.git` gitfile whose `gitdir:`
points at another repository's active git directory. Because repository identity
is the digest of that common directory and policy is keyed by identity, a
checkout can adopt another local repository's policy/consent scope: `enable` on
the alias rewrites the other repository's policy and hook scope checks accept
the alias. A legitimate linked worktree is indistinguishable by path shape, so
the fix requires an explicit design decision (worktree-registration check or
explicit identity confirmation when the requested root is not the registered
worktree). Tracked as P1-11 in [todo.md](../../todo.md) and TM-027 in the
[threat model](../threat-model.md); not fixed in this audit to avoid silently
changing worktree semantics.

## Residual hardening (recorded, not fixed)

Repository-local `http.sslCAInfo`, `http.sslCAPath`, `http.sslCert`,
`http.sslKey`, and `http.proxy` cannot be reset to their defaults through
`-c` pins (empty values either break TLS or change semantics). A hostile repo
local configuration plus a reachable proxy can therefore still subvert TLS
trust. Recommended follow-up: refuse the push when repository-local config
carries transport override keys, or push through a boundary that does not read
repository-local transport configuration.

## Informational observations

- The verifier executes in the live worktree rather than an immutable snapshot
  of the exact candidate digest; the same consenting user owns both, and
  publication re-verifies the exact SHA, so this remains a documented
  trade-off.
- `AUTOGIT_STATE_DIR`-style overrides are trusted inputs; controlling the local
  environment already implies same-user control.
